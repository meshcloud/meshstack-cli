package scope

import "strings"

type (
	Scope  string
	Scopes []Scope
)

// The standard scopes. offline_access is an optional client scope, so it has to be
// named or a login lasts as long as one access token.
const (
	OpenId        Scope = "openid"
	Profile       Scope = "profile"
	Email         Scope = "email"
	OfflineAccess Scope = "offline_access"
)

// String renders the scope request parameter, which RFC 6749 defines as space-delimited.
func (s Scopes) String() string {
	parts := make([]string, len(s))
	for i, one := range s {
		parts[i] = string(one)
	}
	return strings.Join(parts, " ")
}
