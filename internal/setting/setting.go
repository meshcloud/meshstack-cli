package setting

import (
	"encoding"
	"strings"
)

// Setting declares one input. Its environment variable key is also its identity, so every setting
// can be controlled through the environment and there is no second identifier to hold in step.
type Setting[T any] struct {
	Env EnvKey

	// Short is plain text for a cobra flag, Long is Markdown for the Terraform provider's schema.
	// Both state facts about the setting rather than about a front end, so neither says "flag",
	// "block" or "attribute".
	Short func(envKey string) string
	Long  func(envKey string) string

	Default DefaultSource
	// Parse always receives a non-empty string: Resolve trims what a source returned and skips
	// that source when nothing is left.
	Parse func(v string) (T, error)
}

func (s Setting[T]) EnvKey() string {
	return string(s.Env)
}

func (s Setting[T]) Help() string {
	if s.Short == nil {
		return ""
	}
	return s.Short(s.EnvKey())
}

func (s Setting[T]) HelpMarkdown() string {
	if s.Long == nil {
		return s.Help()
	}
	if long := s.Long(s.EnvKey()); long != "" {
		return long
	}
	return s.Help()
}

func ParseText[T ~string](s string) (T, error) { return T(s), nil }

func ParseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "n", "no", "0", "false":
		return false, nil
	default:
		return true, nil
	}
}

func ParseTextUnmarshaler[T any, P interface {
	*T
	encoding.TextUnmarshaler
}](s string) (T, error) {
	instance := new(T)
	err := P(instance).UnmarshalText(([]byte)(s))
	return *instance, err
}
