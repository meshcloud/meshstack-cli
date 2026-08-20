// Package login resolves meshStack API credentials and turns them into a
// client.Authorization.
//
// It is shared by the meshStack CLI and the meshStack Terraform provider, so both
// read the same environment variables and run the same login exchange. The
// exchange itself is not here: it lives in client/internal, which Go's internal
// rule keeps inside client/, and is reached through client.NewApiKeyAuthorization.
// A second, hand-rolled exchange would get a static token and start returning 401
// once it expired.
package login

import (
	"fmt"
	"net/url"
	"os"

	"github.com/meshcloud/meshstack-cli/client"
)

// Environment variables holding meshStack API credentials. They are exported so
// that callers can name them in their own error messages — the Terraform provider
// does, because its diagnostics have to mention both the provider attribute and
// the variable — and so that both repositories share one definition.
const (
	EnvKeyEndpoint  = "MESHSTACK_ENDPOINT"
	EnvKeyApiKey    = "MESHSTACK_API_KEY"
	EnvKeyApiSecret = "MESHSTACK_API_SECRET"
	EnvKeyApiToken  = "MESHSTACK_API_TOKEN"
)

// Credentials addresses one meshStack API. The fields are unvalidated: an empty
// field means "not configured", which lets a caller merge several sources before
// deciding whether anything is missing.
type Credentials struct {
	Endpoint string
	// ApiKey and ApiSecret are exchanged for an access token at every client
	// creation. This is the pair to prefer, because the client refreshes the
	// token it receives.
	ApiKey    string
	ApiSecret string
	// ApiToken is an access token that is already valid, and skips the exchange.
	// It takes precedence over ApiKey and ApiSecret. Nothing can refresh it, so it
	// expires during long-running work.
	ApiToken string
}

// FromEnv reads credentials from the environment. Variables that are unset yield
// empty fields rather than an error, so a caller with another source of
// configuration can fill them in.
func FromEnv() Credentials {
	return Credentials{
		Endpoint:  os.Getenv(EnvKeyEndpoint),
		ApiKey:    os.Getenv(EnvKeyApiKey),
		ApiSecret: os.Getenv(EnvKeyApiSecret),
		ApiToken:  os.Getenv(EnvKeyApiToken),
	}
}

// Merge returns c with every non-empty field of override applied on top. Callers
// that read credentials from more than one place use this to rank the sources:
// the Terraform provider merges its provider block attributes over FromEnv, so an
// explicitly configured attribute wins over the environment.
func (c Credentials) Merge(override Credentials) Credentials {
	if override.Endpoint != "" {
		c.Endpoint = override.Endpoint
	}
	if override.ApiKey != "" {
		c.ApiKey = override.ApiKey
	}
	if override.ApiSecret != "" {
		c.ApiSecret = override.ApiSecret
	}
	if override.ApiToken != "" {
		c.ApiToken = override.ApiToken
	}
	return c
}

// EndpointURL parses the endpoint into the form client.New expects.
func (c Credentials) EndpointURL() (*url.URL, error) {
	if c.Endpoint == "" {
		return nil, fmt.Errorf("meshStack endpoint is not configured, set the %s environment variable", EnvKeyEndpoint)
	}
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("meshStack endpoint %q is not a valid URL: %w", c.Endpoint, err)
	}
	return endpoint, nil
}

// Authorization builds the authorization to hand to client.New. It reports what is
// missing rather than producing an authorization that fails on first use.
func (c Credentials) Authorization() (client.Authorization, error) {
	if c.ApiToken != "" {
		return client.NewApiTokenAuthorization(c.ApiToken), nil
	}
	switch {
	case c.ApiKey == "" && c.ApiSecret == "":
		return nil, fmt.Errorf("meshStack API credentials are not configured, set the %s and %s environment variables", EnvKeyApiKey, EnvKeyApiSecret)
	case c.ApiKey == "":
		return nil, fmt.Errorf("meshStack API key is not configured, set the %s environment variable", EnvKeyApiKey)
	case c.ApiSecret == "":
		return nil, fmt.Errorf("meshStack API secret is not configured, set the %s environment variable", EnvKeyApiSecret)
	}
	return client.NewApiKeyAuthorization(c.ApiKey, c.ApiSecret), nil
}
