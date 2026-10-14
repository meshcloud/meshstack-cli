package types

import (
	"reflect"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/types/variant"
)

type (
	Set[T any] []T

	Secret struct {
		// Plaintext is sent only to create or rotate the secret; a response never carries it.
		Plaintext *string `json:"plaintext,omitzero" tfsdk:"plaintext"`
		// Hash is in every response, and a request sends it to keep the secret unchanged.
		Hash *string `json:"hash,omitzero" tfsdk:"-"`
	}

	SecretOrAny = variant.Variant[Secret, any]

	Any any
)

// IsSet ignores the element type of the Set.
func IsSet(other reflect.Type) bool {
	setType := reflect.TypeFor[Set[any]]()
	if other.PkgPath() == setType.PkgPath() {
		stripGenerics := func(s string) string {
			if startIdx := strings.Index(s, "["); startIdx > 0 {
				return s[0 : startIdx-1]
			}
			return s
		}
		if stripGenerics(other.Name()) == stripGenerics(setType.Name()) {
			return true
		}
	}
	return false
}
