package jwt

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

var (
	_ encoding.TextUnmarshaler = &JWT{}
	_ encoding.TextMarshaler   = JWT{}
)

type JWT struct {
	String string
	claims map[string]any
}

// Parse reads a token that arrived as a plain string rather than inside a JSON document. A
// pasted API token is the case: it may not be a JWT at all, and then it carries no claims.
func Parse(text string) (token JWT, err error) {
	err = token.UnmarshalText([]byte(text))
	return
}

func (jwt JWT) MarshalText() (text []byte, err error) {
	return []byte(jwt.String), nil
}

func (jwt *JWT) UnmarshalText(text []byte) error {
	jwt.String = string(text)
	parts := strings.Split(jwt.String, ".")
	if len(parts) != 3 {
		return fmt.Errorf("the access token is not a JWT: it has %d dot-separated parts rather than 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("cannot decode the JWT payload: %w", err)
	}
	if err := json.Unmarshal(payload, &jwt.claims); err != nil {
		return fmt.Errorf("cannot parse JWT payload as JSON: %w", err)
	}
	return nil
}
