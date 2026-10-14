package setting

import "context"

type Source interface {
	// Lookup returns an empty string and no error when it carries no value. An error stops the
	// whole resolution, so return one only for a condition no other source can make up for.
	Lookup(ctx context.Context, key string) (string, error)
	// Describe names this source to a person, so that a failed resolution can list what to set.
	Describe(key string) string
}

type DefaultSource func() (string, error)

var _ Source = DefaultSource(nil)

func (d DefaultSource) Lookup(_ context.Context, _ string) (string, error) {
	return d()
}

func (d DefaultSource) Describe(key string) string {
	return "default value for " + key
}

func (d DefaultSource) asSourcesIfPresent() Sources {
	if d == nil {
		return nil
	}
	return Sources{d}
}

func StaticDefault(v string) DefaultSource {
	return func() (string, error) {
		return v, nil
	}
}

type LookupSource struct {
	MatchingKey string
	Description string
	Func        func(ctx context.Context) (string, error)
}

func (s LookupSource) Lookup(ctx context.Context, key string) (string, error) {
	if s.MatchingKey != "" && s.MatchingKey != key {
		return "", nil
	}
	return s.Func(ctx)
}

func (s LookupSource) Describe(key string) string {
	if s.MatchingKey != "" && s.MatchingKey != key {
		return ""
	}
	return s.Description
}

// FallbackSource ranks the wrapped source below the environment, see Sources.ResolveSetting.
type FallbackSource struct {
	Source
}

// FrontendSource ranks the wrapped source above the environment, see Sources.ResolveSetting.
type FrontendSource struct {
	Source
}
