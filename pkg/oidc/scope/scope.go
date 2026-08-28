// Package scope names an OAuth scope value. It is a package of its own because three
// unrelated packages need the type and none of them may depend on the others: pkg/workspace
// derives the scope a workspace asks for, pkg/profile keys its cached access tokens by it,
// and pkg/oidc sends a list of them in a grant.
package scope

import "strings"

type Scope string

func (s Scope) String() string { return string(s) }

type Scopes []Scope

// String renders the scope request parameter, which RFC 6749 defines as space-delimited.
func (s Scopes) String() string {
	parts := make([]string, len(s))
	for i, one := range s {
		parts[i] = string(one)
	}
	return strings.Join(parts, " ")
}
