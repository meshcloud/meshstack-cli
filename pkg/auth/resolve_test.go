package auth

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

// TestExplicitBeatsEnvironmentBeatsProfileBeatsDefault pins the precedence order on each
// value separately: a flag or a provider block attribute, then the environment, then the
// selected profile, then the built-in default.
func TestExplicitBeatsEnvironmentBeatsProfileBeatsDefault(t *testing.T) {
	t.Run("endpoint", func(t *testing.T) {
		tests := []struct {
			name     string
			explicit string
			env      string
			want     string
			wantFrom string
		}{
			{name: "explicit wins", explicit: "https://flag.example.com", env: "https://env.example.com", want: "https://flag.example.com", wantFrom: "explicit"},
			{name: "the environment beats the profile", env: "https://env.example.com", want: "https://env.example.com", wantFrom: "environment " + envEndpoint},
			{name: "the profile is the floor", want: "https://profile.example.com", wantFrom: "profile 'default'"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				isolate(t)
				writeConfig(t, "default", map[string]profile.Profile{
					"default": {Endpoint: mustUrl("https://profile.example.com")},
				})
				t.Setenv(envEndpoint, test.env)

				// The profile is named, so that an explicit endpoint is not also asked to
				// select a profile — that rule has its own test.
				session := resolved(t, &fakeInput{values: Values{Profile: "default", Endpoint: test.explicit}})
				require.Equal(t, test.want, session.Endpoint.String())
				require.Equal(t, test.wantFrom, session.sources["endpoint"])
			})
		}

		t.Run("there is no built-in default endpoint", func(t *testing.T) {
			isolate(t)
			writeConfig(t, "default", map[string]profile.Profile{"default": {}})

			_, err := Resolve(t.Context(), &fakeInput{})
			p := problemOf(t, err)
			require.Equal(t, "meshStack endpoint is not configured", p.Summary())
			assert.Contains(t, p.Detail(), "--endpoint")
			assert.Contains(t, p.Detail(), envEndpoint)
		})
	})

	t.Run("workspace", func(t *testing.T) {
		tests := []struct {
			name     string
			explicit string
			env      string
			want     string
		}{
			{name: "explicit wins", explicit: "from-flag", env: "from-env", want: "from-flag"},
			{name: "the environment beats the profile", env: "from-env", want: "from-env"},
			{name: "the profile is the floor", want: "from-profile"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				isolate(t)
				writeConfig(t, "default", map[string]profile.Profile{
					"default": {Endpoint: mustUrl("https://api.example.com"), DefaultWorkspace: "from-profile"},
				})
				t.Setenv("MESHSTACK_WORKSPACE", test.env)

				session := resolved(t, &fakeInput{values: Values{Workspace: test.explicit}})
				require.Equal(t, test.want, session.Workspace)
			})
		}

		t.Run("there is no built-in default workspace", func(t *testing.T) {
			isolate(t)
			writeConfig(t, "default", map[string]profile.Profile{
				"default": {Endpoint: mustUrl("https://api.example.com")},
			})

			session := resolved(t, &fakeInput{})
			require.Empty(t, session.Workspace)
		})
	})

	t.Run("profile", func(t *testing.T) {
		tests := []struct {
			name     string
			explicit string
			env      string
			current  string
			want     string
		}{
			{name: "explicit wins", explicit: "named", env: "from-env", current: "current", want: "named"},
			{name: "the environment beats currentProfile", env: "from-env", current: "current", want: "from-env"},
			{name: "currentProfile beats the built-in default", current: "current", want: "current"},
			{name: "the built-in default is the floor", want: DefaultProfile},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				isolate(t)
				writeConfig(t, test.current, map[string]profile.Profile{
					"named":    {Endpoint: mustUrl("https://named.example.com")},
					"from-env": {Endpoint: mustUrl("https://from-env.example.com")},
					"current":  {Endpoint: mustUrl("https://current.example.com")},
					"default":  {Endpoint: mustUrl("https://default.example.com")},
				})
				t.Setenv(envProfile, test.env)

				session := resolved(t, &fakeInput{values: Values{Profile: test.explicit}})
				require.Equal(t, test.want, session.Profile)
			})
		}
	})
}

// TestAnApiTokenInTheEnvironmentResolvesToManualInMemoryAndWritesNothing is the
// CI-and-building-block rule: a credential that arrived whole never lands in a profile, and
// a process running on one needs no files at all.
func TestAnApiTokenInTheEnvironmentResolvesToManualInMemoryAndWritesNothing(t *testing.T) {
	at := isolate(t)
	t.Setenv(envEndpoint, "https://api.example.com")
	t.Setenv(envApiToken, "pasted-token")

	in := &fakeInput{token: fakeToken("pasted-token")}
	session := resolved(t, in)

	require.Equal(t, credential.MethodManual, session.Method())
	require.Empty(t, session.Profile, "a whole credential comes from above the profile layer")
	require.False(t, session.store.Writable())
	assert.Contains(t, session.store.Describe(), "memory")
	assert.Contains(t, session.sources["credential"], envApiToken)

	// Using the token has to leave the directory empty too, not just resolving it.
	token, err := session.BearerToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, fakeToken("pasted-token"), token)

	requireNothingStored(t, at)
}

// TestAnApiKeyAndSecretInTheEnvironmentResolveToApiKeyInMemoryAndWriteNothing is the same
// rule for the credential a CI job and the Terraform provider actually use.
func TestAnApiKeyAndSecretInTheEnvironmentResolveToApiKeyInMemoryAndWriteNothing(t *testing.T) {
	stack := newMeshStack(t)
	at := isolate(t)
	t.Setenv(envEndpoint, stack.URL.String())
	t.Setenv(envApiKey, "key-42")
	t.Setenv(envApiSecret, testSecret)

	session := resolved(t, &fakeInput{secret: testSecret})

	require.Equal(t, credential.MethodApiKey, session.Method())
	require.Empty(t, session.Profile)
	require.False(t, session.store.Writable())
	assert.Contains(t, session.sources["credential"], envApiKey)
	assert.Contains(t, session.sources["credential"], envApiSecret)

	token, err := session.BearerToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, "api-key-token-1", tokenId(token))

	requireNothingStored(t, at)
}

// TestAWholeCredentialNeverOverwritesAProfile holds the other half of the memory-store rule:
// where a profile does exist, a credential from the environment must not mix its identity
// into that profile's file.
func TestAWholeCredentialNeverOverwritesAProfile(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl(stack.URL.String())}})
	writeCredentials(t, profile.Credentials{
		Endpoint: mustUrl(stack.URL.String()),
		Credential: credential.FromLogin(credential.Login{
			Issuer: mustUrl(stack.URL.String()), RefreshToken: "somebody-elses-login",
		}),
	})
	path, err := profile.CredentialsPath("default")
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	t.Setenv(envEndpoint, stack.URL.String())
	t.Setenv(envApiKey, "key-42")
	t.Setenv(envApiSecret, testSecret)

	session := resolved(t, &fakeInput{secret: testSecret})
	require.Equal(t, credential.MethodApiKey, session.Method())
	_, err = session.BearerToken(t.Context())
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "the profile's file was written by a credential that did not come from it")
}

// TestWithoutAWholeCredentialResolutionFallsToTheProfile pins the profile layer's own order:
// an explicit name, then MESHSTACK_PROFILE, then a match on the endpoint, then
// currentProfile, then "default".
func TestWithoutAWholeCredentialResolutionFallsToTheProfile(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		envName  string
		envHost  string
		current  string
		want     string
		wantFrom string
	}{
		{name: "the named profile", explicit: "named", envName: "from-env", envHost: "https://matched.example.com", current: "current", want: "named", wantFrom: "explicit"},
		{name: "then MESHSTACK_PROFILE", envName: "from-env", envHost: "https://matched.example.com", current: "current", want: "from-env", wantFrom: "environment " + envProfile},
		{name: "then a match on the endpoint", envHost: "https://matched.example.com", current: "current", want: "matched", wantFrom: "profile matched on the endpoint"},
		{name: "then currentProfile", current: "current", want: "current"},
		{name: "then default", want: DefaultProfile, wantFrom: "built-in default profile default"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolate(t)
			writeConfig(t, test.current, map[string]profile.Profile{
				"named":    {Endpoint: mustUrl("https://named.example.com")},
				"from-env": {Endpoint: mustUrl("https://from-env.example.com")},
				"matched":  {Endpoint: mustUrl("https://matched.example.com")},
				"current":  {Endpoint: mustUrl("https://current.example.com")},
				"default":  {Endpoint: mustUrl("https://default.example.com")},
			})
			t.Setenv(envProfile, test.envName)
			t.Setenv(envEndpoint, test.envHost)

			session := resolved(t, &fakeInput{values: Values{Profile: test.explicit}})
			require.Equal(t, test.want, session.Profile)
			if test.wantFrom != "" {
				require.Equal(t, test.wantFrom, session.sources["profile"])
			}
		})
	}
}

// TestSelectingAProfileByEndpoint pins the three rows of the endpoint-matching table.
func TestSelectingAProfileByEndpoint(t *testing.T) {
	t.Run("exactly one match is used", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{
			"live":  {Endpoint: mustUrl("https://api.live.example.com")},
			"other": {Endpoint: mustUrl("https://api.other.example.com")},
		})
		// The comparison is by scheme, host and port, so case and a trailing slash do not
		// stop a profile from matching.
		t.Setenv(envEndpoint, "https://API.live.example.com/")

		logs := captureLogs(t)
		session := resolved(t, &fakeInput{})
		require.Equal(t, "live", session.Profile)
		require.Equal(t, "profile matched on the endpoint", session.sources["profile"])
		// The log record is the whole of what a person gets told, now that a warning has no
		// other way out of this package. A silent match would leave a `terraform apply` whose
		// identity depends on which profiles exist on the machine saying nothing at all.
		require.Contains(t, logs.warnings(), "picked a profile by endpoint")
	})

	t.Run("several matches stop and name --profile", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{
			"alpha": {Endpoint: mustUrl("https://api.live.example.com")},
			"beta":  {Endpoint: mustUrl("https://api.live.example.com/")},
			"other": {Endpoint: mustUrl("https://api.other.example.com")},
		})
		t.Setenv(envEndpoint, "https://api.live.example.com")

		_, err := Resolve(t.Context(), &fakeInput{})
		p := problemOf(t, err)
		require.Equal(t, "several profiles match this endpoint", p.Summary())
		assert.Contains(t, p.Detail(), `"alpha"`)
		assert.Contains(t, p.Detail(), `"beta"`)
		assert.NotContains(t, p.Detail(), `"other"`)
		assert.Contains(t, p.Detail(), "--profile")
	})

	t.Run("no match and no whole credential names the endpoint", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"other": {Endpoint: mustUrl("https://api.other.example.com")}})
		t.Setenv(envEndpoint, "https://api.nowhere.example.com")

		_, err := Resolve(t.Context(), &fakeInput{})
		p := problemOf(t, err)
		require.Equal(t, "no profile for this endpoint", p.Summary())
		assert.Contains(t, p.Detail(), "https://api.nowhere.example.com")
		assert.Contains(t, p.Detail(), envApiKey)
	})

	t.Run("no match with a whole credential keeps CI working", func(t *testing.T) {
		at := isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"other": {Endpoint: mustUrl("https://api.other.example.com")}})
		t.Setenv(envEndpoint, "https://api.nowhere.example.com")
		t.Setenv(envApiKey, "key-42")
		t.Setenv(envApiSecret, testSecret)

		session := resolved(t, &fakeInput{secret: testSecret})
		require.Equal(t, credential.MethodApiKey, session.Method())
		require.Empty(t, session.Profile)
		require.False(t, session.store.Writable())
		require.NoDirExists(t, at.credentials)
	})
}

// TestAnUnknownProfileFailsExceptWhenLoggingIn holds the rule that `auth login` is the only
// command that creates a profile, so a mistyped --profile is an error everywhere else.
func TestAnUnknownProfileFailsExceptWhenLoggingIn(t *testing.T) {
	newInput := func() *fakeInput {
		return &fakeInput{values: Values{Profile: "typo", Endpoint: "https://api.example.com"}}
	}

	t.Run("an ordinary command refuses to create one", func(t *testing.T) {
		at := isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"live": {Endpoint: mustUrl("https://api.example.com")}})

		_, err := Resolve(t.Context(), newInput())
		p := problemOf(t, err)
		require.Equal(t, "unknown profile", p.Summary())
		assert.Contains(t, p.Detail(), `"typo"`)
		assert.Contains(t, p.Detail(), "meshstack auth login --profile typo")
		require.NoDirExists(t, at.credentials)
	})

	t.Run("auth login accepts a name that does not exist yet", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"live": {Endpoint: mustUrl("https://api.example.com")}})

		session, err := ResolveForLogin(t.Context(), newInput())
		require.NoError(t, err)
		require.Equal(t, "typo", session.Profile)
		require.Equal(t, "https://api.example.com", session.Endpoint.String())
		require.True(t, session.store.Writable())
	})
}

// TestACredentialForAnotherEndpointIsRefusedBeforeItIsUsed holds the check that keeps a
// stored bearer token from reaching a different meshStack after a profile is repointed.
func TestACredentialForAnotherEndpointIsRefusedBeforeItIsUsed(t *testing.T) {
	isolate(t)
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl("https://api.new.example.com")}})
	writeCredentials(t, profile.Credentials{
		Endpoint: mustUrl("https://api.old.example.com"),
		Credential: credential.FromManual(credential.Manual{
			AccessToken: credential.IssuedToken{Token: mustJwt(fakeToken("a-token-for-the-old-instance")), ExpiresAt: futureExpiry()},
		}),
	})

	_, err := Resolve(t.Context(), &fakeInput{})
	p := problemOf(t, err)
	require.Equal(t, "this credential belongs to a different meshStack", p.Summary())
	assert.Contains(t, p.Detail(), "https://api.old.example.com")
	assert.Contains(t, p.Detail(), "https://api.new.example.com")
	assert.Contains(t, p.Detail(), "--profile")
	assert.NotContains(t, p.Detail(), "a-token-for-the-old-instance")
}

// TestResolveForLoginUsesTheProfileStoreEvenWithAWholeCredential holds the one way
// `auth login` differs: writing a profile is its purpose, so MESHSTACK_API_SECRET reaches
// disk when it puts it there, and never otherwise.
func TestResolveForLoginUsesTheProfileStoreEvenWithAWholeCredential(t *testing.T) {
	isolate(t)
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl("https://api.example.com")}})
	t.Setenv(envEndpoint, "https://api.example.com")
	t.Setenv(envApiToken, "pasted-token")

	ordinary := resolved(t, &fakeInput{token: fakeToken("pasted-token")})
	require.False(t, ordinary.store.Writable(), "an ordinary command keeps a whole credential in memory")
	require.Empty(t, ordinary.Profile)

	forLogin, err := ResolveForLogin(t.Context(), &fakeInput{token: fakeToken("pasted-token")})
	require.NoError(t, err)
	require.Equal(t, "default", forLogin.Profile)
	require.True(t, forLogin.store.Writable())
	expected, err := profile.CredentialsPath("default")
	require.NoError(t, err)
	require.Equal(t, expected, forLogin.store.Describe())
}

// TestProfileSuppliesTheEndpointForAnEnvironmentCredential pins the rule that the endpoint and
// the workspace are their own axes of the precedence order. A credential is resolved as a whole,
// but that is about the credential — an endpoint sitting in config.yaml still applies, and the
// store stays a memory store because the credential did not come from the profile.
func TestProfileSuppliesTheEndpointForAnEnvironmentCredential(t *testing.T) {
	at := isolate(t)
	writeConfig(t, "dev", map[string]profile.Profile{
		"dev": {Endpoint: mustUrl("https://api.dev.example.com"), DefaultWorkspace: "from-the-profile"},
	})
	t.Setenv(envApiToken, "a-token")

	session, err := Resolve(t.Context(), &fakeInput{})
	require.NoError(t, err)
	assert.Equal(t, "https://api.dev.example.com", session.Endpoint.String())
	assert.Equal(t, "from-the-profile", session.Workspace)
	assert.Equal(t, credential.MethodManual, session.Method())

	entries, err := os.ReadDir(at.credentials)
	require.True(t, err != nil || len(entries) == 0, "a credential from the environment must leave no file behind")
}

// TestANamedProfileBeatsAnEnvironmentCredential pins which layer a --profile flag sits in. It is
// the top one, alongside a provider block attribute, so naming a profile decides the credential
// even where the environment holds a complete one. MESHSTACK_PROFILE does not: it sits in the
// environment layer next to MESHSTACK_API_KEY, so neither outranks the other.
func TestANamedProfileBeatsAnEnvironmentCredential(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{
			"dev": {Endpoint: mustUrl("https://api.dev.example.com"), DefaultWorkspace: "from-the-profile"},
		})
		t.Setenv(envEndpoint, "https://api.env.example.com")
		t.Setenv(envApiKey, "an-id")
		t.Setenv(envApiSecret, "a-secret")
	}

	t.Run("the flag wins", func(t *testing.T) {
		setup(t)
		session, err := Resolve(t.Context(), &fakeInput{values: Values{Profile: "dev"}})
		require.NoError(t, err)
		assert.Equal(t, "dev", session.Profile)
		assert.Equal(t, "https://api.env.example.com", session.Endpoint.String(),
			"the endpoint is its own axis, and the environment still outranks the profile there")
		assert.Equal(t, sourceProfile.describe("'dev'"), session.sources["credential"])
	})

	t.Run("the environment variable does not", func(t *testing.T) {
		setup(t)
		t.Setenv(envProfile, "dev")
		session, err := Resolve(t.Context(), &fakeInput{})
		require.NoError(t, err)
		assert.Empty(t, session.Profile, "an environment credential and MESHSTACK_PROFILE are the same layer")
		assert.Equal(t, credential.MethodApiKey, session.Method())
	})
}
