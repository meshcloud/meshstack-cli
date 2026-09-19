package variant

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
)

// A Variant represents a single JSON map entry having two different Go type representations X and Y.
// After JSON unmarshalling you can check with HasX, HasY which field has been detected, while X is preferred.
// An example usage is a Client DTO response which can either be struct representing a secret hash,
// or a simple string response if that's a non-sensitive value.
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
	if kind != reflect.Interface {
		// T is not any (aka as a valid 'zero' representation)
		return !v.IsZero()
	} else {
		// T is any, so we only check for validness
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
		// Explicitly prefer X over Y and set Y to zero even if unmarshalling has also worked,
		// this supports having Y with catch-all type 'any'
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
			// support optional unmarshalling aka neither X nor Y is set
			return nil
		}
		return errors.Join(fmt.Errorf("variant[%T, %T]: cannot unmarshal '%s' to any field", v.X, v.Y, string(bytes)), errX, errY)
	}
}
