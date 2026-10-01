package testacc

import (
	"encoding/json/v2"
	"html"
	"io"
	"maps"
	gohttp "net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// envTestUsers carries auth.openid.users out of ../meshfed-release, whose logins its
// DevLocalUserBootstrapService reconciles into keycloak on every startup.
const envTestUsers = "MESHSTACK_CLI_TEST_USERS"

type devLogin struct {
	Username   string            `json:"username"`
	Password   string            `json:"password"`
	Workspaces map[string]string `json:"workspaces"`
}

func devLogins(t *testing.T) []devLogin {
	t.Helper()
	var logins []devLogin
	require.NoErrorf(t, json.Unmarshal([]byte(requireEnv(t, envTestUsers)), &logins), "%s does not decode", envTestUsers)
	require.NotEmptyf(t, logins, "%s carries no login, so there is nobody to log in as", envTestUsers)
	return logins
}

// keycloak lets a login with no workspace in, and the workspace list then answers 403.
const refusedWithoutAWorkspace = "cannot list workspaces"

// TestAccOidcLogin drives the authorization code flow with no browser and no terminal, which is the
// shape CI has: the CLI prints the URL of its access level page to stderr and waits on a loopback
// listener, so anything that can read stderr and speak HTTP can finish the login.
func TestAccOidcLogin(t *testing.T) {
	endpoint := requireLocalStack(t)
	issuer := meshInfo(t, endpoint).Issuer.String()
	logins := devLogins(t)

	t.Run("a wrong password logs nobody in", func(t *testing.T) {
		c := newCLI(t, endpoint)
		login := startLogin(t, c, "1")

		_, page := keycloakLogin(t, login.awaitStartURL(t), "full", logins[0].Username, "not-the-password")
		assert.Truef(t, strings.HasPrefix(page.url.String(), issuer), "the access level page redirected to %s, not to %s", page.url, issuer)
		stillAsksForALogin := strings.Contains(page.body, "kc-form-login")
		assert.Truef(t, stillAsksForALogin, "keycloak took a wrong password and answered %s", page.url)
		assert.Equal(t, "Invalid username or password.", keycloakFeedback(page.body))

		login.abort()
		assert.NoFileExists(t, c.credentialsJson())
	})

	for _, login := range logins {
		t.Run(login.Username, func(t *testing.T) {
			c := newCLI(t, endpoint)
			run := startLogin(t, c, "1")

			completeKeycloakLogin(t, run.awaitStartURL(t), "full", login.Username, login.Password)

			if len(login.Workspaces) == 0 {
				require.Errorf(t, run.wait(), "the login stored a credential it cannot use:\n%s", run.output.String())
				assert.Contains(t, run.output.String(), refusedWithoutAWorkspace)
				assert.NoFileExists(t, c.credentialsJson())
				return
			}
			require.NoErrorf(t, run.wait(), "the browser login did not finish:\n%s", run.output.String())
			assert.Contains(t, run.output.String(), "| meshStack | ", "the login shows the status, with the meshStack it reached")
			requireStoredLogin(t, c, run.output.String())
			requireAuthStatus(t, c, "Browser login")
		})
	}

	t.Run("a changed access level asks for consent again", func(t *testing.T) {
		login := firstLoginWithAWorkspace(t, logins)
		c := newCLI(t, endpoint)
		loginWith := func(level string) (askedForConsent bool) {
			run := startLogin(t, c, "1")
			startURL := run.awaitStartURL(t)
			askedForConsent = completeKeycloakLogin(t, startURL, level, login.Username, login.Password)
			require.NoErrorf(t, run.wait(), "the browser login did not finish:\n%s", run.output.String())
			assert.Equal(t, level, storedAccessLevel(t, c), "the credential keeps the chosen access level")
			return
		}

		loginWith("read")
		assert.True(t, loginWith("write"), "a changed access level must ask for consent again")
		assert.False(t, loginWith("write"), "the same access level must reuse the consent keycloak remembers")
	})
}

func firstLoginWithAWorkspace(t *testing.T, logins []devLogin) devLogin {
	t.Helper()
	found := slices.IndexFunc(logins, func(login devLogin) bool { return len(login.Workspaces) > 0 })
	require.NotEqualf(t, -1, found, "%s carries no login with a workspace", envTestUsers)
	return logins[found]
}

func storedAccessLevel(t *testing.T, c *cli) string {
	t.Helper()
	content, err := os.ReadFile(c.credentialsJson())
	require.NoError(t, err)
	var credentials struct {
		OidcLogin struct {
			AccessLevel string `json:"accessLevel"`
		} `json:"oidcLogin"`
	}
	require.NoError(t, json.Unmarshal(content, &credentials))
	return credentials.OidcLogin.AccessLevel
}

// TestAccApiKeyLogin logs in with the API key the Terraform provider's acceptance suite uses, and
// finishes with the token that login cached: reading it back off disk is the only way to reach
// --apitoken without minting a token, and a configuration directory is writable in CI too.
func TestAccApiKeyLogin(t *testing.T) {
	endpoint := requireLocalStack(t)
	c := newCLI(t, endpoint)
	c.setEnv(setting.ApiKeyClientId.EnvKey(), requireEnv(t, setting.ApiKeyClientId.EnvKey()))
	c.setEnv(setting.ApiKeyClientSecret.EnvKey(), requireEnv(t, setting.ApiKeyClientSecret.EnvKey()))

	// A bare --apikey reads the id from the environment, which is what its NoOptDefVal is for.
	output, err := c.run("1\n", "login", "--apikey")
	require.NoErrorf(t, err, "the API key login did not finish:\n%s", output)
	assert.Contains(t, output, "| meshStack | ", "the login shows the status, with the meshStack it reached")
	requireStoredLogin(t, c, output)
	requireAuthStatus(t, c, "API key")

	t.Run("--apitoken sends the token the API key login cached", func(t *testing.T) {
		withToken := newCLI(t, endpoint)
		withToken.setEnv(setting.ApiToken.EnvKey(), cachedApiKeyToken(t, c))

		output, err := withToken.run("", "login", "--apitoken")
		require.NoErrorf(t, err, "the API token login did not finish:\n%s", output)
		assert.Contains(t, output, "| meshStack | ", "the login shows the status, with the meshStack it reached")
		require.FileExists(t, withToken.credentialsJson())
		requireAuthStatus(t, withToken, "API token")
	})
}

// requireAuthStatus does not check whether this meshStack lets an API key read itself through
// /self yet, so it accepts the warnings about the key's details.
func requireAuthStatus(t *testing.T, c *cli, wantCredential string) {
	t.Helper()
	status, err := c.run("", "auth", "status")
	require.NoErrorf(t, err, "meshstack auth status failed:\n%s", status)
	assert.Contains(t, status, "| Credential | "+wantCredential)
}

func requireStoredLogin(t *testing.T, c *cli, output string) {
	t.Helper()
	require.FileExists(t, c.profilesJson())
	require.FileExists(t, c.credentialsJson())

	profiles, err := os.ReadFile(c.profilesJson())
	require.NoError(t, err)
	assert.Contains(t, string(profiles), `"default_workspace"`, "the login stored the workspace it resolved")
	// The identifier is read out of the list the login printed, which only appears where more than
	// one workspace is reachable, so this holds for any seed.
	if offered := regexp.MustCompile(`\[ *1\] .*\((\S+)\)`).FindStringSubmatch(output); offered != nil {
		assert.Contains(t, string(profiles), offered[1], "the selected workspace is the profile's default")
	}
}

func cachedApiKeyToken(t *testing.T, c *cli) string {
	t.Helper()
	content, err := os.ReadFile(c.credentialsCacheJson("apiKey"))
	require.NoError(t, err)
	var cacheFile struct {
		Cache struct {
			Token string `json:"token"`
		} `json:"cache"`
	}
	require.NoError(t, json.Unmarshal(content, &cacheFile))
	require.NotEmpty(t, cacheFile.Cache.Token, "the API key login cached no token to log in with")
	return cacheFile.Cache.Token
}

func startLogin(t *testing.T, c *cli, workspaceAnswer string) *cliRun {
	t.Helper()
	return c.start(workspaceAnswer+"\n", "login")
}

// awaitStartURL reads the output while the login runs, since a browser login writes the URL of its
// access level page and then blocks on the redirect.
func (r *cliRun) awaitStartURL(t *testing.T) string {
	t.Helper()
	printed := regexp.MustCompile(`http://127\.0\.0\.1:\d+\S*`)
	timeout := time.After(time.Minute)
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for {
		if found := printed.FindString(r.output.String()); found != "" {
			return found
		}
		select {
		case <-r.finished:
			t.Fatalf("`meshstack login` ended without printing the URL of its login page. It said:\n%s", r.output.String())
		case <-timeout:
			t.Fatalf("no login page URL appeared within a minute. `meshstack login` said:\n%s", r.output.String())
		case <-poll.C:
		}
	}
}

// completeKeycloakLogin reports whether keycloak asked for consent, which it does for an access
// level the user has not granted yet, and on prompt=consent.
func completeKeycloakLogin(t *testing.T, startURL, accessLevel, username, password string) (askedForConsent bool) {
	t.Helper()
	browser, page := keycloakLogin(t, startURL, accessLevel, username, password)
	// Asserted as a bool: a Contains assertion would quote two hundred lines of patternfly.
	stillAsksForALogin := strings.Contains(page.body, "kc-form-login")
	require.Falsef(t, stillAsksForALogin,
		"keycloak is still asking for a login, so it refused %s: %s", username, keycloakFeedback(page.body))

	askedForConsent = strings.Contains(page.body, consentAction)
	if askedForConsent {
		page = submitForm(t, browser, page, `action="[^"]*`+consentAction, url.Values{
			"code":   {firstSubmatch(t, page.body, `name="code" value="([^"]*)"`)},
			"accept": {"Yes"},
		})
	}
	require.Containsf(t, page.body, "You are logged in", "the login did not reach the CLI's result page at %s", page.url)
	return
}

// keycloakLogin is the browser's part up to the consent screen: pick the access level on the CLI's
// own page, which redirects to keycloak, and post the login form there. Keycloak then redirects to
// http://127.0.0.1:<port>/callback, which hands the CLI its authorization code and ends its wait.
func keycloakLogin(t *testing.T, startURL, accessLevel, username, password string) (*gohttp.Client, htmlPage) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	// Keycloak carries the authentication session in a cookie, so the jar is not a convenience.
	browser := &gohttp.Client{Jar: jar, Timeout: 30 * time.Second}

	page := fetch(t, browser, startURL)
	page = submitForm(t, browser, page, `action="/"`, url.Values{"access": {accessLevel}})
	asksForALogin := strings.Contains(page.body, "kc-form-login")
	require.Truef(t, asksForALogin,
		"keycloak served no login form at %s, but %s", page.url, keycloakFeedback(page.body))
	page = submitForm(t, browser, page, `id="kc-form-login"`, url.Values{
		"username": {username},
		"password": {password},
		// Posted empty as keycloak's own form does: leaving it out picks a different authenticator.
		"credentialId": {""},
	})
	return browser, page
}

// consentAction identifies the consent form, which carries no id of its own.
const consentAction = "login-actions/consent"

func keycloakFeedback(body string) string {
	said := regexp.MustCompile(`kc-feedback-text[^>]*>([^<]*)<`).FindStringSubmatch(body)
	if len(said) != 2 {
		return "and said nothing about why"
	}
	return strings.TrimSpace(said[1])
}

type htmlPage struct {
	// url is where the page was finally served from, which a relative form action resolves against.
	url  *url.URL
	body string
}

func fetch(t *testing.T, browser *gohttp.Client, target string) htmlPage {
	t.Helper()
	req, err := gohttp.NewRequestWithContext(t.Context(), gohttp.MethodGet, target, nil)
	require.NoError(t, err)
	return read(t, browser, req)
}

// submitForm posts every hidden input the matched form carries as well: keycloak puts a session
// code in the action URL and, on some screens, more in hidden fields.
func submitForm(t *testing.T, browser *gohttp.Client, page htmlPage, formPattern string, values url.Values) htmlPage {
	t.Helper()
	form := formMatching(t, page.body, formPattern)
	action, err := page.url.Parse(html.UnescapeString(firstSubmatch(t, form, `action="([^"]*)"`)))
	require.NoError(t, err)

	posted := url.Values{}
	for _, hidden := range regexp.MustCompile(`<input[^>]*type="hidden"[^>]*>`).FindAllString(form, -1) {
		name := regexp.MustCompile(`name="([^"]*)"`).FindStringSubmatch(hidden)
		value := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(hidden)
		if len(name) == 2 && len(value) == 2 {
			posted.Set(name[1], html.UnescapeString(value[1]))
		}
	}
	maps.Copy(posted, values)

	req, err := gohttp.NewRequestWithContext(t.Context(), gohttp.MethodPost, action.String(), strings.NewReader(posted.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return read(t, browser, req)
}

// formMatching cuts out the one <form> whose opening tag matches. A keycloak page carries more than
// one — the locale picker is a form too — so the first one is not the one to post to.
func formMatching(t *testing.T, body, pattern string) string {
	t.Helper()
	opening := regexp.MustCompile(`<form[^>]*` + pattern + `[^>]*>`).FindStringIndex(body)
	require.NotNilf(t, opening, "no <form> matching %s in:\n%s", pattern, body)
	element, _, _ := strings.Cut(body[opening[0]:], "</form>")
	return element
}

func read(t *testing.T, browser *gohttp.Client, req *gohttp.Request) htmlPage {
	t.Helper()
	resp, err := browser.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// resp.Request is the last request in the redirect chain, so its URL is the page's own.
	return htmlPage{url: resp.Request.URL, body: string(body)}
}

func firstSubmatch(t *testing.T, body, pattern string) string {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(body)
	require.Lenf(t, match, 2, "nothing matched %s in:\n%s", pattern, body)
	return match[1]
}
