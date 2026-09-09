package setting

import (
	"fmt"
)

type Source interface {
	// Lookup returns empty string, no error if nothing can be provided, handled in Resolve.
	// An error can be returned if a fatal condition is detected, usually used for low-priority sources such as DefaultSource.
	Lookup(key string) (string, error)
	// Describe returns SourceDescription with proper SourceDescription.String representation for logging
	Describe(key string) SourceDescription
}

type Sources []Source

type SourceDescription struct {
	// Type is a constant string identifying sources (env, default, custom such as flag/stdin/prompt, tf provider block attribute).
	Type string
	// Details is appended with space to Type in SourceDescription.String. If empty, that source is currently a DefaultSource.
	Details string
}

func (s SourceDescription) String() string {
	if s.Details == "" {
		return s.Type
	}
	return fmt.Sprintf("%s %s", s.Type, s.Details)
}

type LookupFunc func() (string, error)

// StaticLookup constructs a static lookup value. Useful for DefaultSource.
func StaticLookup(v string) LookupFunc {
	return func() (string, error) {
		return v, nil
	}
}

// DefaultSource provides Setting.Default source from the given LookupFunc.
// See also StaticLookup.
type DefaultSource LookupFunc

var _ Source = DefaultSource(nil)

func (d DefaultSource) Lookup(string) (string, error) {
	return d()
}

func (d DefaultSource) Describe(key string) SourceDescription {
	return SourceDescription{"default value", fmt.Sprintf("for %s", key)}
}

// ExplicitSource gives the sources a higher precedence than EnvKey source, see Resolve.
type ExplicitSource struct {
	Source
}
