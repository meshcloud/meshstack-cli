package generator

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

//go:embed generated.go.tmpl
var fileTemplate string

var tmpl = template.Must(template.New("file").Funcs(template.FuncMap{
	"comment": comment,
	"quote":   strconv.Quote,
}).Parse(fileTemplate))

type fileData struct {
	Header      string
	Doc         string
	Package     string
	StdImports  []string
	Imports     []string
	Kind        string
	ApiVersion  string
	MediaType   string
	Collection  string
	EmbeddedRel string
	Operations  []operationData
	Queries     []typeData
	Types       []typeData
}

type operationData struct {
	Name, Doc, Params, Returns, Result, Call, Body string
	List                                           bool
}

type typeData struct {
	Name, Doc, Kind string
	Fields          []fieldData
	Values          []enumValue
	Plural          string
	DiscField       string
	DiscType        string
	DiscJSON        string
	Wrap            bool
	Cases           []caseData
}

type fieldData struct {
	Name, Type, Tag, Doc string
}

type enumValue struct {
	Const, Value string
}

type caseData struct {
	Value, Field, Type, JSON string
}

type renderer struct {
	g       *generator
	p       *pkg
	imports map[string]bool
}

func (g *generator) render(p *pkg) ([]byte, error) {
	r := renderer{g: g, p: p, imports: map[string]bool{}}
	data := fileData{
		Header:      header,
		Package:     p.Name,
		Kind:        p.Kind,
		ApiVersion:  p.ApiVersion,
		MediaType:   p.mediaType,
		Collection:  p.collection,
		EmbeddedRel: p.embeddedRel,
	}
	if p.Kind != "" {
		data.Doc = fmt.Sprintf("Package %s is the meshObject kind %s in API version %s.", p.Name, p.Kind, p.ApiVersion)
	}
	for _, o := range p.operations {
		data.Operations = append(data.Operations, r.operation(o))
		if o.queryType != "" {
			query := typeData{Name: o.queryType, Kind: "object"}
			for _, q := range o.query {
				query.Fields = append(query.Fields, fieldData{Name: q.goName, Type: q.goType, Tag: fmt.Sprintf("`json:%q`", q.name)})
			}
			data.Queries = append(data.Queries, query)
		}
	}
	if len(p.operations) > 0 {
		r.imports["context"] = true
		r.imports[runtimeImport] = true
	}
	for _, d := range p.types {
		data.Types = append(data.Types, r.typeData(d))
	}
	for _, imp := range slices.Sorted(maps.Keys(r.imports)) {
		if first, _, _ := strings.Cut(imp, "/"); strings.Contains(first, ".") {
			data.Imports = append(data.Imports, imp)
		} else {
			data.StdImports = append(data.StdImports, imp)
		}
	}
	var source bytes.Buffer
	if err := tmpl.Execute(&source, data); err != nil {
		return nil, err
	}
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%w in\n%s", err, source.String())
	}
	return formatted, nil
}

func (r renderer) expr(t typeRef) string {
	switch {
	case t.slice != nil:
		return "[]" + r.expr(*t.slice)
	case t.mapOf != nil:
		return "map[string]" + r.expr(*t.mapOf)
	case t.goType != nil:
		if t.goType.Import != "" {
			r.imports[t.goType.Import] = true
		}
		return t.goType.Expr
	}
	d := t.named.decls[t.dir]
	if d.home == r.p {
		return d.name
	}
	r.imports[d.home.importPath] = true
	return d.home.Name + "." + d.name
}

func (r renderer) operation(o *operation) operationData {
	params := []string{"ctx context.Context"}
	pathArgs := []string{strconv.Quote(o.pathTemplate)}
	for _, p := range o.pathParams {
		params = append(params, p.goName+" "+p.goType)
		if p.uuid {
			r.imports["uuid"] = true
			pathArgs = append(pathArgs, p.goName+".String()")
		} else {
			pathArgs = append(pathArgs, p.goName)
		}
	}
	call := []string{"Method: " + strconv.Quote(o.method)}
	if len(o.pathParams) > 0 {
		call = append(call, "Path: internal.Path("+strings.Join(pathArgs, ", ")+")")
	} else {
		call = append(call, "Path: "+pathArgs[0])
	}
	if o.queryType != "" {
		name := "query"
		if o.list {
			name = "filter"
		}
		params = append(params, name+" "+o.queryType)
		call = append(call, "Query: "+name)
	}
	data := operationData{Name: o.name, Doc: o.doc, List: o.list, Body: "nil", Returns: "error"}
	if o.body != nil {
		params = append(params, "body "+r.expr(*o.body))
		data.Body = "body"
	}
	if o.sendsMeshObject {
		call = append(call, "SendsMeshObject: true")
	}
	if o.answersMeshObject {
		call = append(call, "AnswersMeshObject: true")
	}
	if o.result != nil {
		data.Result = r.expr(*o.result)
		data.Returns = "(" + data.Result + ", error)"
	}
	if o.list {
		r.imports["iter"] = true
		data.Returns = "iter.Seq2[" + data.Result + ", error]"
	}
	data.Params = strings.Join(params, ", ")
	data.Call = "internal.Call{" + strings.Join(call, ", ") + "}"
	return data
}

func (r renderer) typeData(d *decl) typeData {
	t := d.t
	data := typeData{Name: d.name, Doc: t.schema.Description}
	dir := d.dir
	if dir == 0 {
		dir = t.dirs()[len(t.dirs())-1]
	}
	switch t.kind {
	case enumType:
		r.imports[enumImport] = true
		data.Kind, data.Plural = "enum", plural(d.name)
		for _, value := range t.values {
			data.Values = append(data.Values, enumValue{d.name + goName(value), value})
		}
	case unionType:
		u := t.unions[dir]
		data.Kind, data.Wrap = "union", u.wrap
		data.DiscField, data.DiscType, data.DiscJSON = u.field, r.expr(u.discType), u.property
		if !u.wrap {
			for _, imp := range []string{"encoding/json/jsontext", "encoding/json/v2", "fmt"} {
				r.imports[imp] = true
			}
		}
		for _, c := range u.cases {
			data.Cases = append(data.Cases, caseData{c.value, c.field, r.expr(c.ref), c.json})
		}
	default:
		data.Kind = "object"
		for _, f := range t.fields[dir] {
			data.Fields = append(data.Fields, r.field(f))
		}
		if t.links && d.dir != request {
			data.Fields = append(data.Fields, fieldData{Name: "Links", Type: r.expr(typeRef{goType: &r.g.links}), Tag: "`json:\"_links,omitzero\"`"})
		}
	}
	return data
}

func (r renderer) field(f field) fieldData {
	typ := r.expr(f.ref)
	if f.pointer {
		typ = "*" + typ
	}
	tag := f.json
	if f.omitZero {
		tag += ",omitzero"
	}
	return fieldData{Name: f.name, Type: typ, Tag: fmt.Sprintf("`json:%q`", tag), Doc: f.doc}
}

func plural(name string) string {
	switch {
	case strings.HasSuffix(name, "s"), strings.HasSuffix(name, "x"):
		return name + "es"
	case strings.HasSuffix(name, "y") && !strings.ContainsAny(name[len(name)-2:len(name)-1], "aeiou"):
		return name[:len(name)-1] + "ies"
	}
	return name + "s"
}

func comment(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
		b.WriteString(strings.TrimRight("// "+line, " ") + "\n")
	}
	return b.String()
}
