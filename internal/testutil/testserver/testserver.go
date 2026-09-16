// Package testserver stands in for meshStack's login endpoint and for one endpoint behind it,
// so that a test can drive the token cache without a backend. Every exported function takes a
// *testing.T, which is what says that nothing here belongs in a shipped binary.
//
// It reaches internal/auth for the api key settings, and so for everything internal/auth
// imports. A package whose own tests are in the internal test package — internal/profile is
// one — therefore cannot use this one without turning that import into a cycle, and has to
// move its tests to an external test package first.
package testserver

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth"
)

const greeting = "hello"

// ApiKey is the client id and secret pair that a Server honors at its login endpoint.
type ApiKey struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// SetEnv points the api key settings at this key for the rest of the test, which is how a test
// makes the code under test resolve this credential.
func (k ApiKey) SetEnv(t *testing.T) {
	t.Helper()
	t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), k.ClientId)
	t.Setenv(auth.ApiKeyClientSecretSetting.EnvKey(), k.ClientSecret)
}

// GreetingClient is all this package needs of the code under test: a GET that parses a JSON
// string, carrying whatever authorization the caller wired up.
type GreetingClient func(ctx context.Context, url *url.URL) (string, error)

// Counts is what a Server saw. It comes back as one snapshot, so that assertions on several
// of these numbers describe the same moment.
type Counts struct {
	Logins        int64
	Greetings     int64
	RevokedTokens int64
	// UnknownTokens counts the 401s no test asked for: a token this Server never minted, or
	// no token at all. It is the one count that stays at zero.
	UnknownTokens int64
}

type Server struct {
	url     *url.URL
	honored []ApiKey

	logins        atomic.Int64
	greetings     atomic.Int64
	revokedTokens atomic.Int64
	unknownTokens atomic.Int64

	mu     sync.Mutex
	minted []mintedToken
}

type mintedToken struct {
	value   string
	at      time.Time
	honored bool
}

// New starts a server that honors the given api keys, and stops it when the test ends.
func New(t *testing.T, honored ...ApiKey) *Server {
	t.Helper()
	server := &Server{honored: honored}
	httpServer := httptest.NewServer(gohttp.HandlerFunc(server.handle))
	t.Cleanup(httpServer.Close)
	serverUrl, err := url.Parse(httpServer.URL)
	require.NoError(t, err)
	server.url = serverUrl
	return server
}

func (s *Server) Url(t *testing.T) *url.URL {
	t.Helper()
	return s.url
}

func (s *Server) GreetingUrl(t *testing.T) *url.URL {
	t.Helper()
	return s.url.JoinPath("greeting")
}

func (s *Server) Counts(t *testing.T) Counts {
	t.Helper()
	return Counts{
		Logins:        s.logins.Load(),
		Greetings:     s.greetings.Load(),
		RevokedTokens: s.revokedTokens.Load(),
		UnknownTokens: s.unknownTokens.Load(),
	}
}

// MintToken issues a token this Server honors, exactly as its login endpoint does, for a test
// that hands one to the code under test directly.
func (s *Server) MintToken(t *testing.T, validFor time.Duration) string {
	t.Helper()
	return s.mint(validFor)
}

// RevokeNewestToken stops honoring the newest token still honored, and reports whether there
// was one. The newest is the one a cache is most likely to be holding, so it is the one that
// makes a caller refresh.
//
// A concurrent caller must not have a request in flight while this runs. An authorized client
// retries a 401 exactly once, and the token it retries with can be one it adopted from another
// process's cache — so revoking during a request can leave a 401 that nothing recovers from,
// which is a limit of the one retry rather than anything a test can observe.
func (s *Server) RevokeNewestToken(t *testing.T) (revoked bool) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.minted) - 1; i >= 0; i-- {
		if s.minted[i].honored {
			s.minted[i].honored = false
			return true
		}
	}
	return false
}

// Greeting calls the authorized endpoint and returns what went wrong, for a caller on its own
// goroutine, where require would be undefined behaviour.
func (s *Server) Greeting(t *testing.T, ctx context.Context, greet GreetingClient) error {
	t.Helper()
	answered, err := greet(ctx, s.GreetingUrl(t))
	if err != nil {
		return err
	}
	if answered != greeting {
		return fmt.Errorf("the endpoint answered %q rather than %q", answered, greeting)
	}
	return nil
}

// RequireGreeting fails the test unless the greeting comes back.
func (s *Server) RequireGreeting(t *testing.T, greet GreetingClient) {
	t.Helper()
	require.NoError(t, s.Greeting(t, t.Context(), greet))
}

func (s *Server) handle(resp gohttp.ResponseWriter, req *gohttp.Request) {
	switch req.URL.Path {
	case "/api/login":
		s.handleLogin(resp, req)
	case "/greeting":
		s.handleGreeting(resp, req)
	default:
		resp.WriteHeader(gohttp.StatusNotFound)
	}
}

func (s *Server) handleLogin(resp gohttp.ResponseWriter, req *gohttp.Request) {
	body, err := io.ReadAll(req.Body)
	var offered ApiKey
	if err != nil || json.Unmarshal(body, &offered) != nil {
		resp.WriteHeader(gohttp.StatusBadRequest)
		return
	}
	if !slices.Contains(s.honored, offered) {
		resp.WriteHeader(gohttp.StatusUnauthorized)
		return
	}
	s.logins.Add(1)
	resp.WriteHeader(gohttp.StatusOK)
	_, _ = fmt.Fprintf(resp, `{"access_token":%q}`, s.mint(time.Hour))
}

func (s *Server) handleGreeting(resp gohttp.ResponseWriter, req *gohttp.Request) {
	offered, _ := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")

	s.mu.Lock()
	minted := slices.IndexFunc(s.minted, func(candidate mintedToken) bool {
		return candidate.value == offered
	})
	honored := minted >= 0 && s.minted[minted].honored
	s.mu.Unlock()

	switch {
	case honored:
		s.greetings.Add(1)
		resp.WriteHeader(gohttp.StatusOK)
		_, _ = fmt.Fprintf(resp, "%q", greeting)
	case minted >= 0:
		s.revokedTokens.Add(1)
		resp.WriteHeader(gohttp.StatusUnauthorized)
	default:
		s.unknownTokens.Add(1)
		resp.WriteHeader(gohttp.StatusUnauthorized)
	}
}

func (s *Server) mint(validFor time.Duration) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The jti counter is what tells two tokens minted in the same second apart.
	claims := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d,"jti":"%d"}`,
		time.Now().Add(validFor).Unix(), len(s.minted)+1))
	minted := mintedToken{value: "e30." + claims + ".test-signature", at: time.Now(), honored: true}
	s.minted = append(s.minted, minted)
	return minted.value
}
