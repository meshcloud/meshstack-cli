package jwt

import (
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

type Claim[V any] struct {
	key       string
	converter func(v any) V
}

var (
	ExpiryClaim = Claim[Expiry]{
		key: "exp",
		converter: func(v any) (expiry Expiry) {
			// JSON numbers decode as float64, and exp counts seconds since the epoch.
			seconds, ok := v.(float64)
			if !ok {
				return
			}
			expiry.Time = time.Unix(int64(seconds), 0)
			return
		},
	}
	// WorkspaceClaim is written by keycloak's MC_CUSTOMER script mapper, which strips the c: scope
	// prefix, so this carries the identifier rather than the scope. It is absent altogether where
	// the user holds no role on the workspace the token was asked for, which is NoWorkspace here.
	WorkspaceClaim = StringClaim[meshstack.Workspace]("MC_CUSTOMER")

	ScopeClaim = Claim[scope.Scopes]{
		key: "scope",
		converter: func(v any) (scopes scope.Scopes) {
			text, _ := v.(string)
			for one := range strings.FieldsSeq(text) {
				scopes = append(scopes, scope.Scope(one))
			}
			return
		},
	}
	PreferredUsernameClaim = StringClaim[string]("preferred_username")
	EmailClaim             = StringClaim[string]("email")
	// ClientIdClaim is only on a token a client minted with its own credentials, such as an API key.
	ClientIdClaim = StringClaim[string]("client_id")
)

// StringClaim reads a claim written as a JSON string, and is the zero value of V where the claim
// is absent or is not a string.
func StringClaim[V ~string](key string) Claim[V] {
	return Claim[V]{key: key, converter: func(claim any) V {
		value, _ := claim.(string)
		return V(value)
	}}
}

// getFrom is the zero value of V for a Claim with no converter. A claim's JSON value rarely has
// the type V itself, so asserting on it would pass off a wrong value as a missing one.
func (c Claim[V]) getFrom(jwt JWT) (value V) {
	if c.converter == nil {
		return
	}
	return c.converter(jwt.claims[c.key])
}

type Expiry struct {
	time.Time
}

func (expiry Expiry) Expired(grace time.Duration) bool {
	if expiry == (Expiry{}) {
		// A token that carries no exp claim counts as expired.
		return true
	}
	return time.Until(expiry.Time) <= grace
}
