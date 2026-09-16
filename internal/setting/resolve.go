package setting

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoSourceProvidedValue is wrapped with the Setting.EnvKey of the setting that stayed empty,
// so a caller reading errors.Is still matches while the message names what is missing.
var ErrNoSourceProvidedValue = errors.New("no source provided a value")

// Resolve returns the first value a source carries, the resolution details, and an error if sth went wrong.
// Given sources are preceded by Setting.Env, succeeded by Setting.Default (if any), unless ExplicitSource is used, so:
// [Explicit sources..., Env, other sources..., Default].
// Returns ErrNoSourceProvidedValue iff no source provided a value.
func (s Setting[T]) Resolve(sources ...Source) (value T, err error) {
	sourcesWithEnvAndDefault := slices.Insert(slices.Clone(sources), 0, Source(s.Env))
	if s.Default != nil {
		// a nil DefaultSource in a Source is not a nil Source, so the skip below misses it
		sourcesWithEnvAndDefault = append(sourcesWithEnvAndDefault, s.Default)
	}
	slices.SortStableFunc(sourcesWithEnvAndDefault, byExplicitSourceFirst)

	var emptySources []Source
	defer func() {
		if err != nil {
			errs := []error{err}
			for _, source := range emptySources {
				if _, isDefault := source.(DefaultSource); isDefault {
					continue
				} else if describeSource := source.Describe(s.EnvKey()); describeSource != "" {
					errs = append(errs, fmt.Errorf("try setting %s", describeSource))
				}
			}
			err = errors.Join(errs...)
		}
	}()

	for _, source := range sourcesWithEnvAndDefault {
		if source == nil {
			// convenient to skip "no default" case, also callers can make use of that
			continue
		}
		var text string
		text, err = source.Lookup(s.EnvKey())
		if err != nil {
			return value, fmt.Errorf("value from source '%s' could not be looked up: %w", source.Describe(s.EnvKey()), err)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			emptySources = append(emptySources, source)
			continue
		}
		value, err = s.Parse(text)
		if err != nil {
			return value, fmt.Errorf("value from source '%s' could not be parsed: %w", source.Describe(s.EnvKey()), err)
		}
		return // resolution found
	}
	return value, fmt.Errorf("%w for %s", ErrNoSourceProvidedValue, s.EnvKey())
}

// byExplicitSourceFirst is used by Resolve. See ExplicitSource.
func byExplicitSourceFirst(a, b Source) int {
	_, aIsExplicit := a.(ExplicitSource)
	_, bIsExplicit := b.(ExplicitSource)
	switch {
	case aIsExplicit && !bIsExplicit:
		return -1
	case !aIsExplicit && bIsExplicit:
		return 1
	default:
		return 0
	}
}
