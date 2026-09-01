// Package setting declares a configuration item and resolves it from ranked sources.
//
// Declarations live in the domain packages rather than here: a domain package names its own
// MESHSTACK_* variable in its own error messages, so holding both here would be an import cycle.
//
// A ranking is a []Source at the call site, which is what keeps a precedence order visible in
// one function instead of spread over every declaration.
package setting

import (
	"os"
	"strings"

	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

type Value[T any] struct {
	// EnvKey is also the setting's identity, so there is no second identifier to hold in step.
	EnvKey string

	// Short is one line of plain text, for a cobra flag; Long is markdown, for the Terraform
	// provider's schema. Both state facts about the setting rather than about a front end, so
	// neither says "flag", "block" or "attribute".
	Short string
	Long  string

	Parse func(string) (T, error)
}

// Help is Long, or Short where a declaration wrote only the one.
func (v Value[T]) Help() string {
	if v.Long != "" {
		return v.Long
	}
	return v.Short
}

// Source is one place a value may come from.
//
// Describe takes the key because a flag source names the flag it matched and an environment
// source names the variable, neither of which is one string for the whole source.
//
// Text, not T: the environment, argv and a Terraform types.String can carry nothing else.
type Source interface {
	Lookup(key string) (string, bool)
	Describe(key string) string
}

// Origin is where one resolved value came from. Source is the winning source's own Describe,
// so it cannot drift from the value; Key is the setting's EnvKey.
type Origin struct {
	Key    string
	Source string
}

// Environ is the environment layer of a ranked list.
func Environ() Source { return environ{} }

type environ struct{}

// An exported but empty variable has not answered, the way Resolve treats every other source:
// `export MESHSTACK_WORKSPACE=` must not silence the profile below it.
func (environ) Lookup(key string) (string, bool) {
	value := os.Getenv(key)
	return value, value != ""
}

func (environ) Describe(key string) string { return key }

// Default is the bottom of a ranked list.
func Default(value string) Source {
	return DefaultFunc(func() (string, bool) { return value, value != "" })
}

// DefaultFunc is a default that can fail — a directory derived from os.UserConfigDir — where
// the failure is ("", false) and the message naming what is missing belongs to the caller.
func DefaultFunc(f func() (string, bool)) Source { return defaultSource{answer: f} }

type defaultSource struct{ answer func() (string, bool) }

// The key is ignored: a default source is only ever placed in one setting's ranked list.
func (d defaultSource) Lookup(string) (string, bool) { return d.answer() }

func (defaultSource) Describe(string) string { return "built-in default" }

// Resolve returns the first value a source carries, the source that answered, and an error only
// if that value would not parse. Nothing configured is the zero T, a nil Source and a nil error:
// the "not configured" message is the caller's, because what makes it good is which profile was
// picked and which command to run next.
//
// A nil Source is skipped, so a front end contributing nothing explicit costs its caller no
// filtering. So is a source answering "", because every source here reports an unset value that
// way and an unset flag must not silence the environment below it.
func Resolve[T any](v Value[T], sources ...Source) (T, Source, error) {
	var zero T
	for _, source := range sources {
		if source == nil {
			continue
		}
		text, ok := source.Lookup(v.EnvKey)
		if !ok || text == "" {
			continue
		}
		parsed, err := v.Parse(text)
		if err != nil {
			return zero, source, diags.Wrap(err, "invalid "+v.EnvKey,
				"the value from %s could not be used: %v", source.Describe(v.EnvKey), err)
		}
		return parsed, source, nil
	}
	return zero, nil, nil
}

// Text is the Parse for a plain string setting.
func Text(s string) (string, error) { return strings.TrimSpace(s), nil }
