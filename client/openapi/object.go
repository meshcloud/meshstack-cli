package openapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
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

func (o object) without(name string) object {
	return slices.DeleteFunc(o, func(m member) bool { return m.name == name })
}

// update replaces the object at name, where there is one.
func (o object) update(name string, f func(object) (object, error)) error {
	for i, m := range o {
		if m.name != name {
			continue
		}
		var value object
		if err := json.Unmarshal(m.value, &value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		updated, err := f(value)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if o[i].value, err = json.Marshal(updated); err != nil {
			return err
		}
	}
	return nil
}

// updateAll replaces each member that is an object, and leaves any other value as it is.
func (o object) updateAll(f func(object) (object, error)) error {
	for _, m := range o {
		if m.value.Kind() != '{' {
			continue
		}
		if err := o.update(m.name, f); err != nil {
			return err
		}
	}
	return nil
}

// updateString replaces the string at name, where there is one.
func (o object) updateString(name string, f func(string) string) error {
	for i, m := range o {
		var value string
		if m.name != name || json.Unmarshal(m.value, &value) != nil {
			continue
		}
		var err error
		if o[i].value, err = json.Marshal(f(value)); err != nil {
			return err
		}
	}
	return nil
}
