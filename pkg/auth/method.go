package auth

import (
	"context"

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

// MethodFromContext returns how this session authenticates. The method is only in the context
// while MESHSTACK_WORKSPACE is being resolved, so a lookup for any other setting gets an error.
func MethodFromContext(ctx context.Context) (Method, error) {
	return credential.NameFromContext(ctx)
}
