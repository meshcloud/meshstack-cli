package profile

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type selectionContextKey struct{}

// SelectionFromContext returns what a front end picks a profile from. It is only in the context
// while NameSetting is being resolved, so a lookup for any other setting gets an error.
func SelectionFromContext(ctx context.Context) (Selection, error) {
	selection, found := ctx.Value(selectionContextKey{}).(Selection)
	if !found {
		return Selection{}, fmt.Errorf("no profile selection available in this context; it is only provided while resolving %s", NameSetting.EnvKey())
	}
	return selection, nil
}

func SetSelectionInContext(ctx context.Context, selection Selection) context.Context {
	return context.WithValue(ctx, selectionContextKey{}, selection)
}

type Selection struct {
	// Profiles are all stored profiles, ordered by name, so a numbered list stays stable.
	Profiles []*Profile
	Current  Name
	// Endpoint is nil when this run names no endpoint.
	Endpoint *xurl.URL
}

func (ps *Profiles) Selection() Selection {
	sorted := slices.SortedFunc(maps.Values(ps.Profiles), func(a, b *Profile) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return Selection{Profiles: sorted, Current: ps.CurrentProfile}
}

// ForEndpoint returns copies, so that a caller can read them while the profiles change.
func (ps *Profiles) ForEndpoint(endpoint xurl.URL) (matching []Profile) {
	selection := ps.Selection()
	selection.Endpoint = &endpoint
	for _, p := range selection.Candidates() {
		matching = append(matching, *p)
	}
	return matching
}

func (s Selection) Candidates() []*Profile {
	if s.Endpoint == nil {
		return s.Profiles
	}
	return slices.DeleteFunc(slices.Clone(s.Profiles), func(p *Profile) bool {
		return !p.Endpoint.Equal(*s.Endpoint)
	})
}

func (s Selection) nameSource() setting.FallbackSource {
	var name Name
	description := "current profile"
	if candidates := s.Candidates(); slices.ContainsFunc(candidates, func(p *Profile) bool { return p.Name == s.Current }) {
		name = s.Current
	} else if s.Endpoint != nil && len(candidates) == 1 {
		name, description = candidates[0].Name, "only profile with the endpoint "+s.Endpoint.String()
	}
	return setting.FallbackSource{Stored: true, Source: setting.LookupSource{
		Description: description,
		Func:        func(context.Context) (string, error) { return string(name), nil },
	}}
}

func (p Profile) Label() string {
	if p.Endpoint.URL == nil {
		return fmt.Sprintf("%s (no endpoint)", p.Name)
	}
	return fmt.Sprintf("%s (%s)", p.Name, p.Endpoint)
}
