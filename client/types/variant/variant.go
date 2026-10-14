package variant

import (
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
	_ json.Unmarshaler = (*Variant[int, string])(nil)
	_ json.Marshaler   = Variant[int, string]{}
)

// wireCompatibility repeats the options internal/json marshals every request with: a v1-style
// MarshalJSON receives none of its caller's. A Y decoded from JSON is a map, so without
// Deterministic it would leave this method in a random member order.
var wireCompatibility = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)

func (v Variant[X, Y]) MarshalJSON() ([]byte, error) {
	if v.HasX() {
		return json.Marshal(v.X, wireCompatibility)
	} else if v.HasY() {
		return json.Marshal(v.Y, wireCompatibility)
	}
	return json.Marshal(nil)
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

func (v *Variant[X, Y]) UnmarshalJSON(bytes []byte) error {
	errX := json.Unmarshal(bytes, &v.X)
	errY := json.Unmarshal(bytes, &v.Y)
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
