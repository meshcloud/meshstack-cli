package profile

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
)

type (
	selectionContextKey int
	SelectionFunc       func() (Selection, error)
)

// SelectionFromContext returns what a front end picks a profile from. It is only in the context
// while NameSetting is being resolved, so a lookup for any other setting gets an error.
func SelectionFromContext(ctx context.Context) (Selection, error) {
	selection, found := ctx.Value(selectionContextKey(0)).(SelectionFunc)
	if !found {
		return Selection{}, fmt.Errorf("no profile selection available in this context; it is only provided while resolving %s", NameSetting.EnvKey())
	}
	return selection()
}

func SetSelectionInContext(ctx context.Context, selection SelectionFunc) context.Context {
	return context.WithValue(ctx, selectionContextKey(0), selection)
}

type Selection struct {
	// Profiles are all stored profiles, ordered by name, so a numbered list stays stable.
	Profiles []*Profile
	Current  Name
	// Endpoint is nil when this run names no endpoint.
	Endpoint *xurl.URL
}

func (ps Profiles) selection() Selection {
	sorted := slices.SortedFunc(maps.Values(ps.Profiles), func(a, b *Profile) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return Selection{Profiles: sorted, Current: ps.CurrentProfile}
}

// Candidates are the profiles this run may use: those for its endpoint, or all without one.
func (s Selection) Candidates() []*Profile {
	if s.Endpoint == nil {
		return s.Profiles
	}
	return slices.DeleteFunc(slices.Clone(s.Profiles), func(p *Profile) bool {
		return !p.Endpoint.Equal(*s.Endpoint)
	})
}

// Label is what a person picks a profile by.
func (p Profile) Label() string {
	if p.Endpoint.URL == nil {
		return fmt.Sprintf("%s (no endpoint)", p.Name)
	}
	return fmt.Sprintf("%s (%s)", p.Name, p.Endpoint)
}
