// Package method names how a caller authenticates against the meshStack API.
//
// It is a leaf on purpose: it imports nothing, so pkg/profile can write a method into
// the credentials file and pkg/auth can act on one without either depending on the
// other. These strings are part of the on-disk format, so renaming one breaks every
// credentials file already written.
package method

type Method string

const (
	Login  Method = "login"  // an OIDC refresh token from the browser flow
	ApiKey Method = "apiKey" // an API key id and secret
	Manual Method = "manual" // a token somebody pasted in; nothing can renew it
)

// Description names the method the way a message to a user should, because "apiKey"
// on its own reads like a field name rather than a thing the reader has.
func (m Method) Description() string {
	switch m {
	case Login:
		return "browser login"
	case ApiKey:
		return "API key"
	case Manual:
		return "API token"
	default:
		return string(m)
	}
}
