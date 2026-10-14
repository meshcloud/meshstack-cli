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
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
)

func newLoginPages(previous meshstack.AccessLevel) *loginPages {
	client := oidc.Client{Id: "meshstack-cli", AuthorizationEndpoint: xurl.MustParsef("https://keycloak.example/auth")}
	return &loginPages{
		flow:     client.NewAuthorizationCode(xurl.MustParsef("http://127.0.0.1:1234/callback")),
		previous: previous,
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
