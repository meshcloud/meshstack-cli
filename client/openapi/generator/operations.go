package generator

import (
	"cmp"
	"errors"
	"fmt"
	"go/token"
	"path"
	"slices"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/openapi"
)

var methodOfAction = map[string]string{
	"list":   "List",
	"show":   "Get",
	"create": "Create",
	"update": "Update",
	"delete": "Delete",
}

func (g *generator) addPackage(config Package) error {
	config, err := config.defaults()
	if err != nil {
		return err
	}
	p := &pkg{Package: config, importPath: path.Join(g.cfg.Module, config.Path), names: map[string]bool{}}
	g.packages = append(g.packages, p)
	selected, err := g.selectOperations(p)
	if err != nil {
		return err
	}
	for _, operation := range selected {
		if p.Kind != "" {
			if mediaType, ok := operation.MediaType(openapi.MustParseApiVersion(p.ApiVersion)); ok {
				p.mediaType = mediaType.Name
			}
		}
		if literal := literalStart(string(operation.PathTemplate)); p.collection == "" || len(literal) < len(p.collection) {
			p.collection = literal
		}
	}
	if p.Kind != "" && p.mediaType == "" {
		return fmt.Errorf("no operation of %s offers %s", p.Kind, p.ApiVersion)
	}
	for _, operation := range selected {
		o, err := g.operation(p, operation)
		if err != nil {
			return fmt.Errorf("%s %s: %w", operation.Method, operation.PathTemplate, err)
		}
		p.operations = append(p.operations, o)
	}
	slices.SortFunc(p.operations, func(a, b *operation) int { return cmp.Compare(a.name, b.name) })
	return nil
}

func (g *generator) selectOperations(p *pkg) ([]openapi.Operation, error) {
	var selectors []openapi.Selector
	var version openapi.ApiVersion
	if p.ApiVersion != "" {
		version = openapi.MustParseApiVersion(p.ApiVersion)
	}
	for _, action := range p.Actions {
		selectors = append(selectors, openapi.Selector{Kind: p.Kind, Action: action, ApiVersion: version})
	}
	for _, id := range p.OperationIds {
		selectors = append(selectors, openapi.Selector{OperationId: id, ApiVersion: version})
	}
	var selected []openapi.Operation
	for _, selector := range selectors {
		spec, err := g.spec.Select(selector)
		if err != nil {
			return nil, err
		}
		if len(spec.Operations) == 0 {
			return nil, fmt.Errorf("the API docs have no operation for %+v", selector)
		}
		selected = append(selected, spec.Operations...)
	}
	return selected, nil
}

func literalStart(template string) string {
	before, _, _ := strings.Cut(template, "{")
	return strings.TrimSuffix(before, "/")
}

func (g *generator) operation(p *pkg, selected openapi.Operation) (*operation, error) {
	schemas, err := selected.Schemas()
	if err != nil {
		return nil, err
	}
	o := &operation{
		name:         cmp.Or(methodOfAction[selected.Action], goName(selected.Action)),
		method:       selected.Method,
		pathTemplate: string(selected.PathTemplate),
		list:         selected.Action == "list",
	}
	if p.Kind == "" || selected.Action == "" {
		o.name = goName(strings.TrimPrefix(selected.OperationId, p.Kind))
	}
	o.doc, err = g.summary(selected)
	if err != nil {
		return nil, err
	}
	if lifecycle := schemas.Lifecycle(p.mediaType); lifecycle != nil {
		p.lifecycle = lifecycle
	}
	if selected.Deprecated {
		o.deprecation = cmp.Or(p.deprecation(), openapi.NewerVersionAvailable)
	}
	o.queryType = o.name + "Query"
	if o.list {
		o.queryType = "ListFilter"
	}
	for _, parameter := range schemas.Parameters {
		if parameterErr := g.addParameter(p, o, parameter); parameterErr != nil {
			return nil, parameterErr
		}
	}
	o.disambiguatePathParams()
	if len(o.query) == 0 {
		o.queryType = ""
	}
	if body := p.content(schemas.RequestBody); body != nil {
		r, sendsMeshObject, bodyErr := g.root(p, body, request)
		if bodyErr != nil {
			return nil, fmt.Errorf("request body: %w", bodyErr)
		}
		o.body, o.sendsMeshObject = &r, sendsMeshObject
	}
	for _, r := range schemas.Responses {
		answer := p.content(r.Content)
		if !strings.HasPrefix(r.Status, "2") || answer == nil {
			continue
		}
		if o.list {
			if answer, err = g.pageItems(p, answer); err != nil {
				return nil, err
			}
		}
		result, answersMeshObject, err := g.root(p, answer, response)
		if err != nil {
			return nil, fmt.Errorf("response %s: %w", r.Status, err)
		}
		o.result, o.answersMeshObject = &result, answersMeshObject
		break
	}
	if o.list && o.result == nil {
		return nil, fmt.Errorf("a list answers no page of %s", p.mediaType)
	}
	return o, nil
}

func (g *generator) summary(operation openapi.Operation) (string, error) {
	doc, err := g.spec.Doc(operation)
	return doc.Summary, err
}

func (g *generator) addParameter(p *pkg, o *operation, parameter openapi.ParameterSchema) error {
	if parameter.Schema == nil || parameter.In == "header" {
		return nil
	}
	resolved, err := g.resolve(parameter.Schema)
	if err != nil {
		return err
	}
	switch parameter.In {
	case "path":
		key := parameter.PathKey.Key
		if byVersion, ok := parameter.PathKey.ByVersion[p.ApiVersion]; ok {
			key = byVersion
		}
		o.addPathParam(parameter, key, resolved.Format == "uuid")
	case "query":
		if parameter.Name == "page" || parameter.Name == "size" {
			return nil
		}
		r, err := g.queryRef(p, Location{Schema: o.queryType, Property: parameter.Name}, parameter.Schema)
		if err != nil {
			return fmt.Errorf("query parameter %s: %w", parameter.Name, err)
		}
		o.query = append(o.query, param{name: parameter.Name, goName: goName(parameter.Name), ref: r})
	}
	return nil
}

// addPathParam names a path parameter after the key it takes, as uuid or name. A template key with
// a name of as many parts, as {metadata.ownedByWorkspace}.{metadata.name} with workspace.project,
// takes one parameter per part, so that a caller passes the fields and the client writes the
// template.
func (o *operation) addPathParam(parameter openapi.ParameterSchema, key string, isUuid bool) {
	add := func(name, goType, doc string) int {
		o.pathParams = append(o.pathParams, param{name: parameter.Name, goName: argName(name), goType: goType, doc: doc})
		return len(o.pathParams) - 1
	}
	names := strings.Split(parameter.PathKey.Name, ".")
	if parts := templateParts(key); countFields(parts) == len(names) {
		var arg []argPart
		for _, part := range parts {
			if part.field {
				part.param = add(names[countFields(arg)], "string", parameter.Description)
			}
			arg = append(arg, part)
		}
		o.pathArgs = append(o.pathArgs, arg)
		return
	}
	name := parameter.Name
	if key != "" && !strings.Contains(key, "{") {
		name = key[strings.LastIndex(key, ".")+1:]
	}
	goType := "string"
	if isUuid {
		goType = "uuid.UUID"
	}
	o.pathArgs = append(o.pathArgs, []argPart{{field: true, param: add(name, goType, parameter.Description)}})
}

func argName(name string) string {
	name = lowerFirst(goName(name))
	if token.IsKeyword(name) {
		return name + "Value"
	}
	return name
}

// argPart is a literal of a key's template, or a field, which a parameter of the method fills.
type argPart struct {
	text  string
	field bool
	param int
}

func templateParts(template string) []argPart {
	var parts []argPart
	for rest := template; rest != ""; {
		before, after, found := strings.Cut(rest, "{")
		if before != "" {
			parts = append(parts, argPart{text: before})
		}
		if !found {
			break
		}
		field, after, _ := strings.Cut(after, "}")
		parts = append(parts, argPart{text: field, field: true})
		rest = after
	}
	return parts
}

func countFields(parts []argPart) int {
	n := 0
	for _, part := range parts {
		if part.field {
			n++
		}
	}
	return n
}

// disambiguatePathParams prefixes the name of the path parameter where two keys share a name, as
// workspaceName and userName.
func (o *operation) disambiguatePathParams() {
	count := map[string]int{}
	for _, p := range o.pathParams {
		count[p.goName]++
	}
	for i, p := range o.pathParams {
		if count[p.goName] > 1 {
			o.pathParams[i].goName = lowerFirst(goName(p.name)) + upperFirst(p.goName)
		}
	}
}

// queryRef keys an inline enum by the package, since the ListFilter of every kind names its enums
// alike.
func (g *generator) queryRef(p *pkg, at Location, s *openapi.Schema) (typeRef, error) {
	resolved, err := g.resolve(s)
	if err != nil {
		return typeRef{}, err
	}
	switch {
	case resolved.Type == "array" && resolved.Items != nil:
		items, err := g.queryRef(p, at, resolved.Items)
		return typeRef{slice: &items}, err
	case len(resolved.Enum) > 0 && s.Ref == "":
		key := "#" + p.importPath + "#" + at.String()
		t, ok := g.types[key]
		if !ok {
			t = &namedType{loc: at, schema: resolved, kind: enumType}
			g.types[key] = t
		}
		return typeRef{named: t, dir: request}, g.visit(p, t, request)
	}
	return g.ref(p, s, request, at)
}

// content is the schema of the package's media type, or of the one media type an operation of no
// version has.
func (p *pkg) content(contents []openapi.Content) *openapi.Schema {
	for _, c := range contents {
		if c.MediaType == p.mediaType {
			return c.Schema
		}
	}
	if len(contents) == 1 && openapi.ParseMediaType(contents[0].MediaType).ApiVersion.IsZero() && strings.HasSuffix(contents[0].MediaType, "json") {
		return contents[0].Schema
	}
	return nil
}

// pageItems reads the schema of the objects of a HalPage, and the name they are embedded by.
func (g *generator) pageItems(p *pkg, page *openapi.Schema) (*openapi.Schema, error) {
	resolved, err := g.resolve(page)
	if err != nil {
		return nil, err
	}
	embedded, ok := resolved.Property("_embedded")
	if !ok || len(embedded.Properties) != 1 || embedded.Properties[0].Schema.Items == nil {
		return nil, fmt.Errorf("%s is no HalPage with one embedded array", page.RefName())
	}
	p.embeddedRel = embedded.Properties[0].Name
	return embedded.Properties[0].Schema.Items, nil
}

// root marks a meshObject, whose kind and apiVersion the generated client writes and checks,
// before its type is reached.
func (g *generator) root(p *pkg, s *openapi.Schema, dir direction) (typeRef, bool, error) {
	entity := s
	if strings.HasPrefix(s.RefName(), "HalEntityModel") {
		model, err := g.schema(s.RefName())
		if err != nil {
			return typeRef{}, false, err
		}
		if len(model.AllOf) > 0 {
			entity = model.AllOf[0]
		}
	}
	if entity.Ref == "" {
		if isNamed(entity) || len(entity.AllOf) > 0 {
			return typeRef{}, false, errors.New("an inline schema has no component to name its Go type by")
		}
		r, err := g.ref(p, s, dir, Location{})
		return r, false, err
	}
	resolved, err := g.resolve(entity)
	if err != nil {
		return typeRef{}, false, err
	}
	meshObject := isSingleValue(resolved, "kind") && isSingleValue(resolved, "apiVersion")
	if meshObject {
		t := g.componentType(g.canonical(entity.RefName()), resolved)
		if len(t.reachers) > 0 && !t.meshObject {
			return typeRef{}, false, fmt.Errorf("%s is a meshObject, and reached before as a part of another one", t.component)
		}
		t.meshObject = true
	}
	r, err := g.ref(p, s, dir, Location{})
	return r, meshObject, err
}

func isSingleValue(s *openapi.Schema, property string) bool {
	value, ok := s.Property(property)
	return ok && len(value.Enum) == 1
}
