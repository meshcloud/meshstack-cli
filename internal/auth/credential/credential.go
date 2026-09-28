package credential

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"strings"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

type Credential interface {
	// Name returns a human-readable credential name (unique for all available credentials).
	Name() Name
	// Identity identifies the credential, so that a cache minted for another one is not used.
	Identity() Identity
	// CachedToken consults the cache (if present) and returns a token.
	// Token expiry must be checked and if RefreshCachedToken is then to be called if the token is expired.
	CachedToken(ctx context.Context, getWorkspace getWorkspaceFunc) (token jwt.JWT, found bool)
	// RefreshCachedToken re-mints the token, so a CachedToken call after it finds one.
	RefreshCachedToken(ctx context.Context, client http.Client, getWorkspace getWorkspaceFunc) error
}

type getWorkspaceFunc func() (meshstack.Workspace, error)

type Credentials struct {
	ApiKey    *ApiKey    `json:"apiKey,omitempty"`
	Manual    *Manual    `json:"manual,omitempty"`
	OidcLogin *OidcLogin `json:"oidcLogin,omitempty"`
}

func (cs *Credentials) Set(cred Credential) {
	cs.withFieldFor(cred, func(_ Name, storedValue reflect.Value) {
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
