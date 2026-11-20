package openapi

import (
	"cmp"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"

	"github.com/meshcloud/meshstack-cli/internal/json"
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
	// PathKey names the metadata field that fills a path parameter, such as metadata.uuid, or a
	// template of several in braces, such as {metadata.ownedByWorkspace}.{metadata.name}.
	PathKey string
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
// of a map as {}, as in spec.inputs{}.value.
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

type schema struct {
	Ref                  string           `json:"$ref"`
	Type                 string           `json:"type"`
	Format               string           `json:"format"`
	Description          string           `json:"description"`
	Required             []string         `json:"required"`
	Properties           object           `json:"properties"`
	Items                jsontext.Value   `json:"items"`
	AdditionalProperties jsontext.Value   `json:"additionalProperties"`
	AllOf                []jsontext.Value `json:"allOf"`
	OneOf                []jsontext.Value `json:"oneOf"`
	AnyOf                []jsontext.Value `json:"anyOf"`
	resolvedRef          string
}

// Doc reads the operation of a Spec, or of a selection of it, which keeps every component.
func (s Spec) Doc(operation Operation) (OperationDoc, error) {
	var parsed struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Parameters  []struct {
			Name             string            `json:"name"`
			In               string            `json:"in"`
			Required         bool              `json:"required"`
			Description      string            `json:"description"`
			Schema           jsontext.Value    `json:"schema"`
			PathKey          string            `json:"x-meshstack-path-key"`
			PathKeyByVersion map[string]string `json:"x-meshstack-path-key-by-version"`
		} `json:"parameters"`
		RequestBody struct {
			Content object `json:"content"`
		} `json:"requestBody"`
		Responses object `json:"responses"`
	}
	if err := json.Unmarshal(operation.raw, &parsed); err != nil {
		return OperationDoc{}, err
	}
	doc := OperationDoc{Summary: parsed.Summary, Description: parsed.Description}
	for _, parameter := range parsed.Parameters {
		typeName, err := s.typeName(parameter.Schema, nil)
		if err != nil {
			return doc, fmt.Errorf("parameter %s: %w", parameter.Name, err)
		}
		doc.Parameters = append(doc.Parameters, Parameter{
			Name:        parameter.Name,
			In:          parameter.In,
			Required:    parameter.Required,
			Type:        typeName,
			Description: parameter.Description,
			PathKey:     cmp.Or(parameter.PathKeyByVersion[operation.LatestApiVersion().String()], parameter.PathKey),
		})
	}
	var err error
	if doc.RequestBodies, err = s.bodies(parsed.RequestBody.Content); err != nil {
		return doc, fmt.Errorf("request body: %w", err)
	}
	for _, member := range parsed.Responses {
		var response struct {
			Description string `json:"description"`
			Content     object `json:"content"`
		}
		if err := json.Unmarshal(member.value, &response); err != nil {
			return doc, fmt.Errorf("response %s: %w", member.name, err)
		}
		bodies, err := s.bodies(response.Content)
		if err != nil {
			return doc, fmt.Errorf("response %s: %w", member.name, err)
		}
		doc.Responses = append(doc.Responses, Response{Status: member.name, Description: response.Description, Bodies: bodies})
	}
	return doc, nil
}

func (s Spec) bodies(c object) ([]Body, error) {
	var bodies []Body
	for _, member := range c {
		var mediaType struct {
			Schema jsontext.Value `json:"schema"`
		}
		if err := json.Unmarshal(member.value, &mediaType); err != nil {
			return nil, err
		}
		body := Body{MediaType: member.name}
		if mediaType.Schema != nil {
			var err error
			if body.Fields, err = s.fields("", mediaType.Schema, nil); err != nil {
				return nil, fmt.Errorf("%s: %w", member.name, err)
			}
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

// fields stops at a $ref it is already inside of, because a schema may contain itself.
func (s Spec) fields(path string, raw jsontext.Value, refs []string) ([]Field, error) {
	resolved, refs, err := s.resolve(raw, refs)
	if err != nil || resolved.Ref != "" {
		return nil, err
	}
	if resolved.Items != nil {
		return s.fields(path+"[]", resolved.Items, refs)
	}
	if values := resolved.mapValues(); values != nil {
		return s.fields(path+"{}", values, refs)
	}
	var fields []Field
	for _, property := range resolved.Properties {
		child, _, err := s.resolve(property.value, refs)
		if err != nil {
			return nil, err
		}
		typeName, err := s.typeName(property.value, refs)
		if err != nil {
			return nil, err
		}
		field := Field{
			Path:        strings.TrimPrefix(path+"."+property.name, "."),
			Type:        typeName,
			Required:    slices.Contains(resolved.Required, property.name),
			Description: child.Description,
		}
		below, err := s.fields(field.Path, property.value, refs)
		if err != nil {
			return nil, err
		}
		fields = append(append(fields, field), below...)
	}
	return fields, nil
}

// resolve also follows an allOf of one schema, in which the API docs wrap a $ref to describe it.
func (s Spec) resolve(raw jsontext.Value, refs []string) (schema, []string, error) {
	var resolved schema
	if err := json.Unmarshal(raw, &resolved); err != nil {
		return resolved, refs, err
	}
	var resolvedRef string
	for {
		var next jsontext.Value
		switch {
		case resolved.Ref != "" && !slices.Contains(refs, resolved.Ref):
			i := slices.IndexFunc(s.components, func(c component) bool { return c.ref() == resolved.Ref })
			if i < 0 {
				return resolved, refs, nil
			}
			refs = append(slices.Clip(refs), resolved.Ref)
			resolvedRef = resolved.Ref
			next = s.components[i].value
		case resolved.Ref == "" && len(resolved.AllOf) == 1:
			next = resolved.AllOf[0]
		default:
			resolved.resolvedRef = resolvedRef
			return resolved, refs, nil
		}
		description := resolved.Description
		resolved = schema{}
		if err := json.Unmarshal(next, &resolved); err != nil {
			return resolved, refs, err
		}
		resolved.Description = cmp.Or(description, resolved.Description)
	}
}

func (s Spec) typeName(raw jsontext.Value, refs []string) (string, error) {
	if raw == nil {
		return "", nil
	}
	resolved, refs, err := s.resolve(raw, refs)
	if err != nil {
		return "", err
	}
	if variants := slices.Concat(resolved.OneOf, resolved.AnyOf); len(variants) > 0 && resolved.name() == "" {
		names := make([]string, len(variants))
		for i, variant := range variants {
			if names[i], err = s.typeName(variant, refs); err != nil {
				return "", err
			}
		}
		return "one of " + strings.Join(names, ", "), nil
	}
	if values := resolved.mapValues(); values != nil {
		var valueType string
		valueType, err = s.typeName(values, refs)
		return "map of " + cmp.Or(valueType, "any"), err
	}
	if resolved.Items == nil {
		return resolved.name(), nil
	}
	items, err := s.typeName(resolved.Items, refs)
	return strings.TrimSpace(resolved.name() + " of " + items), err
}

func (s schema) name() string {
	name := s.Type
	if _, component, isRef := strings.CutLast(cmp.Or(s.Ref, s.resolvedRef), "/"); isRef && (s.Ref != "" || s.Type == "" || s.Type == "object") {
		name = component
	}
	if s.Format != "" {
		name += " (" + s.Format + ")"
	}
	return name
}

func (s schema) mapValues() jsontext.Value {
	switch s.AdditionalProperties.Kind() {
	case jsontext.KindTrue:
		return jsontext.Value("{}")
	case jsontext.KindBeginObject:
		return s.AdditionalProperties
	default:
		return nil
	}
}
