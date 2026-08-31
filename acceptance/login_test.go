package acceptance

import (
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccDevLocalLogin is the .env file's replacement, proved end to end: nothing is exported,
// nothing is configured, and the endpoint alone is enough to leave the machine able to talk to
// meshStack.
func TestAccDevLocalLogin(t *testing.T) {
	endpoint, info := requireLocalStack(t)
	dev := requireDevLocalCredentials(t, endpoint, info)

	cli := newCLI(t)
	cli.mustRun("login", "--dev-local", "--endpoint", endpoint)

	require.FileExists(t, cli.config)
	// The reserved profile name, which is what makes the flag configless on the way back out.
	credentials := filepath.Join(cli.credentials, "dev-local.json")
	require.FileExists(t, credentials)
	stored, err := os.ReadFile(credentials)
	require.NoError(t, err)
	assert.Contains(t, string(stored), dev.ApiKeyClientId)
	assert.Contains(t, string(stored), dev.ApiKeyClientSecret)

	// The proof itself: the next two commands are given nothing at all, and work off what the
	// login wrote. --verify is the one that makes a round trip with the credential.
	cli.mustRun("auth", "status", "--verify")
	cli.mustRun("workspace", "list")
}

// TestAccBrowserLoginHeadless drives the authorization code flow with no browser and no
// terminal, which is the shape CI has. It works because the CLI prints the authorization URL
// to stderr and then waits on a loopback listener, so anything that can read stderr and speak
// HTTP can finish the login — here, keycloak's own forms, posted by an http.Client.
//
// ../terraform-provider-meshstack/scratch/headless-login.sh is the shell reference this
// replicates.
func TestAccBrowserLoginHeadless(t *testing.T) {
	endpoint, info := requireLocalStack(t)
	dev := requireDevLocalCredentials(t, endpoint, info)
	require.NotEmpty(t, dev.Users, "the local dev stack published no seeded logins to log in as")
	// The list is ordered and the first entry is the one meant to be used: it is the one whose
	// keycloak account carries a workspace, without which a browser login cannot act.
	user := dev.Users[0]
	require.NotEmptyf(t, user.Workspace, "the first seeded login %q has no workspace, so a browser login as it cannot act", user.Username)

	const profileName = "acc-browser-login"
	cli := newCLI(t)
	login := cli.command("login", "--profile", profileName, "--endpoint", endpoint, "--workspace", user.Workspace)
	output := &syncBuffer{}
	login.Stdout, login.Stderr = output, output
	require.NoError(t, login.Start())

	completeKeycloakLogin(t, awaitAuthorizationURL(t, output, info.Issuer.String()), user.Username, user.Password)

	require.NoErrorf(t, login.Wait(), "the browser login did not finish:\n%s", output.String())
	assert.Contains(t, output.String(), user.Username, "the login reports who it logged in as")

	// A login is only worth anything if what follows it works.
	cli.mustRun("auth", "status", "--verify", "--profile", profileName)
}

// awaitAuthorizationURL watches the subprocess's output for the URL it wants a person to
// visit. Polling is what this has to be: the CLI writes the URL and then blocks for ten
// minutes, so there is no line-oriented end to read up to.
func awaitAuthorizationURL(t *testing.T, output *syncBuffer, issuer string) string {
	t.Helper()
	printed := regexp.MustCompile(regexp.QuoteMeta(issuer) + `/\S+`)
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if found := printed.FindString(output.String()); found != "" {
			return found
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no authorization URL under %s appeared within a minute. The CLI said:\n%s", issuer, output.String())
	return ""
}

// completeKeycloakLogin is the browser's part: fetch the authorization URL, post the forms
// keycloak answers with, and follow every redirect. The last of those redirects goes to
// http://127.0.0.1:<port>/callback, and making that request is what hands the CLI its
// authorization code and ends its wait.
func completeKeycloakLogin(t *testing.T, authURL, username, password string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	// Keycloak carries the authentication session in a cookie, so the jar is not a convenience.
	browser := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	page := fetch(t, browser, authURL)
	if strings.Contains(page.body, "kc-form-login") || strings.Contains(page.body, `id="password"`) {
		page = submitForm(t, browser, page, url.Values{
			"username": {username},
			"password": {password},
			// Posted empty, as keycloak's own form does: leaving the field out picks a
			// different authenticator.
			"credentialId": {""},
		})
	}
	// The consent screen appears on a first login for this client and not on later ones, so
	// this is conditional rather than a step.
	if strings.Contains(page.body, "login-actions/consent") {
		page = submitForm(t, browser, page, url.Values{
			"code":   {firstSubmatch(t, page.body, `name="code" value="([^"]*)"`)},
			"accept": {"Yes"},
		})
	}
	require.NotContainsf(t, page.body, "kc-form-login", "keycloak is still asking for a login, so the credentials were refused:\n%s", page.body)
}

// htmlPage is one response: what it said, and where it was finally served from — which is what
// a relative form action has to resolve against after a chain of redirects.
type htmlPage struct {
	url  *url.URL
	body string
}

func fetch(t *testing.T, browser *http.Client, target string) htmlPage {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	require.NoError(t, err)
	return read(t, browser, req)
}

func submitForm(t *testing.T, browser *http.Client, page htmlPage, values url.Values) htmlPage {
	t.Helper()
	action, err := page.url.Parse(html.UnescapeString(firstSubmatch(t, page.body, `action="([^"]*)"`)))
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, action.String(), strings.NewReader(values.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return read(t, browser, req)
}

func read(t *testing.T, browser *http.Client, req *http.Request) htmlPage {
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
