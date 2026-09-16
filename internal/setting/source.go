package setting

type Source interface {
	// Lookup returns empty string, no error if nothing can be provided, handled in Resolve.
	// An error can be returned if a fatal condition is detected, usually used for low-priority sources such as DefaultSource.
	Lookup(key string) (string, error)
	// Describe returns a string representation of the Lookup for logging or error handling/hinting.
	Describe(key string) string
}

// DefaultSource provides Setting.Default source from the given LookupFunc.
// See also StaticDefault.
type DefaultSource func() (string, error)

var _ Source = DefaultSource(nil)

func (d DefaultSource) Lookup(string) (string, error) {
	return d()
}

func (d DefaultSource) Describe(key string) string {
	return "default value for " + key
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
	Func        func() (string, error)
}

func (s LookupSource) Lookup(key string) (string, error) {
	if s.MatchingKey != "" && s.MatchingKey != key {
		return "", nil
	}
	return s.Func()
}

func (s LookupSource) Describe(key string) string {
	if s.MatchingKey != "" && s.MatchingKey != key {
		return ""
	}
	return s.Description
}

// ExplicitSource gives the sources a higher precedence than EnvKey source, see Resolve.
type ExplicitSource struct {
	Source
}

// ExplicitSourcesOption conveniently exposes ExplicitSource as an option,
// and ensures by ExplicitSourcesOption.ResolveSetting that this explicit source is always used.
type ExplicitSourcesOption struct {
	UseSettingsFrom []ExplicitSource
}

// ResolveSetting ensures the explicitly configured sources are resolved alongside the given ones.
func (o ExplicitSourcesOption) ResolveSetting[T any](setting Setting[T], sources ...Source) (T, error) {
	for _, explicitSource := range o.UseSettingsFrom {
		// this nil check is important if "NoSource" is passed (default constructed ExplicitSource).
		if explicitSource.Source != nil {
			sources = append(sources, explicitSource)
		}
	}
	return setting.Resolve(sources...)
}
