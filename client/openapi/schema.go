package openapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
)

// Schema is an OpenAPI 3.0 schema object, with the members meshStack's API docs use.
type Schema struct {
	Ref         string     `json:"$ref"`
	Type        string     `json:"type"`
	Format      string     `json:"format"`
	Description string     `json:"description"`
	Required    []string   `json:"required"`
	Properties  Properties `json:"properties"`
	Items       *Schema    `json:"items"`
	// AdditionalProperties is nil for none and for false, and an empty schema for true.
	AdditionalProperties *Schema        `json:"-"`
	AllOf                []*Schema      `json:"allOf"`
	OneOf                []*Schema      `json:"oneOf"`
	Discriminator        *Discriminator `json:"discriminator"`
	Enum                 []any          `json:"enum"`
	Nullable             bool           `json:"nullable"`
	ReadOnly             bool           `json:"readOnly"`
	WriteOnly            bool           `json:"writeOnly"`
	Deprecated           bool           `json:"deprecated"`
	// DeprecatedDescription says why a Deprecated schema is, where meshStack states it.
	DeprecatedDescription string      `json:"x-deprecated-description"`
	Versions              []Lifecycle `json:"x-meshstack-versions"`
}

type Discriminator struct {
	PropertyName string `json:"propertyName"`
	// Mapping is in the order of the document.
	Mapping []DiscriminatorMapping `json:"-"`
}

type DiscriminatorMapping struct {
	Value string
	Ref   string
}

type Properties []Property

type Property struct {
	Name   string
	Schema *Schema
}

func (s *Schema) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type plain Schema
	var wire struct {
		plain `json:",embed"`

		AdditionalProperties jsontext.Value `json:"additionalProperties"`
	}
	if err := json.UnmarshalDecode(dec, &wire); err != nil {
		return err
	}
	*s = Schema(wire.plain)
	if kind := wire.AdditionalProperties.Kind(); kind == 't' || kind == '{' {
		s.AdditionalProperties = &Schema{}
	}
	if wire.AdditionalProperties.Kind() == '{' {
		return json.Unmarshal(wire.AdditionalProperties, s.AdditionalProperties)
	}
	return nil
}

func (d *Discriminator) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var wire struct {
		PropertyName string `json:"propertyName"`
		Mapping      object `json:"mapping"`
	}
	if err := json.UnmarshalDecode(dec, &wire); err != nil {
		return err
	}
	d.PropertyName = wire.PropertyName
	for _, m := range wire.Mapping {
		var ref string
		if err := json.Unmarshal(m.value, &ref); err != nil {
			return err
		}
		d.Mapping = append(d.Mapping, DiscriminatorMapping{m.name, ref})
	}
	return nil
}

func (p *Properties) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var members object
	if err := members.UnmarshalJSONFrom(dec); err != nil {
		return err
	}
	*p = (*p)[:0]
	for _, m := range members {
		var schema Schema
		if err := json.Unmarshal(m.value, &schema); err != nil {
			return fmt.Errorf("property %s: %w", m.name, err)
		}
		*p = append(*p, Property{m.name, &schema})
	}
	return nil
}

func (s *Schema) Property(name string) (*Schema, bool) {
	for _, property := range s.Properties {
		if property.Name == name {
			return property.Schema, true
		}
	}
	return nil, false
}

func (s *Schema) IsRequired(property string) bool {
	return slices.Contains(s.Required, property)
}

// RefName is the name of the component a $ref points to, or empty.
func (s *Schema) RefName() string {
	_, name, _ := strings.CutLast(s.Ref, "/")
	return name
}

// Schema reads the schema component of the name.
func (s Spec) Schema(name string) (*Schema, error) {
	for _, c := range s.components {
		if c.kind == "schemas" && c.name == name {
			var schema Schema
			if err := json.Unmarshal(c.value, &schema); err != nil {
				return nil, fmt.Errorf("schema %s: %w", name, err)
			}
			return &schema, nil
		}
	}
	return nil, fmt.Errorf("the API docs have no schema %s", name)
}
