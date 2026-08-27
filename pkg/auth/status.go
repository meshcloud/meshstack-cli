package auth

import (
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// Status is everything `meshstack auth status` reports without making a network call. That
// is the whole stored state except whether the credential was revoked, which nothing local
// can know because a refresh token carries no expiry — so proving that gets a --verify flag
// rather than a round trip on every call.
type Status struct {
	Profile         string
	ConfigPath      string
	CredentialsPath string
	Endpoint        string
	Workspace       workspace.Name

	// Sources says where each resolved value came from, keyed by "endpoint", "workspace",
	// "profile" and "credential". `meshstack profile view` prints it.
	Sources map[string]string

	Current method.Method
	Login   *LoginStatus
	ApiKey  *ApiKeyStatus
	Token   *TokenStatus
}

type LoginStatus struct {
	Issuer     string
	ObtainedAt time.Time
	// Age is how long ago the login happened. A meshStack CLI login lasts at most 24 hours,
	// idle and absolute, but that is a server-side constant: reporting the age is always
	// truthful where predicting the deadline would not be.
	Age time.Duration
}

type ApiKeyStatus struct {
	ClientId string
	// SecretFrom names where the secret comes from: the credentials file, or the command that
	// prints it.
	SecretFrom string
}

type TokenStatus struct {
	Scope     workspace.Scope
	ExpiresAt time.Time
	// ExpiresIn is negative for a token that has already expired, and zero when the token
	// carries no expiry at all.
	ExpiresIn time.Duration
}

// Status reads the stored state. It makes no network call, so it stays fast enough for a
// shell prompt or a script.
func (s *Session) Status() (Status, error) {
	credentials, err := s.currentStore().Read()
	if err != nil {
		return Status{}, err
	}

	status := Status{
		Profile:         s.Profile,
		CredentialsPath: s.currentStore().Describe(),
		Endpoint:        s.Endpoint.String(),
		Workspace:       s.Workspace,
		Sources:         s.sources,
		Current:         s.Method(),
	}
	if path, err := profile.ConfigPath(); err == nil {
		status.ConfigPath = path
	}
	if login := credentials.Methods.Login; login != nil {
		status.Login = &LoginStatus{Issuer: login.Issuer, ObtainedAt: login.ObtainedAt}
		if !login.ObtainedAt.IsZero() {
			status.Login.Age = time.Since(login.ObtainedAt)
		}
	}
	if apiKey := credentials.Methods.ApiKey; apiKey != nil {
		status.ApiKey = &ApiKeyStatus{ClientId: apiKey.ClientId, SecretFrom: "the credentials file"}
		if len(apiKey.ClientSecretCommand) > 0 {
			status.ApiKey.SecretFrom = "the command " + apiKey.ClientSecretCommand[0]
		}
	}
	scope := s.Scope()
	if token, ok := credentials.AccessTokens[scope]; ok {
		status.Token = &TokenStatus{Scope: scope, ExpiresAt: token.ExpiresAt}
		if !token.ExpiresAt.IsZero() {
			status.Token.ExpiresIn = time.Until(token.ExpiresAt)
		}
	}
	return status, nil
}
