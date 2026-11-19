package browser

import (
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func newLoginPages(previous meshstack.AccessLevel) *loginPages {
	client := oidc.Client{Id: "meshstack-cli", AuthorizationEndpoint: xurl.MustParsef("https://keycloak.example/auth")}
	return &loginPages{
		flow:     client.NewAuthorizationCode(xurl.MustParsef("http://127.0.0.1:1234/callback")),
		endpoint: xurl.MustParsef("https://meshstack.example"),
		previous: Previous{Level: previous},
		nonce:    "the-nonce",
		arrived:  make(chan callback, 1),
	}
}

func submitAccessLevel(t *testing.T, pages *loginPages, nonce, level string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"nonce": {nonce}, "access": {level}}
	req := httptest.NewRequestWithContext(t.Context(), gohttp.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	pages.handler().ServeHTTP(recorder, req)
	return recorder
}

func TestTheAccessLevelPagePreselectsThePreviousLevel(t *testing.T) {
	recorder := httptest.NewRecorder()
	newLoginPages(meshstack.AccessWrite).handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), gohttp.MethodGet, "/", nil))

	require.Equal(t, gohttp.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `value="write" required checked`)
	assert.NotContains(t, recorder.Body.String(), `value="read" required checked`)
}

func TestTheAccessLevelPageExplainsSensitiveOperationsOnTheLevelsThatNameThem(t *testing.T) {
	recorder := httptest.NewRecorder()
	newLoginPages("").handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), gohttp.MethodGet, "/", nil))

	require.Equal(t, gohttp.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Equal(t, 2, strings.Count(body, `data-help="Sensitive operations are deleting meshObjects and approving or rejecting building block runs."`))
	assert.Equal(t, 1, strings.Count(body, "Recommended for work with agents"))
}

func TestAChosenAccessLevelRedirectsToTheIdentityProvider(t *testing.T) {
	for _, test := range []struct {
		name       string
		previous   meshstack.AccessLevel
		chosen     meshstack.AccessLevel
		askConsent bool
	}{
		{name: "a first login leaves the consent to keycloak", previous: "", chosen: meshstack.AccessRead},
		{name: "the same level reuses the consent", previous: meshstack.AccessRead, chosen: meshstack.AccessRead},
		{name: "a changed level asks for consent", previous: meshstack.AccessFull, chosen: meshstack.AccessRead, askConsent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pages := newLoginPages(test.previous)
			recorder := submitAccessLevel(t, pages, "the-nonce", string(test.chosen))

			require.Equal(t, gohttp.StatusSeeOther, recorder.Code)
			location, err := recorder.Result().Location()
			require.NoError(t, err)
			assert.Equal(t, "keycloak.example", location.Host)
			assert.Contains(t, strings.Fields(location.Query().Get("scope")), string(test.chosen.Scope()))
			assert.Equal(t, test.askConsent, location.Query().Get("prompt") == "consent")
			assert.Equal(t, test.chosen, pages.chosenLevel())
		})
	}
}

func TestAnAccessLevelFromAnotherPageIsRefused(t *testing.T) {
	pages := newLoginPages("")

	assert.Equal(t, gohttp.StatusForbidden, submitAccessLevel(t, pages, "another-nonce", "full").Code)
	assert.Equal(t, gohttp.StatusBadRequest, submitAccessLevel(t, pages, "the-nonce", "admin").Code)
	assert.Empty(t, pages.chosenLevel())
}

func TestALoginThatKeycloakGrantsAnotherAccessLevelIsEnded(t *testing.T) {
	var revoked []string
	revokeFails := false
	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		revoked = append(revoked, r.PostFormValue("token"))
		if revokeFails {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(gohttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"temporarily_unavailable","error_description":"try later"}`))
		}
	}))
	defer server.Close()

	pages := newLoginPages("")
	pages.flow.Client.Client = http.NewClient(fakemeshstack.UserAgent)
	pages.flow.Issuer = xurl.MustParsef("https://keycloak.example/realms/meshfed")
	pages.flow.RevocationEndpoint = new(xurl.MustParsef("%s/revoke", server.URL))
	pages.chosen = meshstack.AccessFull
	tokenWith := func(scopes string) oidc.Token {
		return oidc.Token{RefreshToken: "offline-" + scopes, Scope: "openid offline_access " + scopes}
	}

	t.Run("the chosen level is stored", func(t *testing.T) {
		level, err := pages.grantedLevel(t.Context(), tokenWith("cli-access-full"))

		require.NoError(t, err)
		assert.Equal(t, meshstack.AccessFull, level)
		assert.Empty(t, revoked)
	})

	t.Run("a meshStack without access levels stores none for full access", func(t *testing.T) {
		level, err := pages.grantedLevel(t.Context(), tokenWith("email"))

		require.NoError(t, err)
		assert.Empty(t, level)
		assert.Empty(t, revoked)
	})

	t.Run("full access granted as safe access ends the login", func(t *testing.T) {
		level, err := pages.grantedLevel(t.Context(), tokenWith("cli-access-write"))

		require.ErrorContains(t, err, `granted the access level "Safe access" instead of "Full access"`)
		require.ErrorContains(t, err, `log in again and choose "Full access"`)
		assert.Empty(t, level)
		assert.Equal(t, []string{"offline-cli-access-write"}, revoked)
	})

	t.Run("a login that cannot be ended names the two logins that fix it", func(t *testing.T) {
		revokeFails = true
		pages.chosen = meshstack.AccessRead

		_, err := pages.grantedLevel(t.Context(), tokenWith("cli-access-write"))

		require.ErrorContains(t, err, "try later")
		require.ErrorContains(t, err, `log in once with "Safe access", then again with "Read-only"`)
	})
}
