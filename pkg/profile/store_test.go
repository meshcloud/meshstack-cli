package profile

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

// unchanged is the mint a caller passes when it only wants the read-lock-write cycle,
// which the store must still handle: it prunes and writes rather than comparing.
func unchanged(c Credentials) (Credentials, error) { return c, nil }

func newTestStore(t *testing.T, name string) Store {
	t.Helper()
	isolate(t)
	store, err := NewFileStore(name)
	require.NoError(t, err)
	return store
}

func TestNewFileStoreCreatesNothing(t *testing.T) {
	dir := isolate(t)

	store, err := NewFileStore("default")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "meshstack", "credentials", "default.json"), store.Describe())
	require.True(t, store.Writable())

	creds, err := store.Read()
	require.NoError(t, err)
	// A profile with no credentials file is "not logged in", not an error.
	require.Equal(t, Credentials{Version: Version}, creds)

	_, err = os.Stat(filepath.Join(dir, "meshstack"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCredentialsRoundTrip(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	obtained := time.Date(2026, 8, 27, 9, 1, 0, 0, time.UTC)
	expires := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	want := Credentials{
		Version:  Version,
		Endpoint: mustUrl("https://api.dev.meshcloud.io"),
		Credential: credential.Credential{
			Current: credential.MethodLogin,
			Login: &credential.Login{
				Issuer:       mustUrl("https://sso.dev.meshcloud.io/auth/realms/meshfed"),
				RefreshToken: "refresh-1",
				ObtainedAt:   obtained,
				AccessTokens: map[scope.Scope]credential.IssuedToken{
					meshstack.Unscoped:                       {Token: fakeJwt("token-unscoped"), ExpiresAt: expires},
					meshstack.WorkspaceScope("my-workspace"): {Token: fakeJwt("token-scoped"), ExpiresAt: expires},
				},
			},
			ApiKey: &credential.ApiKey{
				Id:            "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a74",
				SecretCommand: []string{"vault", "kv", "get", "-field=secret", "concourse/meshstack-dev"},
			},
		},
	}

	written, err := store.Update(t.Context(), func(Credentials) (Credentials, error) { return want, nil })
	require.NoError(t, err)
	require.Equal(t, want, written)

	raw, err := os.ReadFile(filepath.Join(dir, "meshstack", "credentials", "default.json"))
	require.NoError(t, err)
	text := string(raw)
	assert.Contains(t, text, `"version": 1`)
	assert.Contains(t, text, `"current": "login"`)
	// The embedded credential is inlined: its members sit beside the version rather than
	// under a key named after the type.
	assert.NotContains(t, text, `"Credential"`)
	assert.Contains(t, text, `"refreshToken": "refresh-1"`)
	assert.Contains(t, text, `"clientSecretCommand"`)
	assert.Contains(t, text, `"c:my-workspace"`)
	assert.Contains(t, text, `"unscoped"`)
	// clientSecret is omitted entirely when a command supplies it.
	assert.NotContains(t, text, `"clientSecret"`)

	got, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestUpdateWritesOnlyTheVersionOfAnEmptyCredential(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	_, err = store.Update(t.Context(), unchanged)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "meshstack", "credentials", "default.json"))
	require.NoError(t, err)
	require.Equal(t, "{\n  \"version\": 1\n}\n", string(raw))

	got, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, Credentials{Version: Version}, got)
}

func TestCredentialsFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes")
	}
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	_, err = store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Endpoint = mustUrl("https://api.dev.meshcloud.io")
		return c, nil
	})
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, "meshstack", "credentials", "default.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Join(dir, "meshstack", "credentials"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
}

func TestReadRejectsANewerVersion(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	path := filepath.Join(dir, "meshstack", "credentials", "default.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode))
	require.NoError(t, os.WriteFile(path, []byte(`{"version": 99, "endpoint": "https://example.com"}`), fileMode))

	_, err = store.Read()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version 99")
	assert.Contains(t, err.Error(), path)
}

func TestUpdatePrunesExpiredTokens(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	fresh := credential.IssuedToken{Token: fakeJwt("fresh"), ExpiresAt: time.Now().Add(5 * time.Minute).UTC()}
	stale := credential.IssuedToken{Token: fakeJwt("stale"), ExpiresAt: time.Now().Add(-time.Second).UTC()}

	got, err := store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Credential = credential.FromLogin(credential.Login{
			AccessTokens: map[scope.Scope]credential.IssuedToken{
				meshstack.Unscoped:                    fresh,
				meshstack.WorkspaceScope("gone"):      stale,
				meshstack.WorkspaceScope("also-gone"): stale,
			},
		})
		return c, nil
	})
	require.NoError(t, err)
	require.Equal(t, map[scope.Scope]credential.IssuedToken{meshstack.Unscoped: fresh}, got.Login.AccessTokens)

	raw, err := os.ReadFile(filepath.Join(dir, "meshstack", "credentials", "default.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "stale")

	// The last token expiring leaves the key out of the file rather than an empty map.
	got, err = store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Login.AccessTokens[meshstack.Unscoped] = stale
		return c, nil
	})
	require.NoError(t, err)
	require.Nil(t, got.Login.AccessTokens)
	raw, err = os.ReadFile(filepath.Join(dir, "meshstack", "credentials", "default.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "accessTokens")
}

// TestUpdateKeepsATokenWithNoExpiry pins the difference between "expired at the zero time" and
// "said nothing about its own life". `meshstack auth login --api-token` stores a token that is
// not a JWT with an unknown expiry rather than a guessed one, and pruning it here would make the
// very next command report it as expired.
func TestUpdateKeepsATokenWithNoExpiry(t *testing.T) {
	store := newTestStore(t, "default")

	got, err := store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Credential = credential.FromManual(credential.Manual{
			AccessToken: credential.IssuedToken{Token: fakeJwt("no-expiry")},
		})
		return c, nil
	})
	require.NoError(t, err)
	require.Equal(t, fakeJwt("no-expiry"), got.Manual.AccessToken.Token)

	reread, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, fakeJwt("no-expiry"), reread.Manual.AccessToken.Token)
}

func TestUpdateWritesEvenWhenMintChangesNothing(t *testing.T) {
	store := newTestStore(t, "default")

	_, err := store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Endpoint = mustUrl("https://api.dev.meshcloud.io")
		c.Credential = credential.FromLogin(credential.Login{
			AccessTokens: map[scope.Scope]credential.IssuedToken{
				meshstack.Unscoped: {Token: fakeJwt("expired"), ExpiresAt: time.Now().Add(-time.Minute)},
			},
		})
		return c, nil
	})
	require.NoError(t, err)

	// A mint that hands the credentials straight back still has to reach the file, which
	// is what prunes the expired token.
	got, err := store.Update(t.Context(), unchanged)
	require.NoError(t, err)
	require.Nil(t, got.Login.AccessTokens)

	reread, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, "https://api.dev.meshcloud.io", reread.Endpoint.String())
	require.Nil(t, reread.Login.AccessTokens)
}

func TestUpdateSeesAnotherProcessWrite(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)
	path := filepath.Join(dir, "meshstack", "credentials", "default.json")

	// Hold the lock the way another process would, so Update has to wait for it.
	release, err := acquireLock(t.Context(), path+lockSuffix)
	require.NoError(t, err)

	var seen Credentials
	done := make(chan error, 1)
	go func() {
		_, err := store.Update(t.Context(), func(c Credentials) (Credentials, error) {
			seen = c
			return c, nil
		})
		done <- err
	}()

	// The winner writes while the loser waits, so the loser must re-read under the lock
	// instead of minting from what it saw before.
	require.NoError(t, os.WriteFile(path, []byte(`{"version": 1, "endpoint": "https://written-by-the-winner", "current": "apiKey", "apiKey": {"clientId": "winner"}}`), fileMode))
	release()

	require.NoError(t, <-done)
	require.Equal(t, "https://written-by-the-winner", seen.Endpoint.String())
	require.Equal(t, credential.MethodApiKey, seen.Current)
	require.Equal(t, "winner", seen.ApiKey.Id)
}

func TestConcurrentUpdatesSerialise(t *testing.T) {
	store := newTestStore(t, "default")

	const writers = 8
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Update(t.Context(), func(c Credentials) (Credentials, error) {
				// A read-modify-write that loses an update shows up as a missing count. The
				// key id is the counter because it is the one plain string in the file.
				n := 0
				if c.ApiKey != nil {
					n, _ = strconv.Atoi(c.ApiKey.Id)
				}
				time.Sleep(time.Millisecond) // widen the window a lost update would need
				c.Credential = credential.FromApiKey(credential.ApiKey{Id: strconv.Itoa(n + 1)})
				return c, nil
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(writers), got.ApiKey.Id)
}

func TestUpdateHonoursContext(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	release, err := acquireLock(t.Context(), filepath.Join(dir, "meshstack", "credentials", "default.json")+lockSuffix)
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = store.Update(ctx, unchanged)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default.json.lock")
}

func TestLockIsReleasedAndBrokenWhenStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.json.lock")

	release, err := acquireLock(t.Context(), path)
	require.NoError(t, err)
	require.FileExists(t, path)
	release()
	require.NoFileExists(t, path)

	// A lock left behind by a process that died is broken rather than waited on forever.
	require.NoError(t, os.WriteFile(path, []byte("pid 1\n"), fileMode))
	old := time.Now().Add(-2 * lockStaleAfter)
	require.NoError(t, os.Chtimes(path, old, old))

	release, err = acquireLock(t.Context(), path)
	require.NoError(t, err)
	release()
}

func TestForget(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	// Forgetting a profile that was never logged in is not an error.
	require.NoError(t, store.Forget())

	_, err = store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Endpoint = mustUrl("https://api.dev.meshcloud.io")
		return c, nil
	})
	require.NoError(t, err)
	path := filepath.Join(dir, "meshstack", "credentials", "default.json")
	require.FileExists(t, path)

	require.NoError(t, store.Forget())
	require.NoFileExists(t, path)

	creds, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, Credentials{Version: Version}, creds)
}

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore(Credentials{
		Endpoint:   mustUrl("https://api.dev.meshcloud.io"),
		Credential: credential.FromManual(credential.Manual{}),
	})

	require.False(t, store.Writable())
	assert.Contains(t, store.Describe(), "memory")
	// A memory store names no path, so an error about it cannot point at a file.
	assert.NotContains(t, store.Describe(), string(os.PathSeparator))

	creds, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, Version, creds.Version)
	require.Equal(t, credential.MethodManual, creds.Current)

	got, err := store.Update(t.Context(), func(c Credentials) (Credentials, error) {
		c.Credential = credential.FromLogin(credential.Login{
			AccessTokens: map[scope.Scope]credential.IssuedToken{
				meshstack.Unscoped:               {Token: fakeJwt("fresh"), ExpiresAt: time.Now().Add(time.Minute)},
				meshstack.WorkspaceScope("gone"): {Token: fakeJwt("stale"), ExpiresAt: time.Now().Add(-time.Minute)},
			},
		})
		return c, nil
	})
	require.NoError(t, err)
	require.Len(t, got.Login.AccessTokens, 1)

	// What a caller mutates after reading must not reach into the store.
	read, err := store.Read()
	require.NoError(t, err)
	delete(read.Login.AccessTokens, meshstack.Unscoped)
	again, err := store.Read()
	require.NoError(t, err)
	require.Len(t, again.Login.AccessTokens, 1)

	require.NoError(t, store.Forget())
	creds, err = store.Read()
	require.NoError(t, err)
	require.Equal(t, Credentials{Version: Version}, creds)
}

func TestMemoryStorePropagatesMintError(t *testing.T) {
	store := NewMemoryStore(Credentials{})
	_, err := store.Update(t.Context(), func(Credentials) (Credentials, error) {
		return Credentials{}, assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)
}

func TestUpdatePropagatesMintErrorAndWritesNothing(t *testing.T) {
	dir := isolate(t)
	store, err := NewFileStore("default")
	require.NoError(t, err)

	_, err = store.Update(t.Context(), func(Credentials) (Credentials, error) {
		return Credentials{}, assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)
	require.NoFileExists(t, filepath.Join(dir, "meshstack", "credentials", "default.json"))

	// The lock is released even when mint fails, so the next attempt is not blocked.
	entries, err := os.ReadDir(filepath.Join(dir, "meshstack", "credentials"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), lockSuffix), "lock %s was left behind", e.Name())
	}
}

// mustUrl parses a URL a test wrote, and panics on one it wrote wrong, which is a test bug
// rather than a case worth handling.
func mustUrl(raw string) *xurl.URL {
	parsed := &xurl.URL{}
	if err := parsed.UnmarshalText([]byte(raw)); err != nil {
		panic(err)
	}
	return parsed
}

// fakeJwt is an access token identified by a name a test can assert on. Every access token is
// a JWT, so a test cannot use a bare string for one. Nothing verifies a signature, so only the
// payload is real.
func fakeJwt(id string) jwt.JWT {
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"jti":%q}`, id))
	parsed, err := jwt.Parse("bogus." + payload + ".bogus")
	if err != nil {
		panic(err)
	}
	return parsed
}
