package profile

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

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

func (ps *Profiles) Selection() Selection {
	sorted := slices.SortedFunc(maps.Values(ps.Profiles), func(a, b *Profile) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return Selection{Profiles: sorted, Current: ps.CurrentProfile}
}

// MatchingEndpoint are copies of the profiles for endpoint, ordered by name, so that a caller can
// read them while the profiles change.
func (ps *Profiles) MatchingEndpoint(endpoint xurl.URL) (matching []Profile) {
	selection := ps.Selection()
	selection.Endpoint = &endpoint
	for _, p := range selection.Candidates() {
		matching = append(matching, *p)
	}
	return matching
}

// EndpointHolding returns the scheme and host of requestURL where no profile holds it: the endpoint
// a profile for it would need.
func (ps *Profiles) EndpointHolding(requestURL *url.URL) (endpoint xurl.URL, held bool) {
	for _, p := range ps.Profiles {
		if _, holds := p.Endpoint.PathTo(requestURL); holds && (endpoint.URL == nil || len(p.Endpoint.Path) > len(endpoint.Path)) {
			endpoint = p.Endpoint
		}
	}
	if endpoint.URL == nil {
		return xurl.URL{URL: &url.URL{Scheme: requestURL.Scheme, Host: requestURL.Host}}, false
	}
	return endpoint, true
}

func (ps *Profiles) Holding(requestURL *url.URL) (p Profile, current bool, err error) {
	endpoint, held := ps.EndpointHolding(requestURL)
	if !held {
		return Profile{}, false, fmt.Errorf("no stored profile has the endpoint %s of this URL; run 'meshstack login --endpoint %s' to create one",
			endpoint, endpoint)
	}
	return ps.ForEndpoint(endpoint, requestURL)
}

// ForEndpoint takes the current one of the profiles sharing endpoint, and fails where none of them
// is current rather than guess, as they can hold different credentials.
func (ps *Profiles) ForEndpoint(endpoint xurl.URL, requestURL *url.URL) (p Profile, current bool, err error) {
	matching := ps.MatchingEndpoint(endpoint)
	isCurrent := func(p Profile) bool { return p.Name == ps.CurrentProfile }
	switch {
	case len(matching) == 0:
		return Profile{}, false, fmt.Errorf("no stored profile has the endpoint %s; run 'meshstack login --endpoint %s' to create one", endpoint, endpoint)
	case len(matching) == 1:
		return matching[0], isCurrent(matching[0]), nil
	}
	if i := slices.IndexFunc(matching, isCurrent); i >= 0 {
		return matching[i], true, nil
	}
	names := make([]string, 0, len(matching))
	for _, p := range matching {
		names = append(names, "'"+string(p.Name)+"'")
	}
	return Profile{}, false, fmt.Errorf("profiles %s are all for endpoint %s of %s, and none is the current profile; name one with --profile or %s",
		strings.Join(names, ", "), endpoint, requestURL.Redacted(), NameSetting.EnvKey())
}

func (s Selection) Candidates() []*Profile {
	if s.Endpoint == nil {
		return s.Profiles
	}
	return slices.DeleteFunc(slices.Clone(s.Profiles), func(p *Profile) bool {
		return !p.Endpoint.Equal(*s.Endpoint)
	})
}

func (p Profile) Label() string {
	if p.Endpoint.URL == nil {
		return fmt.Sprintf("%s (no endpoint)", p.Name)
	}
	return fmt.Sprintf("%s (%s)", p.Name, p.Endpoint)
}
