package openapi

import (
	"encoding/json/jsontext"
	"fmt"
)

// object keeps the order of its members, which a map would lose, so that a part of the document
// prints as the document has it.
//
//nolint:recvcheck // MarshalJSONTo takes a value, so that json.Marshal finds it on an object that is not addressable
type object []member

type member struct {
	name  string
	value jsontext.Value
}

func (o *object) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if token, err := dec.ReadToken(); err != nil {
		return err
	} else if token.Kind() != '{' {
		return fmt.Errorf("expected an object, got %s", token.Kind())
	}
	*o = (*o)[:0]
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return err
		}
		// The token is valid only until the next read.
		name := token.String()
		value, err := dec.ReadValue()
		if err != nil {
			return err
		}
		*o = append(*o, member{name, value.Clone()})
	}
	_, err := dec.ReadToken()
	return err
}

func (o object) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, m := range o {
		if err := enc.WriteToken(jsontext.String(m.name)); err != nil {
			return err
		}
		if err := enc.WriteValue(m.value); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

func (o object) get(name string) (jsontext.Value, bool) {
	for _, m := range o {
		if m.name == name {
			return m.value, true
		}
	}
	return nil, false
}
