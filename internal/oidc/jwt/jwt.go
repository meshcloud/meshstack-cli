package jwt

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

var (
	_ encoding.TextUnmarshaler = &JWT{}
	_ encoding.TextMarshaler   = JWT{}
)

type JWT struct {
	encoded []byte
	claims  map[string]any
}

func (jwt JWT) String() string {
	return string(jwt.encoded)
}

func (jwt JWT) GetClaim[V any](claim Claim[V]) V {
	return claim.getFrom(jwt)
}

func (jwt JWT) MarshalText() (text []byte, err error) {
	return jwt.encoded, nil
}

//goland:noinspection GoMixedReceiverTypes
func (jwt *JWT) UnmarshalText(text []byte) error {
	jwt.encoded = text
	parts := bytes.Split(jwt.encoded, []byte("."))
	if len(parts) != 3 {
		return fmt.Errorf("the access token is not a JWT: it has %d dot-separated parts rather than 3", len(parts))
	}
	payload := base64.NewDecoder(base64.RawURLEncoding, bytes.NewBuffer(parts[1]))
	if err := json.UnmarshalDecode(jsontext.NewDecoder(payload), &jwt.claims); err != nil {
		return fmt.Errorf("cannot parse JWT payload as JSON: %w", err)
	}
	return nil
}
