package openapi

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
)

// OperationDoc is what the API docs say about an operation, for a person to read.
type OperationDoc struct {
	Summary       string
	Description   string
	Parameters    []Parameter
	RequestBodies []Body
	Responses     []Response
}

type Parameter struct {
	Name, In    string
	Required    bool
	Type        string
	Description string
}

type Response struct {
	Status, Description string
	Bodies              []Body
}

// Body is the content of one media type.
type Body struct {
	MediaType string
	Fields    []Field
}

// Field is a property anywhere in a body. Its Path joins the property names from the top with dots,
// and writes the items of an array as [], as in spec.parentBuildingBlockRefs[].uuid, and the values
// of a map as the name *, as the API docs do.
type Field struct {
	Path        string
	Type        string
	Required    bool
	Description string
}

func (f Field) Name() string {
	return f.Path[strings.LastIndex(f.Path, ".")+1:]
}

func (f Field) Depth() int {
	return strings.Count(f.Path, ".")
}

// Doc reads the operation of a Spec, or of a selection of it, which keeps every component.
func (s Spec) Doc(operation Operation) (OperationDoc, error) {
	var parsed struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Parameters  []struct {
			Description string `json:"description"`
		} `json:"parameters"`
		Responses object `json:"responses"`
	}
	if err := json.Unmarshal(operation.raw, &parsed); err != nil {
		return OperationDoc{}, err
	}
	schemas, err := operation.Schemas()
	if err != nil {
		return OperationDoc{}, err
	}
	doc := OperationDoc{Summary: parsed.Summary, Description: parsed.Description}
	for i, parameter := range schemas.Parameters {
		schema := cmp.Or(parameter.Schema, &Schema{})
		doc.Parameters = append(doc.Parameters, Parameter{
			Name:        parameter.Name,
			In:          parameter.In,
			Required:    parameter.Required,
			Type:        schema.typeName(nil, ""),
			Description: parsed.Parameters[i].Description,
		})
	}
	if doc.RequestBodies, err = s.bodies(schemas.RequestBody); err != nil {
		return doc, fmt.Errorf("request body: %w", err)
	}
	for i, response := range schemas.Responses {
		var described struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(parsed.Responses[i].value, &described); err != nil {
			return doc, fmt.Errorf("response %s: %w", response.Status, err)
		}
		bodies, err := s.bodies(response.Content)
		if err != nil {
			return doc, fmt.Errorf("response %s: %w", response.Status, err)
		}
		doc.Responses = append(doc.Responses, Response{Status: response.Status, Description: described.Description, Bodies: bodies})
	}
	return doc, nil
}

func (s Spec) bodies(contents []Content) ([]Body, error) {
	var bodies []Body
	for _, content := range contents {
		body := Body{MediaType: content.MediaType}
		if content.Schema != nil {
			var err error
			if body.Fields, err = s.fields("", content.Schema, nil); err != nil {
				return nil, fmt.Errorf("%s: %w", content.MediaType, err)
			}
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

// fields stops at a $ref it is already inside of, because a schema may contain itself.
func (s Spec) fields(path string, schema *Schema, refs []string) ([]Field, error) {
	resolved, refs := s.resolve(schema, refs)
	if resolved.Ref != "" {
		return nil, nil
	}
	if resolved.Items != nil {
		return s.fields(path+"[]", resolved.Items, refs)
	}
	var fields []Field
	for _, property := range resolved.Properties {
		child, _ := s.resolve(property.Schema, refs)
		field := Field{
			Path:        strings.TrimPrefix(path+"."+property.Name, "."),
			Type:        child.typeName(s.itemsOf(child, refs), property.Schema.RefName()),
			Required:    resolved.IsRequired(property.Name),
			Description: child.Description,
		}
		below, err := s.fields(field.Path, property.Schema, refs)
		if err != nil {
			return nil, err
		}
		fields = append(append(fields, field), below...)
	}
	return fields, nil
}

// resolve follows $ref into the components. A schema it cannot follow keeps its Ref.
func (s Spec) resolve(schema *Schema, refs []string) (*Schema, []string) {
	resolved := schema
	for resolved.Ref != "" && !slices.Contains(refs, resolved.Ref) {
		target, err := s.Schema(resolved.RefName())
		if err != nil {
			return resolved, refs
		}
		refs = append(slices.Clip(refs), resolved.Ref)
		target.Description = cmp.Or(resolved.Description, target.Description)
		resolved = target
	}
	return resolved, refs
}

func (s Spec) itemsOf(array *Schema, refs []string) *Schema {
	if array.Items == nil {
		return nil
	}
	items, _ := s.resolve(array.Items, refs)
	if items.Ref == "" && array.Items.Ref != "" {
		named := *items
		named.Ref = array.Items.Ref
		return &named
	}
	return items
}

// typeName names the component a schema was resolved from, given as resolvedFrom.
func (s *Schema) typeName(items *Schema, resolvedFrom string) string {
	name := s.Type
	if component := cmp.Or(s.RefName(), resolvedFrom); component != "" {
		name = component
	}
	if s.Format != "" {
		name += " (" + s.Format + ")"
	}
	if items != nil {
		return strings.TrimSpace(name + " of " + items.typeName(nil, ""))
	}
	return name
}
