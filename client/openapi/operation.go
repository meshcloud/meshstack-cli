package openapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
)

// OperationSchemas are the parameters and bodies of an operation, with their schemas.
type OperationSchemas struct {
	Parameters  []ParameterSchema
	RequestBody []Content
	Responses   []ResponseContent
}

type ParameterSchema struct {
	Name, In    string
	Required    bool
	Schema      *Schema
	Description string
	PathKey     PathKey
}

// PathKey is the field of a meshObject that a path parameter takes, such as metadata.uuid, or a
// template of several fields, such as {metadata.ownedByWorkspace}.{metadata.name}.
type PathKey struct {
	Key string `json:"x-meshstack-path-key"`
	// Name is set where the key is a template, such as workspace.project for the one above.
	Name string `json:"x-meshstack-path-key-name"`
	// ByVersion holds the keys of the API versions that take another key than Key.
	ByVersion map[string]string `json:"x-meshstack-path-key-by-version"`
}

// Content is the body of one media type.
type Content struct {
	MediaType string
	Schema    *Schema
	Lifecycle *Lifecycle
}

// Lifecycle is the state of one version of a meshObject kind.
type Lifecycle struct {
	MeshObject   string `json:"meshObject"`
	State        string `json:"state"`
	RetiredAfter string `json:"retiredAfter"`
	Notice       string `json:"notice"`
}

// Deprecation is empty for a version that is not deprecated.
func (l Lifecycle) Deprecation() string {
	var text string
	switch l.State {
	case "Outdated":
		text = "`" + l.MeshObject + "` is outdated."
	case "Deprecated":
		text = "`" + l.MeshObject + "` is deprecated."
		if l.RetiredAfter != "" {
			text = "`" + l.MeshObject + "` is deprecated and retired after " + l.RetiredAfter + "."
		}
	default:
		return ""
	}
	return strings.TrimSpace(text + " " + l.Notice)
}

// Lifecycle is that of the media type in the request body or a response, or nil.
func (s OperationSchemas) Lifecycle(mediaType string) *Lifecycle {
	bodies := slices.Clone(s.RequestBody)
	for _, response := range s.Responses {
		bodies = append(bodies, response.Content...)
	}
	for _, body := range bodies {
		if body.MediaType == mediaType && body.Lifecycle != nil {
			return body.Lifecycle
		}
	}
	return nil
}

type ResponseContent struct {
	Status  string
	Content []Content
}

func (o Operation) Schemas() (OperationSchemas, error) {
	type content struct {
		Content object `json:"content"`
	}
	var parsed struct {
		Parameters  []ParameterSchema `json:"parameters"`
		RequestBody content           `json:"requestBody"`
		Responses   object            `json:"responses"`
	}
	if err := json.Unmarshal(o.raw, &parsed); err != nil {
		return OperationSchemas{}, err
	}
	schemas := OperationSchemas{Parameters: parsed.Parameters}
	var err error
	if schemas.RequestBody, err = contents(parsed.RequestBody.Content); err != nil {
		return schemas, fmt.Errorf("request body: %w", err)
	}
	for _, response := range parsed.Responses {
		var c content
		if err := json.Unmarshal(response.value, &c); err != nil {
			return schemas, fmt.Errorf("response %s: %w", response.name, err)
		}
		bodies, err := contents(c.Content)
		if err != nil {
			return schemas, fmt.Errorf("response %s: %w", response.name, err)
		}
		schemas.Responses = append(schemas.Responses, ResponseContent{response.name, bodies})
	}
	return schemas, nil
}

func (p *ParameterSchema) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var wire struct {
		Name        string  `json:"name"`
		In          string  `json:"in"`
		Required    bool    `json:"required"`
		Schema      *Schema `json:"schema"`
		Description string  `json:"description"`
		PathKey     PathKey `json:",embed"`
	}
	if err := json.UnmarshalDecode(dec, &wire); err != nil {
		return err
	}
	*p = ParameterSchema(wire)
	return nil
}

func contents(members object) ([]Content, error) {
	var result []Content
	for _, m := range members {
		var mediaType struct {
			Schema    *Schema    `json:"schema"`
			Lifecycle *Lifecycle `json:"x-meshstack-lifecycle"`
		}
		if err := json.Unmarshal(m.value, &mediaType); err != nil {
			return nil, fmt.Errorf("%s: %w", m.name, err)
		}
		result = append(result, Content{m.name, mediaType.Schema, mediaType.Lifecycle})
	}
	return result, nil
}

// inlineRefs replaces a $ref to a response, parameter or request body component by the component,
// and adds the parameters of the path, so that an operation reads as one object.
func inlineRefs(operation, pathParameters jsontext.Value, refs map[string]jsontext.Value) (jsontext.Value, error) {
	var members object
	if err := json.Unmarshal(operation, &members); err != nil {
		return nil, err
	}
	resolve := func(value jsontext.Value) (jsontext.Value, error) {
		var ref struct {
			Ref string `json:"$ref"`
		}
		if value.Kind() != '{' {
			return value, nil
		}
		if err := json.Unmarshal(value, &ref); err != nil || ref.Ref == "" {
			return value, err
		}
		if resolved, ok := refs[ref.Ref]; ok {
			return resolved, nil
		}
		return nil, fmt.Errorf("cannot resolve %s", ref.Ref)
	}
	resolveAll := func(values []jsontext.Value) error {
		for i := range values {
			var err error
			if values[i], err = resolve(values[i]); err != nil {
				return err
			}
		}
		return nil
	}

	var parameters []jsontext.Value
	if pathParameters != nil {
		if err := json.Unmarshal(pathParameters, &parameters); err != nil {
			return nil, err
		}
	}
	hasParameters := false
	for i, m := range members {
		var err error
		switch m.name {
		case "parameters":
			hasParameters = true
			var own []jsontext.Value
			if err = json.Unmarshal(m.value, &own); err != nil {
				return nil, err
			}
			combined := append(slices.Clone(parameters), own...)
			if err = resolveAll(combined); err != nil {
				return nil, err
			}
			members[i].value, err = json.Marshal(combined)
		case "requestBody":
			members[i].value, err = resolve(m.value)
		case "responses":
			var responses object
			if err = json.Unmarshal(m.value, &responses); err != nil {
				return nil, err
			}
			for j := range responses {
				if responses[j].value, err = resolve(responses[j].value); err != nil {
					return nil, err
				}
			}
			members[i].value, err = json.Marshal(responses)
		}
		if err != nil {
			return nil, err
		}
	}
	if !hasParameters && len(parameters) > 0 {
		if err := resolveAll(parameters); err != nil {
			return nil, err
		}
		value, err := json.Marshal(parameters)
		if err != nil {
			return nil, err
		}
		members = append(members, member{"parameters", value})
	}
	return json.Marshal(members)
}
