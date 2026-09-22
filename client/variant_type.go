package client

import (
	"errors"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

type variantCandidate[T ~string] struct {
	Type  enum.Entry[T]
	IsSet bool
}

func variant[T ~string](typ enum.Entry[T], isSet bool) variantCandidate[T] {
	return variantCandidate[T]{Type: typ, IsSet: isSet}
}

func inferVariantType[T ~string](candidates ...variantCandidate[T]) (enum.Entry[T], error) {
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
