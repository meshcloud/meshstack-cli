package variant

import (
	"errors"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

// A Candidate is one variant of a type that holds exactly one of several variants, one field each.
type Candidate[T ~string] struct {
	Type  enum.Entry[T]
	IsSet bool
}

func NewCandidate[T ~string](typ enum.Entry[T], isSet bool) Candidate[T] {
	return Candidate[T]{Type: typ, IsSet: isSet}
}

// InferType returns the type of the one candidate that is set.
func InferType[T ~string](candidates ...Candidate[T]) (enum.Entry[T], error) {
	var result enum.Entry[T]
	for _, candidate := range candidates {
		if !candidate.IsSet {
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
