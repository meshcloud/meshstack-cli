// Package polymorph reads and writes a struct as Jackson's
// @JsonTypeInfo(use = NAME, include = PROPERTY, property = "type") does. The struct embeds Polymorph
// first and tags one pointer per subtype polymorph:"<type name>", with ,inline for fields next to "type".
package polymorph

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unsafe"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

type Polymorph[T any, E ~string] struct {
	Type enum.Entry[E] `tfsdk:"-"`
}

func (p *Polymorph[T, E]) MarshalJSONTo(enc *jsontext.Encoder) error {
	typeName := p.Type
	if typeName == "" {
		var err error
		if typeName, err = p.InferType(); err != nil {
			return err
		}
	}
	return p.outer().marshal(enc, string(typeName))
}

func (p *Polymorph[T, E]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	typeName, err := p.outer().unmarshal(dec)
	p.Type = enum.Entry[E](typeName)
	return err
}

func (p *Polymorph[T, E]) InferType() (enum.Entry[E], error) {
	typeName, err := p.outer().inferType()
	return enum.Entry[E](typeName), err
}

// outer is the T that embeds p. As the first field, p has the address of T.
func (p *Polymorph[T, E]) outer() polymorphic {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct || t.NumField() == 0 || t.Field(0).Type != reflect.TypeFor[Polymorph[T, E]]() {
		panic(fmt.Sprintf("%s needs polymorph.Polymorph as its first field", t))
	}
	return newPolymorphic(reflect.ValueOf((*T)(unsafe.Pointer(p))).Elem()) //nolint:gosec // the check above makes p the address of T
}

type polymorphic struct {
	value    reflect.Value
	subtypes []subtype
}

type subtype struct {
	index  int
	name   string
	member string
}

func newPolymorphic(value reflect.Value) polymorphic {
	p := polymorphic{value: value}
	for i := range value.NumField() {
		field := value.Type().Field(i)
		tag, tagged := field.Tag.Lookup("polymorph")
		if !tagged {
			continue
		}
		name, inline := strings.CutSuffix(tag, ",inline")
		s := subtype{index: i, name: name}
		if !inline {
			s.member, _, _ = strings.Cut(field.Tag.Get("json"), ",")
		}
		p.subtypes = append(p.subtypes, s)
	}
	if len(p.subtypes) == 0 {
		panic(fmt.Sprintf("%s needs a field tagged polymorph", value.Type()))
	}
	return p
}

func (p polymorphic) marshal(enc *jsontext.Encoder, typeName string) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := errors.Join(enc.WriteToken(jsontext.String("type")), enc.WriteToken(jsontext.String(typeName))); err != nil {
		return err
	}
	for _, s := range p.subtypes {
		subtype := p.value.Field(s.index)
		if subtype.IsNil() {
			continue
		}
		if err := s.marshal(enc, subtype.Interface()); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

func (p polymorphic) unmarshal(dec *jsontext.Decoder) (typeName string, _ error) {
	raw, err := dec.ReadValue()
	if err != nil {
		return "", err
	}
	p.value.SetZero()
	if raw.Kind() == jsontext.KindNull {
		return "", nil
	}
	var members map[string]jsontext.Value
	if err = json.Unmarshal(raw, &members, dec.Options()); err != nil {
		return "", err
	}
	if encodedType, ok := members["type"]; ok {
		if err = json.Unmarshal(encodedType, &typeName); err != nil {
			return "", err
		}
	}
	for _, s := range p.subtypes {
		if s.name != typeName {
			continue
		}
		field := p.value.Field(s.index)
		subtype := reflect.New(field.Type().Elem())
		encoded := raw
		if s.member != "" {
			var sent bool
			if encoded, sent = members[s.member]; !sent && subtype.Elem().NumField() > 0 {
				return typeName, nil
			}
		}
		if len(encoded) > 0 {
			if err = json.Unmarshal(encoded, subtype.Interface(), dec.Options()); err != nil {
				return typeName, fmt.Errorf("%s: %w", typeName, err)
			}
		}
		field.Set(subtype)
	}
	return typeName, nil
}

func (p polymorphic) inferType() (string, error) {
	var result string
	for _, s := range p.subtypes {
		if p.value.Field(s.index).IsNil() {
			continue
		}
		if result != "" {
			return "", fmt.Errorf("%s: more than one subtype is set: %s and %s", p.value.Type(), result, s.name)
		}
		result = s.name
	}
	if result == "" {
		return "", fmt.Errorf("%s: no subtype is set", p.value.Type())
	}
	return result, nil
}

func (s subtype) marshal(enc *jsontext.Encoder, subtype any) error {
	if s.member != "" {
		if err := enc.WriteToken(jsontext.String(s.member)); err != nil {
			return err
		}
		return json.MarshalEncode(enc, subtype, enc.Options())
	}
	encoded, err := json.Marshal(subtype, enc.Options())
	if err != nil {
		return err
	}
	fields := jsontext.NewDecoder(bytes.NewReader(encoded))
	if _, err = fields.ReadToken(); err != nil {
		return err
	}
	for fields.PeekKind() != jsontext.KindEndObject {
		name, err := fields.ReadToken()
		if err != nil {
			return err
		}
		if err = enc.WriteToken(name); err != nil {
			return err
		}
		fieldValue, err := fields.ReadValue()
		if err != nil {
			return err
		}
		if err = enc.WriteValue(fieldValue); err != nil {
			return err
		}
	}
	return nil
}
