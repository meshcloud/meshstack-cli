package generator

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/openapi"
)

type direction uint8

const (
	request direction = 1 << iota
	response
)

func (d direction) suffix() string {
	if d == request {
		return "Request"
	}
	return "Response"
}

type generator struct {
	cfg      Config
	spec     openapi.Spec
	schemas  map[string]*openapi.Schema
	packages []*pkg
	shared   *pkg
	// types holds every schema that becomes a Go type, by component name or inline location.
	types map[string]*namedType
	links GoType
}

type pkg struct {
	Package

	importPath string
	mediaType  string
	// collection is the path the kind's objects are below, such as /api/meshobjects/meshworkspaces.
	collection  string
	embeddedRel string
	lifecycle   *openapi.Lifecycle
	operations  []*operation
	types       []*decl
	names       map[string]bool
}

type typeKind int

const (
	objectType typeKind = iota
	enumType
	unionType
)

// namedType is a schema that becomes a Go type: an object, an enum or a discriminated union. It
// becomes one decl, or one per direction when its request and response differ.
type namedType struct {
	component string
	// loc is where an inline schema occurs, and the component itself otherwise.
	loc      Location
	schema   *openapi.Schema
	kind     typeKind
	reachers map[direction]map[*pkg]bool
	fields   map[direction][]field
	unions   map[direction]*union
	values   []string
	// links marks the X of a HalEntityModel<X>, which gets the _links of the entity model.
	links bool
	// meshObject marks a body or answer, whose kind and apiVersion the generated client adds.
	meshObject bool
	// dropProperty is the discriminator of the union this is a variant of. The union writes it.
	dropProperty    string
	reachedDirectly bool
	split           bool
	decls           map[direction]*decl
}

type field struct {
	name, json string
	ref        typeRef
	pointer    bool
	omitZero   bool
	doc        string
}

type union struct {
	property string
	field    string
	discType typeRef
	// wrap is the shape {type: "aws", aws: {...}}, whose variant is the property named by the
	// discriminator's value.
	wrap  bool
	cases []unionCase
}

type unionCase struct {
	value, field, json string
	ref                typeRef
}

type typeRef struct {
	goType *GoType
	named  *namedType
	dir    direction
	slice  *typeRef
	mapOf  *typeRef
}

func (r typeRef) container() bool {
	return r.slice != nil || r.mapOf != nil || (r.goType != nil && r.goType.Container)
}

type operation struct {
	name         string
	doc          string
	method       string
	pathTemplate string
	pathParams   []param
	// pathArgs fill the parameters of pathTemplate, in its order.
	pathArgs          [][]argPart
	deprecation       string
	queryType         string
	query             []param
	body              *typeRef
	sendsMeshObject   bool
	result            *typeRef
	answersMeshObject bool
	list              bool
}

type param struct {
	name, goName, goType, doc string
	ref                       typeRef
}

func newGenerator(spec openapi.Spec, cfg Config) *generator {
	cfg.Runtime = cmp.Or(cfg.Runtime, DefaultRuntime)
	shared := &pkg{Path: cfg.Shared, Name: path.Base(cfg.Shared), names: map[string]bool{}}
	shared.importPath = path.Join(cfg.Module, cfg.Shared)
	return &generator{
		cfg:     cfg,
		spec:    spec,
		schemas: map[string]*openapi.Schema{},
		shared:  shared,
		types:   map[string]*namedType{},
		links:   GoType{Expr: shared.Name + ".Links", Import: shared.importPath, Container: true},
	}
}

func (g *generator) schema(name string) (*openapi.Schema, error) {
	if s, ok := g.schemas[name]; ok {
		return s, nil
	}
	s, err := g.spec.Schema(name)
	if err != nil {
		return nil, err
	}
	g.schemas[name] = s
	return s, nil
}

var duplicateSuffixRe = regexp.MustCompile(`_\d+$`)

// canonical names a component X_0 as X when both are the same schema: the spec numbers a schema
// it meets again in another context.
func (g *generator) canonical(name string) string {
	base := duplicateSuffixRe.ReplaceAllString(name, "")
	if base == name {
		return name
	}
	a, errA := g.schema(name)
	b, errB := g.schema(base)
	if errA != nil || errB != nil {
		return name
	}
	jsonA, _ := json.Marshal(a)
	jsonB, _ := json.Marshal(b)
	if string(jsonA) == string(jsonB) {
		return base
	}
	return name
}

func (g *generator) resolve(s *openapi.Schema) (*openapi.Schema, error) {
	for {
		switch {
		case s.Ref != "":
			target, err := g.schema(g.canonical(s.RefName()))
			if err != nil {
				return nil, err
			}
			s = target
		case len(s.AllOf) == 1 && len(s.Properties) == 0:
			s = s.AllOf[0]
		default:
			return s, nil
		}
	}
}

func isNamed(s *openapi.Schema) bool {
	return len(s.Properties) > 0 || len(s.Enum) > 0 || (len(s.OneOf) > 0 && s.Discriminator != nil)
}

func (g *generator) componentType(name string, s *openapi.Schema) *namedType {
	if t, ok := g.types[name]; ok {
		return t
	}
	t := &namedType{component: name, loc: Location{Schema: name}, schema: s, kind: kindOf(s)}
	g.types[name] = t
	return t
}

func (g *generator) inlineType(at Location, s *openapi.Schema) *namedType {
	key := "#" + at.String()
	if t, ok := g.types[key]; ok {
		return t
	}
	t := &namedType{loc: at, schema: s, kind: kindOf(s)}
	g.types[key] = t
	return t
}

func kindOf(s *openapi.Schema) typeKind {
	switch {
	case len(s.Enum) > 0:
		return enumType
	case len(s.OneOf) > 0:
		return unionType
	default:
		return objectType
	}
}

// ref reaches the schema from p in the direction, and returns its Go type.
func (g *generator) ref(p *pkg, s *openapi.Schema, dir direction, at Location) (typeRef, error) {
	if g.cfg.MapType != nil {
		if mapped, ok := g.cfg.MapType(at, s); ok {
			return typeRef{goType: &mapped}, nil
		}
	}
	switch {
	case s.Ref != "":
		name := g.canonical(s.RefName())
		if name == "HalLinks" {
			return typeRef{goType: &g.links}, nil
		}
		target, err := g.schema(name)
		if err != nil {
			return typeRef{}, err
		}
		if strings.HasPrefix(name, "HalEntityModel") {
			return g.entityModel(p, name, target, dir)
		}
		if !isNamed(target) {
			return g.ref(p, target, dir, at)
		}
		t := g.componentType(name, target)
		t.reachedDirectly = true
		return typeRef{named: t, dir: dir}, g.visit(p, t, dir)
	case len(s.AllOf) == 1 && len(s.Properties) == 0:
		return g.ref(p, s.AllOf[0], dir, at)
	case len(s.AllOf) > 1:
		return typeRef{}, fmt.Errorf("%s: allOf of %d schemas is only known for a HalEntityModel", at, len(s.AllOf))
	case len(s.OneOf) > 0 && s.Discriminator != nil:
		return typeRef{}, fmt.Errorf("%s: a oneOf with a discriminator must be a component, to name its Go type", at)
	case len(s.OneOf) > 0:
		return typeRef{}, fmt.Errorf("%s: oneOf without a discriminator has no Go type; the TypeMapper must map it", at)
	case len(s.Enum) > 0 || len(s.Properties) > 0:
		t := g.inlineType(at, s)
		return typeRef{named: t, dir: dir}, g.visit(p, t, dir)
	case s.Type == "array":
		if s.Items == nil {
			return typeRef{}, fmt.Errorf("%s: array without items", at)
		}
		items, err := g.ref(p, s.Items, dir, at)
		return typeRef{slice: &items}, err
	case s.AdditionalProperties != nil:
		values, err := g.ref(p, s.AdditionalProperties, dir, at)
		return typeRef{mapOf: &values}, err
	}
	primitive, err := primitiveOf(s, at)
	return typeRef{goType: &primitive}, err
}

func primitiveOf(s *openapi.Schema, at Location) (GoType, error) {
	switch s.Type + " " + s.Format {
	case "string uuid":
		return GoType{Expr: "uuid.UUID", Import: "uuid"}, nil
	case "string date-time":
		return GoType{Expr: "time.Time", Import: "time"}, nil
	case "string binary":
		return GoType{Expr: "[]byte", Container: true}, nil
	case "integer int32":
		return GoType{Expr: "int32"}, nil
	case "number float":
		return GoType{Expr: "float32"}, nil
	}
	switch s.Type {
	case "string":
		return GoType{Expr: "string"}, nil
	case "integer":
		return GoType{Expr: "int64"}, nil
	case "number":
		return GoType{Expr: "float64"}, nil
	case "boolean":
		return GoType{Expr: "bool"}, nil
	case "object":
		return GoType{Expr: "map[string]any", Container: true}, nil
	case "":
		return GoType{Expr: "any", Container: true}, nil
	}
	return GoType{}, fmt.Errorf("%s: no Go type for a schema of type %q", at, s.Type)
}

// entityModel unwraps HalEntityModel<X> = allOf [X, {_links}] to X, which gets the links.
func (g *generator) entityModel(p *pkg, name string, s *openapi.Schema, dir direction) (typeRef, error) {
	if len(s.AllOf) == 0 || s.AllOf[0].Ref == "" {
		return typeRef{}, fmt.Errorf("%s: expected allOf [$ref, {_links}]", name)
	}
	r, err := g.ref(p, s.AllOf[0], dir, Location{Schema: name})
	if err != nil {
		return r, err
	}
	if r.named == nil || r.named.kind != objectType {
		return r, fmt.Errorf("%s: the entity of a HalEntityModel must be an object", name)
	}
	r.named.links = true
	return r, nil
}

func (g *generator) visit(p *pkg, t *namedType, dir direction) error {
	if t.reachers == nil {
		t.reachers = map[direction]map[*pkg]bool{}
		t.fields = map[direction][]field{}
		t.unions = map[direction]*union{}
	}
	if t.reachers[dir][p] {
		return nil
	}
	if t.reachers[dir] == nil {
		t.reachers[dir] = map[*pkg]bool{}
	}
	t.reachers[dir][p] = true
	if t.kind == enumType {
		t.values = t.values[:0]
		for _, value := range t.schema.Enum {
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s: enum value %v is no string", t.loc, value)
			}
			t.values = append(t.values, s)
		}
		return nil
	}
	if t.kind == unionType {
		u, err := g.union(p, t, dir)
		t.unions[dir] = u
		return err
	}
	var fields []field
	for _, property := range t.schema.Properties {
		include, err := g.includes(t, property, dir)
		if err != nil {
			return err
		}
		if !include {
			continue
		}
		f, err := g.field(p, t, property, dir)
		if err != nil {
			return err
		}
		fields = append(fields, f)
	}
	t.fields[dir] = fields
	return nil
}

func (g *generator) includes(t *namedType, property openapi.Property, dir direction) (bool, error) {
	if property.Name == t.dropProperty || (t.meshObject && (property.Name == "kind" || property.Name == "apiVersion")) {
		return false, nil
	}
	readOnly, writeOnly, err := g.access(property.Schema)
	return (!readOnly || dir != request) && (!writeOnly || dir != response), err
}

// access reads readOnly and writeOnly from the property and the component it refers to.
func (g *generator) access(s *openapi.Schema) (readOnly, writeOnly bool, err error) {
	resolved, err := g.resolve(s)
	if err != nil {
		return false, false, err
	}
	return s.ReadOnly || resolved.ReadOnly, s.WriteOnly || resolved.WriteOnly, nil
}

func (g *generator) field(p *pkg, t *namedType, property openapi.Property, dir direction) (field, error) {
	r, err := g.ref(p, property.Schema, dir, t.loc.child(property.Name))
	if err != nil {
		return field{}, err
	}
	resolved, err := g.resolve(property.Schema)
	if err != nil {
		return field{}, err
	}
	readOnly, _, _ := g.access(property.Schema)
	required := t.schema.IsRequired(property.Name) && (!readOnly || dir != request)
	optional := !required || property.Schema.Nullable || resolved.Nullable
	doc := cmp.Or(property.Schema.Description, resolved.Description)
	if property.Schema.Deprecated {
		doc = deprecated(doc)
	}
	return field{
		name:     goName(property.Name),
		json:     property.Name,
		ref:      r,
		pointer:  optional && !r.container(),
		omitZero: optional,
		doc:      doc,
	}, nil
}

func (g *generator) union(p *pkg, t *namedType, dir direction) (*union, error) {
	property := t.schema.Discriminator.PropertyName
	u := &union{property: property, field: goName(property), discType: typeRef{goType: &GoType{Expr: "string"}}}
	if len(t.schema.Discriminator.Mapping) == 0 {
		return nil, fmt.Errorf("%s: a discriminator needs its mapping", t.loc)
	}
	variants := make([]*openapi.Schema, len(t.schema.Discriminator.Mapping))
	u.wrap = true
	for i, mapping := range t.schema.Discriminator.Mapping {
		var err error
		if variants[i], err = g.resolve(&openapi.Schema{Ref: mapping.Ref}); err != nil {
			return nil, err
		}
		_, hasDiscriminator := variants[i].Property(property)
		_, hasWrapped := variants[i].Property(mapping.Value)
		u.wrap = u.wrap && hasDiscriminator && hasWrapped && len(variants[i].Properties) == 2
	}
	if first, _ := variants[0].Property(property); first != nil {
		wrapped := first
		if len(first.AllOf) == 1 {
			wrapped = first.AllOf[0]
		}
		if wrapped.Ref != "" {
			r, err := g.ref(p, wrapped, dir, t.loc.child(property))
			if err != nil {
				return nil, err
			}
			u.discType = r
		}
	}
	for i, mapping := range t.schema.Discriminator.Mapping {
		variantName := (&openapi.Schema{Ref: mapping.Ref}).RefName()
		c := unionCase{value: mapping.Value, field: goName(mapping.Value)}
		if u.wrap {
			inner := variants[i].Properties[slices.IndexFunc(variants[i].Properties, func(p openapi.Property) bool { return p.Name != property })]
			c.field, c.json = goName(inner.Name), inner.Name
			r, err := g.ref(p, inner.Schema, dir, Location{variantName, inner.Name})
			if err != nil {
				return nil, err
			}
			c.ref = r
		} else {
			vt := g.componentType(variantName, variants[i])
			if vt.kind != objectType {
				return nil, fmt.Errorf("%s: variant %s is no object", t.loc, variantName)
			}
			vt.dropProperty = property
			if err := g.visit(p, vt, dir); err != nil {
				return nil, err
			}
			c.ref = typeRef{named: vt, dir: dir}
		}
		u.cases = append(u.cases, c)
	}
	return u, nil
}

// children are the named types a type refers to in the direction.
func (t *namedType) children(dir direction) []*namedType {
	var refs []typeRef
	for _, f := range t.fields[dir] {
		refs = append(refs, f.ref)
	}
	if u := t.unions[dir]; u != nil {
		refs = append(refs, u.discType)
		for _, c := range u.cases {
			refs = append(refs, c.ref)
		}
	}
	var children []*namedType
	for len(refs) > 0 {
		r := refs[0]
		refs = refs[1:]
		switch {
		case r.named != nil:
			children = append(children, r.named)
		case r.slice != nil:
			refs = append(refs, *r.slice)
		case r.mapOf != nil:
			refs = append(refs, *r.mapOf)
		}
	}
	return children
}

func (t *namedType) dirs() []direction {
	return slices.Sorted(maps.Keys(t.reachers))
}

// ownSplit is a type reached in both directions whose fields differ between them, as by a
// readOnly or writeOnly property.
func (t *namedType) ownSplit() bool {
	if len(t.reachers) < 2 {
		return false
	}
	return !slices.EqualFunc(t.fields[request], t.fields[response], func(a, b field) bool {
		return a.json == b.json && a.pointer == b.pointer && a.omitZero == b.omitZero
	})
}
