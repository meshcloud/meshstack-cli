package setting

import (
	"encoding"
	"strings"
)

// Setting represents the (internal) definition of a setting. Env is its identity used as a "key" to tell settings apart,
// and also requiring the convention that all settings can be controlled via environment variables.
type Setting[T any] struct {
	// Env key is also the setting's identity, so there is no second identifier to hold in step.
	Env EnvKey

	// Short is one line of plain text, for a cobra flag; Long is Markdown, for the Terraform
	// provider's schema. Both state facts about the setting rather than about a front end, so
	// neither says "flag", "block" or "attribute".
	Short string
	Long  string

	// Default source is always used as fallback (if non-nil). See Resolve.
	Default DefaultSource
	// Parse parses an always non-empty string to the actual Setting.
	// See ParseText, ParseBool, ParseTextUnmarshaler.
	Parse func(v string) (T, error)
}

// EnvKey is a unique identifier for Source.Lookup and Source.Describe method implementations.
func (s Setting[T]) EnvKey() string {
	return string(s.Env)
}

// Help is Long, or Short where a declaration wrote only the one.
func (s Setting[T]) Help() string {
	if s.Long != "" {
		return s.Long
	}
	return s.Short
}

// ParseText is the Setting.Parse for a plain string setting.
// Note that the input is already whitespace trimmed, see Resolve.
func ParseText[T ~string](s string) (T, error) { return T(s), nil }

// ParseBool is the Setting.Parse for a boolean-like string.
// Any string except indicating "no" leads to true (be generous).
// Note that an empty string is never passed, see Resolve.
func ParseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "n", "no", "0":
		return false, nil
	default:
		return true, nil
	}
}

// ParseTextUnmarshaler uses T's implementation of [encoding.TextUnmarshaler] for Setting.Parse.
func ParseTextUnmarshaler[T any, P interface {
	*T
	encoding.TextUnmarshaler
}](s string) (T, error) {
	instance := new(T)
	err := P(instance).UnmarshalText(([]byte)(s))
	return *instance, err
}
