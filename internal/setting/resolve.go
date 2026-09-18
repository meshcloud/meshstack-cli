package setting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var ErrNoSourceProvidedValue = errors.New("no source provided a value")

type Sources []Source

// ResolveSetting takes the value from the first source that carries one, in this order:
// FrontendSource, any other source, the environment, FallbackSource, the setting's default.
// Two sources of the same kind keep the order they were given in. It returns
// ErrNoSourceProvidedValue when every source stayed empty.
func (sources Sources) ResolveSetting[T any](ctx context.Context, setting Setting[T], extraSources ...Source) (value T, err error) {
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
		return
	}
	return value, fmt.Errorf("%w for %s", ErrNoSourceProvidedValue, setting.EnvKey())
}
