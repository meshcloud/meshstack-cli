package setting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoSourceProvidedValue is wrapped with the Setting.EnvKey of the setting that stayed empty,
// so a caller reading errors.Is still matches while the message names what is missing.
var ErrNoSourceProvidedValue = errors.New("no source provided a value")

// Sources implement Sources.ResolveSetting, so this type is useful for embedding in options structs.
type Sources []Source

// ResolveSetting returns the first found value for a setting a source carries.
// Sources are ordered by FrontendSource first,
// then other sources (if any),
// then Setting.Env source,
// then FallbackSource,
// then default source (if any).
// Once a value is found, remaining sources are not queried.
// Returns ErrNoSourceProvidedValue iff no source provided a value.
func (sources Sources) ResolveSetting[T any](ctx context.Context, setting Setting[T], extraSources ...Source) (value T, err error) {
	// Set up tracking empty sources to construct a helpful error when resolution fails.
	var emptySources Sources
	defer func() {
		if err != nil {
			errs := []error{err}
			for _, source := range emptySources {
				if _, isDefault := source.(DefaultSource); isDefault {
					continue
				} else if describeSource := source.Describe(setting.EnvKey()); describeSource != "" {
					errs = append(errs, fmt.Errorf("try setting %s", describeSource))
				}
			}
			err = errors.Join(errs...)
		}
	}()

	var frontendSources, otherSources, fallbackSources Sources
	for _, source := range slices.Concat(sources, extraSources) {
		if _, isFrontend := source.(FrontendSource); isFrontend {
			frontendSources = append(frontendSources, source)
		} else if _, isFallback := source.(FallbackSource); isFallback {
			fallbackSources = append(fallbackSources, source)
		} else {
			otherSources = append(otherSources, source)
		}
	}

	allSources := slices.Concat(
		frontendSources,
		otherSources,
		Sources{setting.Env},
		fallbackSources,
		setting.Default.asSourcesIfPresent(),
	)

	for _, source := range allSources {
		if source == nil {
			// convenient to skip "no default" case, also callers can make use of that
			continue
		}
		var text string
		text, err = source.Lookup(ctx, setting.EnvKey())
		if err != nil {
			return value, fmt.Errorf("value from source '%s' could not be looked up: %w", source.Describe(setting.EnvKey()), err)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			emptySources = append(emptySources, source)
			continue
		}
		value, err = setting.Parse(text)
		if err != nil {
			return value, fmt.Errorf("value from source '%s' could not be parsed: %w", source.Describe(setting.EnvKey()), err)
		}
		return // resolution found, stop querying remaining sources
	}
	return value, fmt.Errorf("%w for %s", ErrNoSourceProvidedValue, setting.EnvKey())
}
