package setting

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Resolution is returned by Resolve.
type Resolution struct {
	// SettingEnvKey is Setting.EnvKey for look up / comparison on caller side.
	SettingEnvKey string
	// From is non-nil iff Resolve found a value.
	From Source
	// Queried is used by Hint and callers may inspect / react on their own to a failed/successful Resolve.
	Queried Sources
}

func (r Resolution) Hint(err error) error {
	errs := []error{err}
	for _, source := range r.Queried {
		if _, isDefault := source.(DefaultSource); isDefault {
			continue
		}
		errs = append(errs, fmt.Errorf("try setting %s", source.Describe(r.SettingEnvKey)))
	}
	return errors.Join(errs...)
}

// Resolve returns the first value a source carries, the resolution details, and an error if sth went wrong.
// Given sources are preceded by Setting.Env, succeeded by Setting.Default (if any), unless ExplicitSource is used, so:
// [Explicit sources..., Env, other sources..., Default].
// Resolution.From is nil iff no source provided a value.
func Resolve[T any](setting Setting[T], sources ...Source) (result T, resolution Resolution, err error) {
	resolution.SettingEnvKey = setting.EnvKey() // carry this over to link resolution back to setting if caller wants to do that

	sourcesWithEnvAndDefault := slices.Insert[Sources, Source](slices.Clone(sources), 0, setting.Env)
	if setting.Default != nil {
		// a nil DefaultSource in a Source is not a nil Source, so the skip below misses it
		sourcesWithEnvAndDefault = append(sourcesWithEnvAndDefault, setting.Default)
	}
	slices.SortStableFunc(sourcesWithEnvAndDefault, byExplicitSourceFirst)
	for _, source := range sourcesWithEnvAndDefault {
		if source == nil {
			// convenient to skip "no default" case, also callers can make use of that
			continue
		}
		resolution.Queried = append(resolution.Queried, source)
		var text string
		text, err = source.Lookup(resolution.SettingEnvKey)
		if err != nil {
			return result, resolution, fmt.Errorf("value from source '%s' could not be looked up: %w", source.Describe(resolution.SettingEnvKey), err)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		result, err = setting.Parse(text)
		if err != nil {
			return result, resolution, fmt.Errorf("value from source '%s' could not be parsed: %w", source.Describe(resolution.SettingEnvKey), err)
		}
		resolution.From = source
		return // resolution found
	}
	return
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
