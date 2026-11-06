package variant

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
)

// A Variant decodes one JSON value into X or into Y. When both decode, X wins, so Y can be a type
// that takes any value: a secret comes back as a hash object in X, and a plain value in Y.
type Variant[X, Y any] struct {
	X X
	Y Y
}

var (
	_ json.UnmarshalerFrom = (*Variant[int, string])(nil)
	_ json.MarshalerTo     = Variant[int, string]{}
)

func (v Variant[X, Y]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if v.HasX() {
		return json.MarshalEncode(enc, v.X, enc.Options())
	} else if v.HasY() {
		return json.MarshalEncode(enc, v.Y, enc.Options())
	}
	return enc.WriteToken(jsontext.Null)
}

func has[T any](xy any) bool {
	v := reflect.ValueOf(xy)
	kind := reflect.TypeFor[T]().Kind()
	// For a T of type any, a decoded 0, "" or false counts as set; only JSON null leaves it unset.
	if kind != reflect.Interface {
		return !v.IsZero()
	} else {
		return v.IsValid()
	}
}

func (v Variant[X, Y]) HasX() bool {
	return has[X](v.X)
}

func (v Variant[X, Y]) HasY() bool {
	return has[Y](v.Y)
}

func (v Variant[X, Y]) WithX(action func(x *X)) {
	if v.HasX() {
		action(&v.X)
	} else {
		action(nil)
	}
}

func (v Variant[X, Y]) WithY(action func(y *Y)) {
	if v.HasY() {
		action(&v.Y)
	} else {
		action(nil)
	}
}

func (v *Variant[X, Y]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	bytes, err := dec.ReadValue()
	if err != nil {
		return err
	}
	errX := json.Unmarshal(bytes, &v.X, dec.Options())
	errY := json.Unmarshal(bytes, &v.Y, dec.Options())
	switch {
	case v.HasX() && v.HasY():
		// Y is cleared because a Y of type any decodes from every JSON value.
		var zeroY Y
		v.Y = zeroY
		return errX
	case v.HasX():
		return errX
	case v.HasY():
		return errY
	default:
		var nothing any
		if err := json.Unmarshal(bytes, &nothing); err != nil {
			return fmt.Errorf("cannot unmarshal to any: %w", err)
		}
		if nothing == nil {
			// JSON null leaves both unset, which is not an error.
			return nil
		}
		return errors.Join(fmt.Errorf("variant[%T, %T]: cannot unmarshal '%s' to any field", v.X, v.Y, string(bytes)), errX, errY)
	}
}
