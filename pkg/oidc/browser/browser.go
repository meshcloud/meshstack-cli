// Package browser runs the interactive half of the OIDC protocol: the authorization code
// flow with PKCE, the loopback listener that catches the redirect, and opening a browser.
//
// It is a package of its own so that the guarantee is a compile-time one rather than a
// promise. pkg/auth needs the refresh grant, which the Terraform provider uses, and must not
// be able to reach the browser flow at all — a plan that opened a browser would hang a CI
// run. A depguard rule in .golangci.yml lets only cmd/ import this package, so nothing the
// provider links can even name it.
package browser

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/oidc"
)

// callbackPath is part of the redirect URI registered on the keycloak client. Keycloak
// matches any port on a loopback address but matches host and path literally, so this and
// the 127.0.0.1 below are both fixed — localhost and ::1 are rejected.
const callbackPath = "/callback"

// loginTimeout is how long the listener waits for a person to finish in the browser. The
// consent screen on a first login makes a short timeout hostile.
const loginTimeout = 10 * time.Minute

// Browser is the browser login implementation. It holds no state: everything one login needs
// is created per call, including the listener.
type Browser struct{}

// New returns the browser login implementation. cmd/ hands it to pkg/auth through
// Input.Browser(); the Terraform provider returns nil there.
func New() Browser { return Browser{} }

// Login supplies the two things the authorization code flow needs and pkg/oidc cannot have: a
// loopback address it can receive the redirect on, and a person in front of a browser. The
// protocol itself belongs to the flow this asks for — see oidc.AuthorizationCodeStarter.
func (Browser) Login(ctx context.Context, starter oidc.AuthorizationCodeStarter) (oidc.Token, error) {
	// Bound first: the port it happens to get is part of the redirect URI, which the flow has
	// to put in the authorization request and echo back in the token request.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return oidc.Token{}, fmt.Errorf("cannot listen on a loopback port for the login redirect: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return oidc.Token{}, fmt.Errorf("cannot determine the port of the loopback listener, got %s", listener.Addr())
	}

	flow, err := starter.NewAuthorizationCode(fmt.Sprintf("http://127.0.0.1:%d%s", addr.Port, callbackPath))
	if err != nil {
		// await is what otherwise closes the listener, through the server it hands it to.
		_ = listener.Close()
		return oidc.Token{}, err
	}

	code, err := await(ctx, listener, flow)
	if err != nil {
		return oidc.Token{}, err
	}
	return flow.Exchange(ctx, code)
}

// callback is what the redirect carried: one of the two fields is always empty.
type callback struct {
	code string
	err  error
}

// await serves the loopback listener until the redirect arrives, and always shuts it down
// before returning so that no login leaves a port bound behind it.
func await(ctx context.Context, listener net.Listener, flow oidc.AuthorizationCodeFlow) (string, error) {
	// Buffered, so the handler never blocks on a caller that has already given up.
	arrived := make(chan callback, 1)
	server := &http.Server{
		Handler:           handler(flow, arrived),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			arrived <- callback{err: fmt.Errorf("the loopback listener for the login redirect failed: %w", err)}
		}
	}()
	defer func() {
		// A fresh context: the caller's may already be cancelled, and the browser is still
		// reading the page the handler wrote.
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Debug("the loopback listener did not shut down cleanly", "error", err)
		}
	}()

	openBrowser(flow.URL())

	select {
	case result := <-arrived:
		return result.code, result.err
	case <-ctx.Done():
		return "", fmt.Errorf("the browser login was cancelled before the redirect arrived: %w", ctx.Err())
	case <-time.After(loginTimeout):
		return "", fmt.Errorf("no login redirect arrived within %s, so the browser login was abandoned", loginTimeout)
	}
}

func handler(flow oidc.AuthorizationCodeFlow, arrived chan<- callback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != callbackPath {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()

		if refused := query.Get("error"); refused != "" {
			detail := describe(refused, query.Get("error_description"))
			page(w, http.StatusBadRequest, "Login failed", detail)
			arrived <- callback{err: fmt.Errorf("the identity provider refused the login: %s", detail)}
			return
		}
		if err := flow.CheckState(query.Get("state")); err != nil {
			page(w, http.StatusBadRequest, "Login failed", "The redirect carried the wrong state parameter, so it did not belong to this login.")
			arrived <- callback{err: err}
			return
		}
		if query.Get("code") == "" {
			page(w, http.StatusBadRequest, "Login failed", "The redirect carried no authorization code.")
			arrived <- callback{err: fmt.Errorf("the login redirect carried no authorization code")}
			return
		}
		page(w, http.StatusOK, "You are logged in", "The meshStack CLI has your login. You can close this tab and return to your terminal.")
		arrived <- callback{code: query.Get("code")}
	})
}

func describe(code, description string) string {
	if description == "" {
		return code
	}
	return code + ": " + description
}

// resultPage is deliberately plain: it is shown once, in a tab the user closes immediately.
var resultPage = template.Must(template.New("callback").Parse(
	`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>meshStack CLI</title></head>
<body style="font-family: system-ui, sans-serif; margin: 4rem auto; max-width: 34rem;">
<h1 style="font-size: 1.25rem;">{{.Title}}</h1>
<p>{{.Message}}</p>
</body>
</html>
`))

func page(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := resultPage.Execute(w, struct{ Title, Message string }{title, message}); err != nil {
		slog.Debug("cannot write the login result page", "error", err)
	}
}

// openBrowser asks the desktop to open the authorization URL, and prints it either way: a failure to
// launch a browser is not a failure to log in, because the user can still paste the URL. A
// headless machine reaches this path every time.
func openBrowser(authURL string) {
	fmt.Fprintln(os.Stderr, "Opening your browser to log in to meshStack. If it does not open, visit:")
	fmt.Fprintln(os.Stderr, "\n  "+authURL+"\n")
	// Said out loud because this is where an unattended run stops for ten minutes. The URL above
	// is the whole of what a person needs, so the login waits whether or not a terminal is
	// attached — --no-input is how a script says nobody is coming.
	fmt.Fprintf(os.Stderr, "Waiting up to %s for you to finish. Pass --no-input to fail at once instead.\n", loginTimeout)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", authURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", authURL)
	default:
		cmd = exec.Command("xdg-open", authURL)
	}
	if err := cmd.Start(); err != nil {
		slog.Debug("cannot open a browser, waiting for a manually opened one instead",
			"command", strings.Join(cmd.Args, " "), "error", err)
	}
}
