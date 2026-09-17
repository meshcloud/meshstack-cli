package testacc

import (
	"bufio"
	"html"
	"io"
	"maps"
	gohttp "net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A seeded login of the local dev stack, hardcoded because /mesh/info publishes no logins to
// discover one from. Local dev credentials for a keycloak on loopback, so they are worth nothing
// anywhere else; requireLocalStack is what keeps them from ever being posted somewhere they would be.
//
// This one and not another: ../meshfed-release reconciles these four logins into keycloak on every
// meshfed-api startup, password included, and of the four only this one holds a role on the admin
// workspace. See DevLocalUserBootstrapService and the dev-local-credentials block of
// meshfed/api/src/main/resources/application-default.dhall there.
//
// CI will get them differently: the gradle go-satellite in ../meshfed-release runs this suite and
// exports MESHSTACK_API_KEY and MESHSTACK_API_SECRET but no login, so wiring one through is still
// to come.
const (
	devUsername = "partner@meshcloud.io"
	devPassword = "sample123"
)

// TestAccBrowserLogin drives the authorization code flow with no browser and no terminal, which
// is the shape CI has. It works because the CLI prints the authorization URL to stderr and then
// waits on a loopback listener, so anything that can read stderr and speak HTTP can finish the
// login — here, keycloak's own forms, posted by an http.Client.
func TestAccBrowserLogin(t *testing.T) {
	endpoint := requireLocalStack(t)
	info := meshInfo(t, endpoint)

	c := newCLI(t, endpoint)
	login := startLogin(t, c, info.Issuer.String())

	completeKeycloakLogin(t, login.awaitAuthorizationURL(t), devUsername, devPassword)

	require.NoErrorf(t, login.wait(), "the browser login did not finish:\n%s", login.output.String())
	assert.Contains(t, login.output.String(), "logged in at meshStack", "the login reports what it reached")

	// The login is only worth anything if it left something behind for the next invocation.
	require.FileExists(t, c.profilesJson())
	require.FileExists(t, c.credentialsJson("default"))
}

// loginRun is one `meshstack login` subprocess, with a goroutine draining its stderr. The
// draining has to happen while the command is still running: login writes the authorization URL
// and then blocks on the redirect, so nothing about it is readable after the fact.
type loginRun struct {
	cmd     *exec.Cmd
	output  *syncBuffer
	authURL chan string
	drained chan struct{}
}

func startLogin(t *testing.T, c *cli, issuer string) *loginRun {
	t.Helper()
	cmd := c.command("login")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	cmd.Stdout = nil

	run := &loginRun{
		cmd:    cmd,
		output: &syncBuffer{},
		// Buffered, so the scanner never blocks on a test that has already given up.
		authURL: make(chan string, 1),
		drained: make(chan struct{}),
	}
	require.NoError(t, cmd.Start())

	// The URL is recognised by its issuer prefix rather than by the sentence around it, so a
	// reworded prompt does not break this and a different issuer does.
	printed := regexp.MustCompile(regexp.QuoteMeta(issuer) + `/\S+`)
	go func() {
		defer close(run.drained)
		lines := bufio.NewScanner(stderr)
		for lines.Scan() {
			line := lines.Bytes()
			_, _ = run.output.Write(append(line, '\n'))
			if found := printed.Find(line); found != nil {
				select {
				case run.authURL <- string(found):
				default:
				}
			}
		}
	}()
	return run
}

func (r *loginRun) awaitAuthorizationURL(t *testing.T) string {
	t.Helper()
	select {
	case found := <-r.authURL:
		return found
	case <-r.drained:
		t.Fatalf("`meshstack login` ended without printing an authorization URL. It said:\n%s", r.output.String())
	case <-time.After(time.Minute):
		t.Fatalf("no authorization URL appeared within a minute. `meshstack login` said:\n%s", r.output.String())
	}
	return ""
}

// wait reads stderr to its end before reaping the command, which is what os/exec requires of a
// StderrPipe. The full output is then in r.output for a failure message to quote.
func (r *loginRun) wait() error {
	<-r.drained
	return r.cmd.Wait()
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
	browser := &gohttp.Client{Jar: jar, Timeout: 30 * time.Second}

	page := fetch(t, browser, authURL)
	// The condition is named rather than asserted on inline, because the Contains assertions
	// quote two hundred lines of patternfly on failure and keycloakFeedback says it in one.
	asksForALogin := strings.Contains(page.body, "kc-form-login")
	require.Truef(t, asksForALogin,
		"keycloak served no login form at %s, but %s", authURL, keycloakFeedback(page.body))
	page = submitForm(t, browser, page, "kc-form-login", url.Values{
		"username": {username},
		"password": {password},
		// Posted empty, as keycloak's own form does: leaving the field out picks a different
		// authenticator.
		"credentialId": {""},
	})

	// The consent screen appears on a first login for this client and not on later ones, so this
	// is conditional rather than a step.
	if strings.Contains(page.body, "login-actions/consent") {
		page = submitForm(t, browser, page, "kc-form-login", url.Values{
			"code":   {firstSubmatch(t, page.body, `name="code" value="([^"]*)"`)},
			"accept": {"Yes"},
		})
	}
	stillAsksForALogin := strings.Contains(page.body, "kc-form-login")
	require.Falsef(t, stillAsksForALogin,
		"keycloak is still asking for a login, so it refused %s: %s", username, keycloakFeedback(page.body))
}

// keycloakFeedback pulls the one sentence keycloak puts on the page when it refuses something.
// Quoted instead of the page, because a keycloak login page is 200 lines of patternfly.
func keycloakFeedback(body string) string {
	said := regexp.MustCompile(`kc-feedback-text[^>]*>([^<]*)<`).FindStringSubmatch(body)
	if len(said) != 2 {
		return "and said nothing about why"
	}
	return strings.TrimSpace(said[1])
}

// htmlPage is one response: what it said, and where it was finally served from — which is what a
// relative form action has to resolve against after a chain of redirects.
type htmlPage struct {
	url  *url.URL
	body string
}

func fetch(t *testing.T, browser *gohttp.Client, target string) htmlPage {
	t.Helper()
	req, err := gohttp.NewRequestWithContext(t.Context(), gohttp.MethodGet, target, nil)
	require.NoError(t, err)
	return read(t, browser, req)
}

// submitForm posts to the action of the form with the given id, plus every hidden input that form
// carries: keycloak puts a session code in the action URL and, on some screens, more in hidden
// fields, and a POST that drops either is a different request than the browser's.
func submitForm(t *testing.T, browser *gohttp.Client, page htmlPage, formId string, values url.Values) htmlPage {
	t.Helper()
	form := formWithId(t, page.body, formId)
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

// formWithId cuts out one <form> element. Keycloak's login page carries more than one — the
// locale picker is a form too — so the action of the first one is not the one to post to.
func formWithId(t *testing.T, body, formId string) string {
	t.Helper()
	opening := regexp.MustCompile(`<form[^>]*id="` + regexp.QuoteMeta(formId) + `"[^>]*>`).FindStringIndex(body)
	require.NotNilf(t, opening, "no <form id=%q> in:\n%s", formId, body)
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
