package generator

import (
	"strings"
	"unicode"
)

// goName writes a JSON name or an enum value as an exported Go name: displayName is DisplayName,
// _links is Links, WAITING_FOR_INPUT is WaitingForInput and v2-preview is V2Preview.
func goName(name string) string {
	var b strings.Builder
	for word := range strings.FieldsFuncSeq(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if strings.ToUpper(word) == word {
			word = strings.ToLower(word)
		}
		b.WriteString(upperFirst(word))
	}
	return sanitize(b.String())
}

// sanitize keeps letters and digits, and makes the name exported.
func sanitize(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, name)
	if name != "" && unicode.IsDigit(rune(name[0])) {
		name = "T" + name
	}
	return upperFirst(name)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
