package setting

import "context"

type Source interface {
	// Lookup returns empty string, no error if nothing can be provided, handled in Resolve.
	// An error can be returned if a fatal condition is detected, usually used for low-priority sources such as DefaultSource.
	Lookup(ctx context.Context, key string) (string, error)
	// Describe returns a string representation of the Lookup for logging or error handling/hinting.
	Describe(key string) string
}

// DefaultSource provides Setting.Default source from the given LookupFunc.
// See also StaticDefault.
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

// StaticDefault constructs a default static value. Useful for Setting.Default.
func StaticDefault(v string) DefaultSource {
	return func() (string, error) {
		return v, nil
	}
}

// LookupSource calls its Func when queried as source.
// If MatchingKey is non-empty, Func is only called if given Setting.EnvKey() matches.
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

// FallbackSource gives the wrapped source a lower precedence than EnvKey source, see Sources.ResolveSetting.
type FallbackSource struct {
	Source
}

// FrontendSource gives the wrapped source a higher precedence than EnvKey source, see Sources.ResolveSetting.
type FrontendSource struct {
	Source
}
