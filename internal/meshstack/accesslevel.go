package meshstack

import (
	"slices"

	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

// AccessLevel limits a browser login to a subset of the user's rights.
type AccessLevel string

const (
	AccessFull  AccessLevel = "full"
	AccessWrite AccessLevel = "write"
	AccessRead  AccessLevel = "read"
)

// AccessLevels is the order the login page offers them in.
var AccessLevels = []AccessLevel{AccessFull, AccessWrite, AccessRead}

func ParseAccessLevel(value string) (AccessLevel, bool) {
	if level := AccessLevel(value); slices.Contains(AccessLevels, level) {
		return level, true
	}
	return "", false
}

func AccessLevelOf(scopes scope.Scopes) (AccessLevel, bool) {
	for _, level := range AccessLevels {
		if slices.Contains(scopes, level.Scope()) {
			return level, true
		}
	}
	return "", false
}

// Scope must match the client scopes that CliClientBootstrapService in ../meshfed-release creates.
func (l AccessLevel) Scope() scope.Scope {
	return "cli-access-" + scope.Scope(l)
}

func (l AccessLevel) Label() string {
	switch l {
	case AccessFull:
		return "Full access"
	case AccessWrite:
		return "No delete"
	case AccessRead:
		return "Read-only"
	}
	return string(l)
}

func (l AccessLevel) Detail() string {
	switch l {
	case AccessFull:
		return "Everything your roles allow, including deleting."
	case AccessWrite:
		return "Read and change, but never delete."
	case AccessRead:
		return "List and read only."
	}
	return ""
}
