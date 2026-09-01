package auth

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
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
			{name: "explicit wins", explicit: "https://flag.example.com", env: "https://env.example.com", want: "https://flag.example.com", wantFrom: "explicit " + meshstack.Endpoint.EnvKey},
			{name: "the environment beats the profile", env: "https://env.example.com", want: "https://env.example.com", wantFrom: meshstack.Endpoint.EnvKey},
			{name: "the profile is the floor", want: "https://profile.example.com", wantFrom: "profile default"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				isolate(t)
				writeConfig(t, "default", map[string]profile.Profile{
					"default": {Endpoint: mustUrl("https://profile.example.com")},
				})
				t.Setenv(meshstack.Endpoint.EnvKey, test.env)

				// The profile is named, so that an explicit endpoint is not also asked to
				// select a profile — that rule has its own test.
				session := resolved(t, ResolveSessionOptions{Settings: source{
					profile.Name.EnvKey:       "default",
					meshstack.Endpoint.EnvKey: test.explicit,
				}})
				require.Equal(t, test.want, session.Endpoint.String())
				require.Equal(t, test.wantFrom, originOf(session, meshstack.Endpoint.EnvKey))
			})
		}

		t.Run("there is no built-in default endpoint", func(t *testing.T) {
			isolate(t)
			writeConfig(t, "default", map[string]profile.Profile{"default": {}})

			_, err := ResolveSession(t.Context(), ResolveSessionOptions{})
			p := problemOf(t, err)
			require.Equal(t, "meshStack endpoint is not configured", p.Summary())
			assert.Contains(t, p.Detail(), meshstack.Endpoint.EnvKey)
			assert.Contains(t, p.Detail(), meshstack.Endpoint.EnvKey)
		})
	})

	t.Run("workspace", func(t *testing.T) {
		tests := []struct {
			name     string
			explicit string
			env      string
			want     string
			wantFrom string
		}{
			{name: "explicit wins", explicit: "from-flag", env: "from-env", want: "from-flag", wantFrom: "explicit " + meshstack.Workspace.EnvKey},
			{name: "the environment beats the profile", env: "from-env", want: "from-env", wantFrom: meshstack.Workspace.EnvKey},
			{name: "the profile is the floor", want: "from-profile", wantFrom: "profile default"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				isolate(t)
				writeConfig(t, "default", map[string]profile.Profile{
					"default": {Endpoint: mustUrl("https://api.example.com"), DefaultWorkspace: "from-profile"},
				})
				t.Setenv(meshstack.Workspace.EnvKey, test.env)

				session := resolved(t, ResolveSessionOptions{
					Settings: source{meshstack.Workspace.EnvKey: test.explicit},
				})
				require.Equal(t, test.want, session.Workspace)
				require.Equal(t, test.wantFrom, originOf(session, meshstack.Workspace.EnvKey))
			})
		}

		t.Run("there is no built-in default workspace", func(t *testing.T) {
			isolate(t)
			writeConfig(t, "default", map[string]profile.Profile{
				"default": {Endpoint: mustUrl("https://api.example.com")},
			})

			session := resolved(t, ResolveSessionOptions{})
			require.Empty(t, session.Workspace)
			require.Empty(t, originOf(session, meshstack.Workspace.EnvKey))
		})
	})

	t.Run("profile", func(t *testing.T) {
		tests := []struct {
			name     string
			explicit string
			env      string
			current  string
			want     string
			wantFrom string
		}{
			{name: "explicit wins", explicit: "named", env: "from-env", current: "current", want: "named", wantFrom: "explicit " + profile.Name.EnvKey},
			{name: "the environment beats currentProfile", env: "from-env", current: "current", want: "from-env", wantFrom: profile.Name.EnvKey},
			{name: "currentProfile beats the built-in default", current: "current", want: "current"},
			{name: "the built-in default is the floor", want: profile.DefaultName, wantFrom: "built-in default"},
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
				t.Setenv(profile.Name.EnvKey, test.env)

				session := resolved(t, ResolveSessionOptions{
					Settings: source{profile.Name.EnvKey: test.explicit},
				})
				require.Equal(t, test.want, session.Profile)
				if test.wantFrom != "" {
					require.Equal(t, test.wantFrom, originOf(session, profile.Name.EnvKey))
				}
			})
		}
	})
}

// TestACredentialResolvesAsAUnit walks the four rows of the unit rule. Row 1 is the case to
// protect rather than an edge case: an id in the provider block, or on --api-key, with the
// secret in the environment is the normal non-interactive setup.
func TestACredentialResolvesAsAUnit(t *testing.T) {
	t.Run("an explicit id pairs with the environment's secret", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		session := resolved(t, ResolveSessionOptions{
			Settings: source{credential.ApiKeyId.EnvKey: "block-key"},
		})

		require.Equal(t, credential.MethodApiKey, session.Method())
		require.Equal(t, "block-key", session.resolved.ApiKey.Id)
		require.Equal(t, testSecret, session.resolved.ApiKey.Secret)
		assert.Equal(t, "explicit "+credential.ApiKeyId.EnvKey, originOf(session, credential.ApiKeyId.EnvKey))
		assert.Equal(t, credential.ApiSecret.EnvKey, originOf(session, credential.ApiSecret.EnvKey))
	})

	t.Run("an id with no secret anywhere is ErrNoApiSecret", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")

		_, err := ResolveSession(t.Context(), ResolveSessionOptions{
			Settings: source{credential.ApiKeyId.EnvKey: "flag-key"},
		})

		require.ErrorIs(t, err, ErrNoApiSecret)
		p := problemOf(t, err)
		assert.Contains(t, p.Detail(), "flag-key")
		assert.Contains(t, p.Detail(), "--api-secret-stdin")
	})

	t.Run("the profile's own secret slot wins before anything else is asked", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "dev", map[string]profile.Profile{"dev": {Endpoint: mustUrl("https://api.example.com")}})
		writeCredentialsFor(t, "dev", profile.Credentials{
			Endpoint:   mustUrl("https://api.example.com"),
			Credential: credential.FromApiKey(credential.ApiKey{Id: "dev-key", Secret: testSecret}),
		})
		t.Setenv(credential.ApiSecret.EnvKey, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

		logs := captureLogs(t)
		session := resolved(t, ResolveSessionOptions{})

		require.Equal(t, testSecret, session.resolved.ApiKey.Secret, "the stored secret is the one paired with the stored id")
		assert.Equal(t, "profile dev", originOf(session, credential.ApiSecret.EnvKey))
		// A rotated secret in the shell beside a profile still serving the old one otherwise
		// looks exactly like a revoked key.
		require.Contains(t, logs.warnings(), "the stored API key secret is being used")
	})

	t.Run("a competing id in the environment does not lend its secret", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		t.Setenv(credential.ApiKeyId.EnvKey, "stale-key")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		_, err := ResolveSession(t.Context(), ResolveSessionOptions{
			Settings: source{credential.ApiKeyId.EnvKey: "block-key"},
		})

		require.ErrorIs(t, err, ErrNoApiSecret)
		p := problemOf(t, err)
		assert.Contains(t, p.Detail(), "block-key")
		// Quoting the losing id is deliberate: a client id is not a secret, and it is the
		// fact that identifies which stale export to remove.
		assert.Contains(t, p.Detail(), "stale-key")
		assert.Contains(t, p.Detail(), credential.ApiKeyId.EnvKey)
	})
}

// TestOneSourceCarryingBothIsAnError holds the rule that a token and a key id are two methods
// rather than two spellings of one thing, so choosing silently would hand the user an
// identity they did not pick.
func TestOneSourceCarryingBothIsAnError(t *testing.T) {
	isolate(t)
	t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
	t.Setenv(credential.ApiKeyId.EnvKey, "a-key")
	t.Setenv(credential.ApiToken.EnvKey, fakeToken("a-token"))

	_, err := ResolveSession(t.Context(), ResolveSessionOptions{})

	p := problemOf(t, err)
	require.Equal(t, "two authentication methods from one place", p.Summary())
	assert.Contains(t, p.Detail(), credential.ApiKeyId.EnvKey)
	assert.Contains(t, p.Detail(), credential.ApiToken.EnvKey)
}

// TestAnExplicitSecretOutranksTheEnvironmentAndSaysSo is `--api-secret-stdin` beside an
// exported MESHSTACK_API_SECRET. It is a warning rather than a refusal, because refusing
// would break exactly the CI runs that export the variable on purpose.
func TestAnExplicitSecretOutranksTheEnvironmentAndSaysSo(t *testing.T) {
	isolate(t)
	t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
	t.Setenv(credential.ApiSecret.EnvKey, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	logs := captureLogs(t)
	session := resolved(t, ResolveSessionOptions{Settings: source{
		credential.ApiKeyId.EnvKey:  "a-key",
		credential.ApiSecret.EnvKey: testSecret,
	}})

	require.Equal(t, testSecret, session.resolved.ApiKey.Secret)
	assert.Equal(t, "explicit "+credential.ApiSecret.EnvKey, originOf(session, credential.ApiSecret.EnvKey))
	require.Contains(t, logs.warnings(), "another API key secret is set and ignored")
}

// TestAnApiTokenInTheEnvironmentResolvesToManualInMemoryAndWritesNothing is the
// CI-and-building-block rule: a credential that arrived from above never lands in a profile,
// and a process running on one needs no files at all.
func TestAnApiTokenInTheEnvironmentResolvesToManualInMemoryAndWritesNothing(t *testing.T) {
	at := isolate(t)
	t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
	t.Setenv(credential.ApiToken.EnvKey, fakeToken("pasted-token"))

	session := resolved(t, ResolveSessionOptions{})

	require.Equal(t, credential.MethodManual, session.Method())
	require.False(t, session.store.Writable())
	assert.Contains(t, session.store.Describe(), "memory")
	assert.Equal(t, credential.ApiToken.EnvKey, originOf(session, credential.ApiToken.EnvKey))

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
	t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
	t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
	t.Setenv(credential.ApiSecret.EnvKey, testSecret)

	session := resolved(t, ResolveSessionOptions{})

	require.Equal(t, credential.MethodApiKey, session.Method())
	require.False(t, session.store.Writable())
	assert.Equal(t, credential.ApiKeyId.EnvKey, originOf(session, credential.ApiKeyId.EnvKey))
	assert.Equal(t, credential.ApiSecret.EnvKey, originOf(session, credential.ApiSecret.EnvKey))

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

	t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
	t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
	t.Setenv(credential.ApiSecret.EnvKey, testSecret)

	session := resolved(t, ResolveSessionOptions{})
	require.Equal(t, credential.MethodApiKey, session.Method())
	_, err = session.BearerToken(t.Context())
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "the profile's file was written by a credential that did not come from it")
}

// TestAnExportedCredentialVariableFixesTheIdentity is the behaviour change: it used to be
// ignored where a profile held a browser login, so the user acted under an identity they did
// not ask for with nothing said.
func TestAnExportedCredentialVariableFixesTheIdentity(t *testing.T) {
	isolate(t)
	writeConfig(t, "dev", map[string]profile.Profile{"dev": {Endpoint: mustUrl("https://api.example.com")}})
	writeCredentialsFor(t, "dev", profile.Credentials{
		Endpoint:   mustUrl("https://api.example.com"),
		Credential: credential.FromLogin(credential.Login{RefreshToken: "a-browser-login"}),
	})
	t.Setenv(credential.ApiKeyId.EnvKey, "exported-key")

	_, err := ResolveSession(t.Context(), ResolveSessionOptions{})

	require.ErrorIs(t, err, ErrNoApiSecret)
	assert.Contains(t, problemOf(t, err).Detail(), "exported-key")
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
		t.Setenv(meshstack.Endpoint.EnvKey, "https://API.live.example.com/")

		logs := captureLogs(t)
		session := resolved(t, ResolveSessionOptions{})
		require.Equal(t, "live", session.Profile)
		assert.Contains(t, originOf(session, profile.Name.EnvKey), "the only profile for")
		// The log record is the whole of what a person gets told, now that a warning has no
		// other way out of this package. A silent match would leave a `terraform apply` whose
		// identity depends on which profiles exist on the machine saying nothing at all.
		require.Contains(t, logs.warnings(), "picked a profile by endpoint")
	})

	t.Run("several matches stop and name the profile setting", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{
			"alpha": {Endpoint: mustUrl("https://api.live.example.com")},
			"beta":  {Endpoint: mustUrl("https://api.live.example.com/")},
			"other": {Endpoint: mustUrl("https://api.other.example.com")},
		})
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.live.example.com")

		_, err := ResolveSession(t.Context(), ResolveSessionOptions{})
		p := problemOf(t, err)
		require.Equal(t, "several profiles match this endpoint", p.Summary())
		assert.Contains(t, p.Detail(), `"alpha"`)
		assert.Contains(t, p.Detail(), `"beta"`)
		assert.NotContains(t, p.Detail(), `"other"`)
		assert.Contains(t, p.Detail(), profile.Name.EnvKey)
	})

	t.Run("no match and no credential from above names the endpoint", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"other": {Endpoint: mustUrl("https://api.other.example.com")}})
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.nowhere.example.com")

		_, err := ResolveSession(t.Context(), ResolveSessionOptions{})
		p := problemOf(t, err)
		require.Equal(t, "no profile for this endpoint", p.Summary())
		assert.Contains(t, p.Detail(), "https://api.nowhere.example.com")
		assert.Contains(t, p.Detail(), credential.ApiKeyId.EnvKey)
	})

	t.Run("no match with a credential from above keeps CI working", func(t *testing.T) {
		at := isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"other": {Endpoint: mustUrl("https://api.other.example.com")}})
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.nowhere.example.com")
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		session := resolved(t, ResolveSessionOptions{})
		require.Equal(t, credential.MethodApiKey, session.Method())
		require.False(t, session.store.Writable())
		require.NoDirExists(t, at.credentials)
	})
}

// TestAnUnknownProfileFailsUnlessAStoreSaysItIsAboutToBeWritten holds the rule that `auth
// login` is the only command that creates a profile. Handing a store in is what says so.
func TestAnUnknownProfileFailsUnlessAStoreSaysItIsAboutToBeWritten(t *testing.T) {
	named := source{profile.Name.EnvKey: "typo", meshstack.Endpoint.EnvKey: "https://api.example.com"}

	t.Run("an ordinary command refuses to create one", func(t *testing.T) {
		at := isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"live": {Endpoint: mustUrl("https://api.example.com")}})

		_, err := ResolveSession(t.Context(), ResolveSessionOptions{Settings: named})
		p := problemOf(t, err)
		require.Equal(t, "unknown profile", p.Summary())
		assert.Contains(t, p.Detail(), `"typo"`)
		assert.Contains(t, p.Detail(), "meshstack auth login --profile typo")
		require.NoDirExists(t, at.credentials)
	})

	t.Run("a command holding the store accepts a name that does not exist yet", func(t *testing.T) {
		isolate(t)
		writeConfig(t, "", map[string]profile.Profile{"live": {Endpoint: mustUrl("https://api.example.com")}})

		session := resolved(t, ResolveSessionOptions{Settings: named, Store: profileStore(t, "typo")})
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

	_, err := ResolveSession(t.Context(), ResolveSessionOptions{})
	p := problemOf(t, err)
	require.Equal(t, "this credential belongs to a different meshStack", p.Summary())
	assert.Contains(t, p.Detail(), "https://api.old.example.com")
	assert.Contains(t, p.Detail(), "https://api.new.example.com")
	assert.Contains(t, p.Detail(), profile.Name.EnvKey)
	assert.NotContains(t, p.Detail(), "a-token-for-the-old-instance")
}

// TestTheCredentialsFileIsNotOpenedWhenNothingNeedsIt is why the profile is two sources
// rather than one. Without the short-circuit, a machine whose default profile points at
// another meshStack would start failing runs whose credential came wholly from above.
func TestTheCredentialsFileIsNotOpenedWhenNothingNeedsIt(t *testing.T) {
	isolate(t)
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl("https://api.new.example.com")}})
	writeCredentials(t, profile.Credentials{
		Endpoint:   mustUrl("https://api.old.example.com"),
		Credential: credential.FromLogin(credential.Login{RefreshToken: "for-the-old-instance"}),
	})
	t.Setenv(meshstack.Endpoint.EnvKey, "https://api.new.example.com")
	t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
	t.Setenv(credential.ApiSecret.EnvKey, testSecret)

	session := resolved(t, ResolveSessionOptions{})
	require.Equal(t, credential.MethodApiKey, session.Method())
}

// TestAStoreHandedInIsUsedWhateverSuppliedTheCredential holds the one way `auth login`
// differs: writing a profile is its purpose, so MESHSTACK_API_SECRET reaches disk when it
// puts it there, and never otherwise.
func TestAStoreHandedInIsUsedWhateverSuppliedTheCredential(t *testing.T) {
	isolate(t)
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl("https://api.example.com")}})
	t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
	t.Setenv(credential.ApiToken.EnvKey, fakeToken("pasted-token"))

	ordinary := resolved(t, ResolveSessionOptions{})
	require.False(t, ordinary.store.Writable(), "an ordinary command keeps a credential from above in memory")

	forLogin := resolved(t, ResolveSessionOptions{Store: profileStore(t, "default")})
	require.True(t, forLogin.store.Writable())
	expected, err := profile.CredentialsPath("default")
	require.NoError(t, err)
	require.Equal(t, expected, forLogin.store.Describe())
}

// TestProfileSuppliesTheEndpointAndWorkspaceForAnEnvironmentCredential pins the rule that the
// endpoint and the workspace are their own axes of the precedence order. A credential is
// resolved as a whole, but that is about the credential.
func TestProfileSuppliesTheEndpointAndWorkspaceForAnEnvironmentCredential(t *testing.T) {
	at := isolate(t)
	writeConfig(t, "dev", map[string]profile.Profile{
		"dev": {Endpoint: mustUrl("https://api.dev.example.com"), DefaultWorkspace: "from-the-profile"},
	})
	t.Setenv(credential.ApiToken.EnvKey, fakeToken("a-token"))

	session := resolved(t, ResolveSessionOptions{})
	assert.Equal(t, "https://api.dev.example.com", session.Endpoint.String())
	assert.Equal(t, "from-the-profile", session.Workspace)
	assert.Equal(t, credential.MethodManual, session.Method())

	entries, err := os.ReadDir(at.credentials)
	require.True(t, err != nil || len(entries) == 0, "a credential from the environment must leave no file behind")
}

// TestANamedProfileNoLongerBeatsAnEnvironmentCredential is the other half of the behaviour
// change: --profile still decides which endpoint and workspace apply, but an exported
// credential variable decides who you are.
func TestANamedProfileNoLongerBeatsAnEnvironmentCredential(t *testing.T) {
	isolate(t)
	writeConfig(t, "", map[string]profile.Profile{
		"dev": {Endpoint: mustUrl("https://api.dev.example.com"), DefaultWorkspace: "from-the-profile"},
	})
	writeCredentialsFor(t, "dev", profile.Credentials{
		Endpoint:   mustUrl("https://api.dev.example.com"),
		Credential: credential.FromLogin(credential.Login{RefreshToken: "a-browser-login"}),
	})
	t.Setenv(credential.ApiKeyId.EnvKey, "an-id")
	t.Setenv(credential.ApiSecret.EnvKey, testSecret)

	session := resolved(t, ResolveSessionOptions{Settings: source{profile.Name.EnvKey: "dev"}})

	assert.Equal(t, "dev", session.Profile)
	assert.Equal(t, "https://api.dev.example.com", session.Endpoint.String(), "the profile still supplies the endpoint")
	assert.Equal(t, "from-the-profile", session.Workspace)
	assert.Equal(t, credential.MethodApiKey, session.Method())
	assert.False(t, session.store.Writable(), "an identity from the environment never reaches a profile's file")
}

// TestADemandedMethodFiltersEverySource is what keeps a bare `meshstack login` from resolving
// an exported API key and refusing to open a browser.
func TestADemandedMethodFiltersEverySource(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		isolate(t)
		writeConfig(t, "dev", map[string]profile.Profile{"dev": {Endpoint: mustUrl("https://api.example.com")}})
		writeCredentialsFor(t, "dev", profile.Credentials{
			Endpoint: mustUrl("https://api.example.com"),
			Credential: credential.Credential{
				Current: credential.MethodApiKey,
				Login:   &credential.Login{RefreshToken: "a-browser-login"},
				ApiKey:  &credential.ApiKey{Id: "stored-key", Secret: testSecret},
			},
		})
		t.Setenv(credential.ApiKeyId.EnvKey, "exported-key")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)
	}

	t.Run("login passes over an exported API key", func(t *testing.T) {
		setup(t)
		session := resolved(t, ResolveSessionOptions{
			DemandMethod: credential.MethodLogin, Store: profileStore(t, "dev"),
		})
		require.Equal(t, credential.MethodLogin, session.Method())
		require.Equal(t, "a-browser-login", session.resolved.Login.RefreshToken)
	})

	// A bare --api-key demands the method without supplying an id, which is what lets the
	// environment's id win over the profile's stored one.
	t.Run("a bare --api-key takes the exported id over the stored one", func(t *testing.T) {
		setup(t)
		session := resolved(t, ResolveSessionOptions{
			DemandMethod: credential.MethodApiKey, Store: profileStore(t, "dev"),
		})
		require.Equal(t, "exported-key", session.resolved.ApiKey.Id)
	})

	t.Run("a bare --api-key falls back to the stored id", func(t *testing.T) {
		setup(t)
		t.Setenv(credential.ApiKeyId.EnvKey, "")
		t.Setenv(credential.ApiSecret.EnvKey, "")
		session := resolved(t, ResolveSessionOptions{
			DemandMethod: credential.MethodApiKey, Store: profileStore(t, "dev"),
		})
		require.Equal(t, "stored-key", session.resolved.ApiKey.Id)
		require.Equal(t, testSecret, session.resolved.ApiKey.Secret)
	})

	// A token cannot be renewed, so demanding the method is asking for a new one and the
	// profile's stored token is not an answer to it.
	t.Run("--api-token asks for a token even where one is stored", func(t *testing.T) {
		setup(t)
		writeCredentialsFor(t, "dev", profile.Credentials{
			Endpoint: mustUrl("https://api.example.com"),
			Credential: credential.FromManual(credential.Manual{
				AccessToken: credential.IssuedToken{Token: mustJwt(fakeToken("stored")), ExpiresAt: futureExpiry()},
			}),
		})
		t.Setenv(credential.ApiKeyId.EnvKey, "")

		_, err := ResolveSession(t.Context(), ResolveSessionOptions{
			DemandMethod: credential.MethodManual, Store: profileStore(t, "dev"),
		})
		require.ErrorIs(t, err, ErrNoApiToken)
	})
}

// TestTheThreeSentinelsAreWhatALoginSees pins the contract cmd/auth/login.go matches on:
// errors.Is has to find each of them through the diags.Problem that carries the wording.
func TestTheThreeSentinelsAreWhatALoginSees(t *testing.T) {
	t.Run("no endpoint", func(t *testing.T) {
		isolate(t)
		_, err := ResolveSession(t.Context(), ResolveSessionOptions{Store: profileStore(t, "default")})
		require.ErrorIs(t, err, ErrNoEndpoint)
	})

	t.Run("no API key secret", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		_, err := ResolveSession(t.Context(), ResolveSessionOptions{
			Settings:     source{credential.ApiKeyId.EnvKey: "a-key"},
			DemandMethod: credential.MethodApiKey,
			Store:        profileStore(t, "default"),
		})
		require.ErrorIs(t, err, ErrNoApiSecret)
	})

	t.Run("no API token", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		_, err := ResolveSession(t.Context(), ResolveSessionOptions{
			DemandMethod: credential.MethodManual,
			Store:        profileStore(t, "default"),
		})
		require.ErrorIs(t, err, ErrNoApiToken)
	})
}
