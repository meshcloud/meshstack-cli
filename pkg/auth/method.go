package auth

import (
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
)

// Method names one way of authenticating, as [ResolveSessionOptions.ForceAuthWith] takes it.
type Method = credential.Name

const (
	// ApiKeyMethod mints a token from an API key id and secret.
	ApiKeyMethod = credential.ApiKeyName
	// ManualMethod sends an access token as it is.
	ManualMethod = credential.ManualName
	// OidcLoginMethod logs a person in through a browser, so it resolves only when asked for by name.
	OidcLoginMethod = credential.OidcLoginName
)
