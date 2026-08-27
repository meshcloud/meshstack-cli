package oidc_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// jwt builds a token whose payload holds the given claims. Only the payload is real: nothing
// in this package verifies a signature, so nothing has to produce one.
func jwt(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// stack serves the two public documents Discover reads, with the issuer pointing back at the
// test server so that discovery follows it.
func stack(t *testing.T, info, config map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	write := func(w http.ResponseWriter, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}
	mux.HandleFunc("/mesh/info", func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := info["issuer"]; !ok {
			// Absent means "this meshStack is too old", present but empty is a different test.
			info["issuer"] = server.URL
		}
		write(w, info)
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		write(w, config)
	})
	return server
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	fullConfig := func(issuer string) map[string]any {
		return map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"end_session_endpoint":   issuer + "/logout",
			"revocation_endpoint":    issuer + "/revoke",
		}
	}

	tests := []struct {
		name        string
		info        map[string]any
		config      func(issuer string) map[string]any
		wantErr     string
		wantClient  string
		wantAuthURL string
	}{
		{
			name:        "a complete instance",
			info:        map[string]any{"version": "2026.8.1", "cliClientId": "meshstack-cli"},
			config:      fullConfig,
			wantClient:  "meshstack-cli",
			wantAuthURL: "/auth",
		},
		{
			name:    "a meshStack that does not know about the CLI client",
			info:    map[string]any{"version": "2025.1.1", "issuer": ""},
			config:  fullConfig,
			wantErr: "does not support a browser login",
		},
		{
			name:    "an issuer with no token endpoint",
			info:    map[string]any{"version": "2026.8.1", "cliClientId": "meshstack-cli"},
			config:  func(issuer string) map[string]any { return map[string]any{"issuer": issuer} },
			wantErr: "not a usable OpenID provider",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := map[string]any{}
			server := stack(t, test.info, config)
			for key, value := range test.config(server.URL) {
				config[key] = value
			}

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)

			cfg, err := oidc.Discover(t.Context(), endpoint)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantClient, cfg.ClientId)
			assert.Equal(t, server.URL, cfg.Issuer)
			assert.Equal(t, server.URL+test.wantAuthURL, cfg.AuthorizationEndpoint)
			assert.Equal(t, server.URL+"/token", cfg.TokenEndpoint)
			assert.Equal(t, server.URL+"/logout", cfg.EndSessionEndpoint)
			assert.Equal(t, server.URL+"/revoke", cfg.RevocationEndpoint)
			assert.Equal(t, endpoint, cfg.Endpoint)
		})
	}
}

func TestDiscoverWithoutAnEndpoint(t *testing.T) {
	t.Parallel()

	_, err := oidc.Discover(t.Context(), nil)
	require.ErrorContains(t, err, "without a meshStack endpoint")
}

func TestDiscoverAgainstAnUnreachableEndpoint(t *testing.T) {
	t.Parallel()

	server := stack(t, map[string]any{}, map[string]any{})
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	server.Close()

	_, err = oidc.Discover(t.Context(), endpoint)
	require.ErrorContains(t, err, "cannot read the meshStack instance information")
}

// tokenEndpoint serves one canned answer and records the form of the last request, which is
// what proves the scope parameter is always sent.
func tokenEndpoint(t *testing.T, status int, body string) (cfg oidc.ClientConfig, form *url.Values) {
	t.Helper()
	form = &url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		*form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, err := fmt.Fprint(w, body)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return oidc.ClientConfig{ClientId: "meshstack-cli", TokenEndpoint: server.URL}, form
}

func TestRefresh(t *testing.T) {
	t.Parallel()

	granted := `{"access_token":"new-access","refresh_token":"rotated","expires_in":300,` +
		`"refresh_expires_in":0,"scope":"openid c:demo-customer"}`

	tests := []struct {
		name        string
		ws          workspace.Name
		status      int
		body        string
		wantScope   string
		wantRefresh string
		wantAccess  string
		wantExpiry  time.Duration
		wantErrs    []error
		wantErrText string
	}{
		{
			name:        "a workspace-scoped grant rotates the refresh token",
			ws:          "demo-customer",
			status:      http.StatusOK,
			body:        granted,
			wantScope:   "openid c:demo-customer",
			wantRefresh: "rotated",
			wantAccess:  "new-access",
			wantExpiry:  5 * time.Minute,
		},
		{
			// A refresh that omits the scope comes back without MC_CUSTOMER, so the unscoped case
			// has to send scope=openid rather than nothing at all.
			name:        "an unscoped grant still sends a scope",
			ws:          "",
			status:      http.StatusOK,
			body:        `{"access_token":"a","refresh_token":"b","expires_in":300}`,
			wantScope:   "openid",
			wantRefresh: "b",
			wantAccess:  "a",
			wantExpiry:  5 * time.Minute,
		},
		{
			// RFC 6749 lets a provider omit the new token, meaning "keep the one you have".
			name:        "a grant that returns no new refresh token keeps the old one",
			ws:          "demo-customer",
			status:      http.StatusOK,
			body:        `{"access_token":"a","expires_in":300}`,
			wantScope:   "openid c:demo-customer",
			wantRefresh: "current",
			wantAccess:  "a",
			wantExpiry:  5 * time.Minute,
		},
		{
			name:        "a dead session",
			ws:          "demo-customer",
			status:      http.StatusBadRequest,
			body:        `{"error":"invalid_grant","error_description":"Session doesn't have required client"}`,
			wantScope:   "openid c:demo-customer",
			wantErrs:    []error{oidc.ErrRefreshRejected},
			wantErrText: "Session doesn't have required client",
		},
		{
			name:        "a reused refresh token",
			ws:          "demo-customer",
			status:      http.StatusBadRequest,
			body:        `{"error":"invalid_grant","error_description":"Maximum allowed refresh token reuse exceeded"}`,
			wantScope:   "openid c:demo-customer",
			wantErrs:    []error{oidc.ErrRefreshRejected, oidc.ErrRefreshTokenReused},
			wantErrText: "Maximum allowed refresh token reuse exceeded",
		},
		{
			// Anything but invalid_grant says the request was wrong, not the session, so
			// `meshstack login` would be the wrong advice and the sentinel stays off.
			name:        "a rejected client",
			ws:          "demo-customer",
			status:      http.StatusBadRequest,
			body:        `{"error":"invalid_client","error_description":"Invalid client credentials"}`,
			wantScope:   "openid c:demo-customer",
			wantErrText: "Invalid client credentials",
		},
		{
			name:        "an answer that is not JSON at all",
			ws:          "",
			status:      http.StatusBadGateway,
			body:        "<html>gateway timeout</html>",
			wantScope:   "openid",
			wantErrText: "cannot parse the answer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg, form := tokenEndpoint(t, test.status, test.body)
			refreshed, access, expiry, err := oidc.Refresh(t.Context(), cfg, "current", test.ws)

			assert.Equal(t, "refresh_token", form.Get("grant_type"))
			assert.Equal(t, "current", form.Get("refresh_token"))
			assert.Equal(t, "meshstack-cli", form.Get("client_id"))
			assert.Equal(t, test.wantScope, form.Get("scope"))
			// The client is public, so nothing may authenticate it.
			assert.Empty(t, form.Get("client_secret"))

			if test.wantErrText != "" || len(test.wantErrs) > 0 {
				require.Error(t, err)
				for _, want := range test.wantErrs {
					require.ErrorIs(t, err, want)
				}
				if !containsError(test.wantErrs, oidc.ErrRefreshTokenReused) {
					require.NotErrorIs(t, err, oidc.ErrRefreshTokenReused)
				}
				if !containsError(test.wantErrs, oidc.ErrRefreshRejected) {
					require.NotErrorIs(t, err, oidc.ErrRefreshRejected)
				}
				require.ErrorContains(t, err, test.wantErrText)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.wantRefresh, refreshed)
			assert.Equal(t, test.wantAccess, access)
			assert.Equal(t, test.wantExpiry, expiry)
		})
	}
}

func containsError(errs []error, want error) bool {
	for _, err := range errs {
		if err == want {
			return true
		}
	}
	return false
}

func TestRefreshWithoutAToken(t *testing.T) {
	t.Parallel()

	_, _, _, err := oidc.Refresh(t.Context(), oidc.ClientConfig{ClientId: "c", TokenEndpoint: "http://example.invalid"}, "", "")
	require.ErrorContains(t, err, "without a refresh token")
}

func TestExchange(t *testing.T) {
	t.Parallel()

	t.Run("a granted code yields both tokens", func(t *testing.T) {
		t.Parallel()

		cfg, form := tokenEndpoint(t, http.StatusOK, `{"access_token":"a","refresh_token":"r","expires_in":300}`)
		refresh, access, expiry, err := oidc.Exchange(t.Context(), cfg, "the-code", "http://127.0.0.1:1/callback", "the-verifier")
		require.NoError(t, err)
		assert.Equal(t, "r", refresh)
		assert.Equal(t, "a", access)
		assert.Equal(t, 5*time.Minute, expiry)
		assert.Equal(t, "authorization_code", form.Get("grant_type"))
		assert.Equal(t, "the-code", form.Get("code"))
		assert.Equal(t, "the-verifier", form.Get("code_verifier"))
		assert.Equal(t, "http://127.0.0.1:1/callback", form.Get("redirect_uri"))
	})

	t.Run("a login without offline_access is refused rather than stored", func(t *testing.T) {
		t.Parallel()

		cfg, _ := tokenEndpoint(t, http.StatusOK, `{"access_token":"a","expires_in":300,"scope":"openid profile email"}`)
		_, _, _, err := oidc.Exchange(t.Context(), cfg, "the-code", "http://127.0.0.1:1/callback", "the-verifier")
		require.ErrorContains(t, err, "offline_access")
	})

	t.Run("a stale code is not reported as a dead session", func(t *testing.T) {
		t.Parallel()

		cfg, _ := tokenEndpoint(t, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Code not valid"}`)
		_, _, _, err := oidc.Exchange(t.Context(), cfg, "the-code", "http://127.0.0.1:1/callback", "the-verifier")
		require.ErrorContains(t, err, "Code not valid")
		require.NotErrorIs(t, err, oidc.ErrRefreshRejected)
	})
}

func TestEndSession(t *testing.T) {
	t.Parallel()

	t.Run("the end session endpoint takes the refresh token", func(t *testing.T) {
		t.Parallel()

		cfg, form := tokenEndpoint(t, http.StatusNoContent, "")
		cfg.EndSessionEndpoint = cfg.TokenEndpoint
		require.NoError(t, oidc.EndSession(t.Context(), cfg, "current"))
		assert.Equal(t, "current", form.Get("refresh_token"))
		assert.Equal(t, "meshstack-cli", form.Get("client_id"))
	})

	t.Run("revocation is the fallback", func(t *testing.T) {
		t.Parallel()

		cfg, form := tokenEndpoint(t, http.StatusOK, "")
		cfg.RevocationEndpoint = cfg.TokenEndpoint
		require.NoError(t, oidc.EndSession(t.Context(), cfg, "current"))
		assert.Equal(t, "current", form.Get("token"))
		assert.Equal(t, "refresh_token", form.Get("token_type_hint"))
	})

	t.Run("a refusal is reported", func(t *testing.T) {
		t.Parallel()

		cfg, _ := tokenEndpoint(t, http.StatusBadRequest, `{"error":"invalid_token"}`)
		cfg.EndSessionEndpoint = cfg.TokenEndpoint
		require.ErrorContains(t, oidc.EndSession(t.Context(), cfg, "current"), "HTTP 400")
	})

	t.Run("a provider offering neither endpoint says so", func(t *testing.T) {
		t.Parallel()

		err := oidc.EndSession(t.Context(), oidc.ClientConfig{ClientId: "c"}, "current")
		require.ErrorContains(t, err, "end_session_endpoint")
	})
}

func TestClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		token   string
		want    map[string]any
		wantErr string
	}{
		{
			name:  "a token with claims",
			token: jwt(t, map[string]any{"MC_CUSTOMER": "demo-customer", "typ": "Bearer"}),
			want:  map[string]any{"MC_CUSTOMER": "demo-customer", "typ": "Bearer"},
		},
		{
			name:    "an opaque token",
			token:   "not-a-jwt",
			wantErr: "not a JWT",
		},
		{
			name:    "a payload that is not base64",
			token:   "header.!!!.signature",
			wantErr: "cannot decode the token payload",
		},
		{
			name:    "a payload that is not JSON",
			token:   "header." + base64.RawURLEncoding.EncodeToString([]byte("nonsense")) + ".signature",
			wantErr: "cannot parse the token payload",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			claims, err := oidc.Claims(test.token)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, claims)
		})
	}
}

func TestClaimWorkspace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		want  workspace.Name
	}{
		{
			name:  "a scoped token carries the workspace",
			token: jwt(t, map[string]any{"MC_CUSTOMER": "demo-customer"}),
			want:  "demo-customer",
		},
		{
			// The foreign-workspace case: HTTP 200, a token, and no claim. Turning that into an
			// empty name is what lets the caller explain it before the first 403.
			name:  "a token for a workspace the user is not in carries none",
			token: jwt(t, map[string]any{"MC_GROUPS": []any{}}),
			want:  "",
		},
		{
			name:  "a claim of the wrong type is no workspace",
			token: jwt(t, map[string]any{"MC_CUSTOMER": []any{"demo-customer"}}),
			want:  "",
		},
		{
			name:  "an undecodable token is no workspace",
			token: "not-a-jwt",
			want:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, oidc.ClaimWorkspace(test.token))
		})
	}
}

func TestExpiry(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(5 * time.Minute).Truncate(time.Second)

	tests := []struct {
		name    string
		token   string
		want    time.Time
		wantOk  bool
		comment string
	}{
		{
			name:   "an access token expires",
			token:  jwt(t, map[string]any{"exp": deadline.Unix()}),
			want:   deadline,
			wantOk: true,
		},
		{
			// A refresh token is typ Offline and carries no exp, which is why nothing derives a
			// refresh deadline from a token.
			name:   "a refresh token carries no exp",
			token:  jwt(t, map[string]any{"typ": "Offline"}),
			wantOk: false,
		},
		{
			name:   "an exp that is not a number",
			token:  jwt(t, map[string]any{"exp": "soon"}),
			wantOk: false,
		},
		{
			name:   "an opaque token",
			token:  "not-a-jwt",
			wantOk: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := oidc.Expiry(test.token)
			assert.Equal(t, test.wantOk, ok)
			if test.wantOk {
				assert.True(t, test.want.Equal(got), "expected %s, got %s", test.want, got)
			}
		})
	}
}
