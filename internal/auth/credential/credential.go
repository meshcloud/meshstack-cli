package credential

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"strings"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

type Credential interface {
	// Identity returns the Identity of a credential, see Credentials.SetIdentity
	Identity() Identity
	// CachedToken returns a cached token or found is false if not present.
	CachedToken(ctx context.Context) (token jwt.JWT, found bool)
	// RefreshCachedToken ensures that the CachedToken is re-minted and
	// thus subsequent call to CachedToken() is guaranteed to return token (found always true).
	RefreshCachedToken(ctx context.Context, client http.Client) error
}

type Credentials struct {
	ApiKey *ApiKey `json:"apiKey,omitempty"`
	Manual *Manual `json:"manual,omitempty"`
}

func (cs *Credentials) SetIdentity(cred Credential) {
	cs.withFieldFor(cred, func(_ Name, storedValue reflect.Value) {
		adoptCacheIfIdentityMatches(storedValue, cred)
		storedValue.Set(reflect.ValueOf(cred))
	})
}

func (cs *Credentials) withFieldFor(credential Credential, action func(name Name, credValue reflect.Value)) {
	target := reflect.TypeOf(credential)
	for name, field := range cs.fields() {
		if field.Type() == target {
			action(name, field)
			return
		}
	}
	panic(fmt.Sprintf("credential %T is not a field of %T", credential, *cs))
}

func (cs *Credentials) fields() iter.Seq2[Name, reflect.Value] {
	return func(yield func(Name, reflect.Value) bool) {
		for field, value := range reflect.ValueOf(cs).Elem().Fields() {
			if value.Kind() != reflect.Pointer {
				continue
			}
			if !yield(Name(getJsonKey(field)), value) {
				return
			}
		}
	}
}

func getJsonKey(field reflect.StructField) (jsonKey string) {
	jsonKey, _, _ = strings.Cut(field.Tag.Get("json"), ",")
	return
}
