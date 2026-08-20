package login_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/login"
)

func TestFromEnv(t *testing.T) {
	for name, testCase := range map[string]struct {
		environment map[string]string
		want        login.Credentials
	}{
		"all four variables set": {
			environment: map[string]string{
				login.EnvKeyEndpoint:  "https://api.my.meshstack.io",
				login.EnvKeyApiKey:    "key",
				login.EnvKeyApiSecret: "secret",
				login.EnvKeyApiToken:  "token",
			},
			want: login.Credentials{
				Endpoint:  "https://api.my.meshstack.io",
				ApiKey:    "key",
				ApiSecret: "secret",
				ApiToken:  "token",
			},
		},
		"key and secret without a token": {
			environment: map[string]string{
				login.EnvKeyEndpoint:  "https://api.my.meshstack.io",
				login.EnvKeyApiKey:    "key",
				login.EnvKeyApiSecret: "secret",
			},
			want: login.Credentials{
				Endpoint:  "https://api.my.meshstack.io",
				ApiKey:    "key",
				ApiSecret: "secret",
			},
		},
		"an empty variable reads as unset": {
			environment: map[string]string{login.EnvKeyEndpoint: ""},
			want:        login.Credentials{},
		},
		"nothing set": {
			environment: map[string]string{},
			want:        login.Credentials{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Clear all four first, so a variable left over from the developer's own
			// shell cannot make a case pass.
			for _, key := range []string{login.EnvKeyEndpoint, login.EnvKeyApiKey, login.EnvKeyApiSecret, login.EnvKeyApiToken} {
				t.Setenv(key, "")
			}
			for key, value := range testCase.environment {
				t.Setenv(key, value)
			}

			assert.Equal(t, testCase.want, login.FromEnv())
		})
	}
}

func TestMerge(t *testing.T) {
	environment := login.Credentials{
		Endpoint:  "https://from-env.meshstack.io",
		ApiKey:    "env-key",
		ApiSecret: "env-secret",
		ApiToken:  "env-token",
	}

	for name, testCase := range map[string]struct {
		override login.Credentials
		want     login.Credentials
	}{
		"an empty override changes nothing": {
			override: login.Credentials{},
			want:     environment,
		},
		"a non-empty field wins": {
			override: login.Credentials{Endpoint: "https://explicit.meshstack.io"},
			want: login.Credentials{
				Endpoint:  "https://explicit.meshstack.io",
				ApiKey:    "env-key",
				ApiSecret: "env-secret",
				ApiToken:  "env-token",
			},
		},
		"empty fields of the override do not clear the receiver": {
			override: login.Credentials{ApiKey: "explicit-key"},
			want: login.Credentials{
				Endpoint:  "https://from-env.meshstack.io",
				ApiKey:    "explicit-key",
				ApiSecret: "env-secret",
				ApiToken:  "env-token",
			},
		},
		"every field overridden at once": {
			override: login.Credentials{
				Endpoint:  "https://explicit.meshstack.io",
				ApiKey:    "explicit-key",
				ApiSecret: "explicit-secret",
				ApiToken:  "explicit-token",
			},
			want: login.Credentials{
				Endpoint:  "https://explicit.meshstack.io",
				ApiKey:    "explicit-key",
				ApiSecret: "explicit-secret",
				ApiToken:  "explicit-token",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, testCase.want, environment.Merge(testCase.override))
		})
	}
}

func TestEndpointURL(t *testing.T) {
	for name, testCase := range map[string]struct {
		endpoint string
		want     string
		wantErr  string
	}{
		"a valid URL": {
			endpoint: "https://api.my.meshstack.io",
			want:     "https://api.my.meshstack.io",
		},
		"a URL with a port and path": {
			endpoint: "http://localhost:8080/api",
			want:     "http://localhost:8080/api",
		},
		"an empty endpoint names its variable": {
			endpoint: "",
			wantErr:  login.EnvKeyEndpoint,
		},
		"an unparseable endpoint is reported": {
			endpoint: "://no-scheme",
			wantErr:  "not a valid URL",
		},
	} {
		t.Run(name, func(t *testing.T) {
			endpoint, err := login.Credentials{Endpoint: testCase.endpoint}.EndpointURL()

			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, endpoint.String())
		})
	}
}

func TestAuthorization(t *testing.T) {
	// Header cannot be called from outside client/: it takes a
	// client/internal.HttpClient, which no other package can name. So an
	// authorization is identified by comparing it against one built from a single
	// credential source.
	fromToken, err := login.Credentials{ApiToken: "token"}.Authorization()
	require.NoError(t, err)
	fromKeySecret, err := login.Credentials{ApiKey: "key", ApiSecret: "secret"}.Authorization()
	require.NoError(t, err)

	t.Run("the two credential sources build different authorizations", func(t *testing.T) {
		// Guards the comparisons below: if both sources produced the same value, the
		// precedence cases would pass for the wrong reason.
		assert.NotEqual(t, fromToken, fromKeySecret)
	})

	for name, testCase := range map[string]struct {
		credentials login.Credentials
		want        login.Credentials
	}{
		"a token alone": {
			credentials: login.Credentials{ApiToken: "token"},
			want:        login.Credentials{ApiToken: "token"},
		},
		"a token outranks a key and secret": {
			credentials: login.Credentials{ApiKey: "key", ApiSecret: "secret", ApiToken: "token"},
			want:        login.Credentials{ApiToken: "token"},
		},
		"a key and secret alone": {
			credentials: login.Credentials{ApiKey: "key", ApiSecret: "secret"},
			want:        login.Credentials{ApiKey: "key", ApiSecret: "secret"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			want, err := testCase.want.Authorization()
			require.NoError(t, err)

			got, err := testCase.credentials.Authorization()

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestAuthorizationNamesTheMissingVariable(t *testing.T) {
	for name, testCase := range map[string]struct {
		credentials login.Credentials
		wants       []string
	}{
		"nothing configured": {
			credentials: login.Credentials{},
			wants:       []string{login.EnvKeyApiKey, login.EnvKeyApiSecret},
		},
		"a secret without a key": {
			credentials: login.Credentials{ApiSecret: "secret"},
			wants:       []string{login.EnvKeyApiKey},
		},
		"a key without a secret": {
			credentials: login.Credentials{ApiKey: "key"},
			wants:       []string{login.EnvKeyApiSecret},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testCase.credentials.Authorization()

			require.Error(t, err)
			for _, want := range testCase.wants {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}
