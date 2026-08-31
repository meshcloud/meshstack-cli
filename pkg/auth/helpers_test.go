package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

// testSecret has the shape credential.CheckSecret expects of an API key secret: 32
// alphanumerics, no whitespace, not a UUID.
const testSecret = "abcdef0123456789abcdef0123456789"

// paths names the two files a test is allowed to touch.
type paths struct {
	config      string
	credentials string
}

// isolate points the configuration at a temporary directory and empties every MESHSTACK_*
// variable this package reads, so that no test can see or write the developer's own
// configuration — not even one that is expected never to write.
func isolate(t *testing.T) paths {
	t.Helper()
	dir := t.TempDir()
	found := paths{config: filepath.Join(dir, "config.json"), credentials: filepath.Join(dir, "credentials")}
	t.Setenv("MESHSTACK_CONFIG_FILE", found.config)
	t.Setenv("MESHSTACK_CREDENTIALS_DIR", found.credentials)
	for _, key := range []string{envEndpoint, envApiKey, envApiSecret, envApiToken, envProfile, "MESHSTACK_WORKSPACE"} {
		t.Setenv(key, "")
	}
	// A test process is not a person: nothing prompts, and an unwritable store degrades to
	// memory rather than stopping.
	t.Setenv("MESHSTACK_NO_INPUT", "1")
	return found
}

// requireNothingStored is the CI-and-building-block rule: a credential supplied whole from
// the environment leaves no trace in the configuration directory.
func requireNothingStored(t *testing.T, at paths) {
	t.Helper()
	require.NoFileExists(t, at.config)
	entries, err := os.ReadDir(at.credentials)
	if err != nil {
		require.ErrorIs(t, err, fs.ErrNotExist)
		return
	}
	require.Empty(t, entries, "the credentials directory should still be empty")
}

func writeConfig(t *testing.T, current string, profiles map[string]profile.Profile) {
	t.Helper()
	require.NoError(t, profile.SaveConfig(profile.Config{
		Version:        profile.Version,
		CurrentProfile: current,
		Profiles:       profiles,
	}))
}

// writeCredentials and readCredentials work on the profile these tests select, which is the
// one every precedence rule lands in when nothing names another.
const testProfile = DefaultProfile

func writeCredentials(t *testing.T, credentials profile.Credentials) {
	t.Helper()
	store, err := profile.NewFileStore(testProfile)
	require.NoError(t, err)
	_, err = store.Update(t.Context(), func(profile.Credentials) (profile.Credentials, error) {
		return credentials, nil
	})
	require.NoError(t, err)
}

// futureExpiry and insideGrace sit either side of the 30-second grace window. The numbers
// are literal rather than derived from graceWindow, so that widening the window is a change
// these tests notice.
func futureExpiry() time.Time { return time.Now().Add(5 * time.Minute) }

func insideGrace() time.Time { return time.Now().Add(10 * time.Second) }

func readCredentials(t *testing.T) profile.Credentials {
	t.Helper()
	store, err := profile.NewFileStore(testProfile)
	require.NoError(t, err)
	credentials, err := store.Read()
	require.NoError(t, err)
	return credentials
}

// fakeInput is what a front end would supply: fixed Values, canned secrets, and no browser
// unless a test hands one over.
type fakeInput struct {
	values  Values
	secret  string
	token   string
	browser Browser
}

func (f *fakeInput) Explicit() Values { return f.values }

func (f *fakeInput) ApiKeySecret(context.Context) (string, error) { return f.secret, nil }

func (f *fakeInput) ApiToken(context.Context) (string, error) { return f.token, nil }

func (f *fakeInput) Browser() Browser { return f.browser }

func resolved(t *testing.T, in Input) *Session {
	t.Helper()
	session, err := Resolve(t.Context(), in)
	require.NoError(t, err)
	return session
}

// problemOf is what a front end renders, so an assertion about a message asserts on the
// summary and the detail rather than on a formatted string.
func problemOf(t *testing.T, err error) diags.Problem {
	t.Helper()
	require.Error(t, err)
	p, ok := errors.AsType[diags.Problem](err)
	require.True(t, ok, "expected a diags.Problem, got %T: %v", err, err)
	return p
}

// tokenId reads back the name fakeToken or the fake /api/login gave a token, so an assertion
// can name it without depending on the exp claim minted beside it.
func tokenId(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return raw
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Jti string `json:"jti"`
	}
	_ = json.Unmarshal(payload, &claims)
	return claims.Jti
}

// fakeToken is an access token identified by a name a test can assert on. Every access token
// is a JWT, including a pasted one, so a test cannot use a bare string for one.
func fakeToken(id string) string {
	return fakeJwt(map[string]any{"jti": id})
}

// jwt builds a token whose payload holds the given claims. Nothing verifies a signature, so
// "x.<payload>.y" is a token as far as anything here is concerned.
func fakeJwt(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	return "x." + base64.RawURLEncoding.EncodeToString(payload) + ".y"
}

// fakeMeshStack stands in for a meshStack instance and for the keycloak behind it: the two
// public discovery documents, the API key exchange, and the token endpoint. The issuer
// points back at the same server, so discovery follows it.
type fakeMeshStack struct {
	URL *url.URL

	mu              sync.Mutex
	apiLogins       int
	refreshes       int
	refreshForms    []url.Values
	apiLoginExpires int
	apiLoginStatus  int
	refresh         func(form url.Values) (int, map[string]any)
	observe         func(*http.Request)
}

func newMeshStack(t *testing.T) *fakeMeshStack {
	t.Helper()

	stack := &fakeMeshStack{apiLoginExpires: 300, refresh: refreshFromScope}
	mux := http.NewServeMux()
	server := httptest.NewServer(stack.observing(mux))
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	stack.URL = parsed

	writeJSON := func(w http.ResponseWriter, status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	mux.HandleFunc("/mesh/info", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			// Above client.MinMeshStackVersion, so a test that does build a client only has to
			// set MESHSTACK_SKIP_VERSION_CHECK when it wants to skip the request itself.
			"version":     "2026.34.0",
			"issuer":      server.URL,
			"cliClientId": "meshstack-cli",
		})
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/auth",
			"token_endpoint":         server.URL + "/token",
			"end_session_endpoint":   server.URL + "/logout",
		})
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		stack.mu.Lock()
		stack.apiLogins++
		count, expires, status := stack.apiLogins, stack.apiLoginExpires, stack.apiLoginStatus
		stack.mu.Unlock()
		if status != 0 {
			writeJSON(w, status, map[string]any{"message": "no"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			// The deadline is in the token, in its exp claim, which is where pkg/auth reads
			// it from. expires_in is sent as well because meshStack sends it.
			"access_token": fakeJwt(map[string]any{
				"jti": fmt.Sprintf("api-key-token-%d", count),
				"exp": time.Now().Add(time.Duration(expires) * time.Second).Unix(),
			}),
			"expires_in": expires,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		stack.mu.Lock()
		stack.refreshes++
		stack.refreshForms = append(stack.refreshForms, r.PostForm)
		answer := stack.refresh
		count := stack.refreshes
		stack.mu.Unlock()

		status, body := answer(r.PostForm)
		if rotated, ok := body["refresh_token"]; ok && rotated == "" {
			body["refresh_token"] = fmt.Sprintf("refresh-%d", count)
		}
		writeJSON(w, status, body)
	})
	return stack
}

// refreshFromScope is the identity provider behaving: it hands back a token carrying the
// workspace that was asked for, and rotates the refresh token as keycloak does.
func refreshFromScope(form url.Values) (int, map[string]any) {
	name := ""
	for _, scope := range strings.Fields(form.Get("scope")) {
		if after, ok := strings.CutPrefix(scope, "c:"); ok {
			name = after
		}
	}
	return http.StatusOK, map[string]any{
		"access_token": fakeJwt(map[string]any{
			"MC_CUSTOMER":        name,
			"preferred_username": "someone@example.com",
			"exp":                float64(time.Now().Add(5 * time.Minute).Unix()),
		}),
		"refresh_token": "", // filled in with the rotation counter.
		"expires_in":    300,
	}
}

func (m *fakeMeshStack) apiLoginCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apiLogins
}

func (m *fakeMeshStack) refreshCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshes
}

func (m *fakeMeshStack) lastRefreshForm(t *testing.T) url.Values {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.NotEmpty(t, m.refreshForms, "nothing was posted to the token endpoint")
	return m.refreshForms[len(m.refreshForms)-1]
}

// setApiLoginExpiry makes /api/login answer with a token that is already inside the grace
// window, which is how a method that cannot produce a usable token is exercised.
func (m *fakeMeshStack) setApiLoginExpiry(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiLoginExpires = seconds
}

// answerApiLoginWith makes /api/login answer with the given status instead of a token.
func (m *fakeMeshStack) answerApiLoginWith(status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiLoginStatus = status
}

// onEachRequest lets a test look at the world at the moment a request arrives, which is how a
// rule about what is held while one is in flight is asserted.
func (m *fakeMeshStack) onEachRequest(observe func(*http.Request)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observe = observe
}

func (m *fakeMeshStack) observing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		observe := m.observe
		m.mu.Unlock()
		if observe != nil {
			observe(r)
		}
		next.ServeHTTP(w, r)
	})
}

func (m *fakeMeshStack) answerRefreshWith(answer func(url.Values) (int, map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh = answer
}

// logRecorder captures what pkg/auth logs, because HintErr's whole output is a log record.
type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func captureLogs(t *testing.T) *logRecorder {
	t.Helper()
	recorder := &logRecorder{}
	previous := slog.Default()
	slog.SetDefault(slog.New(recorder))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return recorder
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record.Clone())
	return nil
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *logRecorder) WithGroup(string) slog.Handler { return r }

// warnings returns the messages logged at warning level or above.
func (r *logRecorder) warnings() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var messages []string
	for _, record := range r.records {
		if record.Level >= slog.LevelWarn {
			messages = append(messages, record.Message)
		}
	}
	return messages
}

// mustUrl and mustJwt build the two parsing types the stored files hold. Both panic on input
// a test wrote wrong, which is a test bug rather than a case worth handling.
func mustUrl(raw string) *xurl.URL {
	parsed := &xurl.URL{}
	if err := parsed.UnmarshalText([]byte(raw)); err != nil {
		panic(err)
	}
	return parsed
}

func mustJwt(raw string) jwt.JWT {
	parsed, err := jwt.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}
