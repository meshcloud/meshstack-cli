package client

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

// variantCandidate pairs a variant's type with the field that holds it.
type variantCandidate[T ~string] struct {
	Type  enum.Entry[T]
	Value any
}

func variant[T ~string](typ enum.Entry[T], value any) variantCandidate[T] {
	return variantCandidate[T]{Type: typ, Value: value}
}

// inferVariantType returns the type of the one candidate whose field is set. None and several are errors, so
// both a request built from an incomplete plan and a response meshStack has redacted end in a diagnostic
// instead of a crash.
func inferVariantType[T ~string](candidates ...variantCandidate[T]) (enum.Entry[T], error) {
	var result enum.Entry[T]
	for _, candidate := range candidates {
		// A variant may be a pointer to an empty struct, so nil-ness has to be checked by reflection.
		if reflect.ValueOf(candidate.Value).IsZero() {
			continue
		}
		if len(result) > 0 {
			return "", fmt.Errorf("more than one variant is set: %s and %s", result, candidate.Type)
		}
		result = candidate.Type
	}
	if len(result) == 0 {
		return "", errors.New("no variant is set")
	}
	return result, nil
}
