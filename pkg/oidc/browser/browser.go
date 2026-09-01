// Package browser runs the interactive half of the OIDC protocol: the authorization code
// flow with PKCE, the loopback listener that catches the redirect, and opening a browser.
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
)

// callbackPath is part of the redirect URI registered on the keycloak client. Keycloak
// matches any port on a loopback address but matches host and path literally, so this and
// the 127.0.0.1 below are both fixed — localhost and ::1 are rejected.
const callbackPath = "/callback"

// loginTimeout is how long the listener waits for a person to finish in the browser. The
// consent screen on a first login makes a short timeout hostile.
const loginTimeout = 10 * time.Minute

// envNoBrowser suppresses the launch and leaves the printed URL as the whole of the offer.
// It is not the same statement as MESHSTACK_NO_INPUT, which says nobody is coming and makes
// the login refuse outright: here somebody is coming, just not through a browser this process
// started. That covers an ssh session whose $DISPLAY belongs to the wrong machine, a
// container, and a test driving the login over HTTP — all cases where opening a window is at
// best useless and at worst somebody else's screen.
const envNoBrowser = "MESHSTACK_NO_BROWSER"

// Login supplies the two things the authorization code flow needs and pkg/oidc cannot have: a
// loopback address it can receive the redirect on, and a person in front of a browser. The
// protocol itself belongs to the flow — see oidc.AuthorizationCodeFlow.
func Login(ctx context.Context, client oidc.Client) (oidc.Token, error) {
	// Bound first: the port it happens to get is part of the redirect URI, which the flow has
	// to put in the authorization request and echo back in the token request.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return oidc.Token{}, fmt.Errorf("cannot listen on a loopback port for the login redirect: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		// await is what otherwise closes the listener, through the server it hands it to.
		_ = listener.Close()
		return oidc.Token{}, fmt.Errorf("cannot determine the port of the loopback listener, got %s", listener.Addr())
	}

	// callbackPath carries its own leading slash, so the format string must not add one: a
	// redirect URI of //callback is not the one keycloak has registered.
	flow := client.NewAuthorizationCode(xurl.MustParsef("http://%s%s", addr, callbackPath))

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
			detail := refused
			if description := query.Get("error_description"); description != "" {
				detail += ": " + description
			}
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

// openBrowser asks the desktop to open the authorization URL, and prints it either way: a failure
// to launch a browser is not a failure to log in, because the user can still paste the URL. A
// headless machine reaches this path every time.
//
// The three lines go straight to stderr rather than through slog. The URL is not a record of what
// happened, it is the thing the person has to copy, so it must survive any log level and reach the
// terminal unprefixed and unquoted — which is also how the prompt in internal/cli writes. Only
// what goes wrong is logged.
func openBrowser(authURL *url.URL) {
	suppressed := os.Getenv(envNoBrowser) != ""
	if suppressed {
		fmt.Fprintf(os.Stderr, "%s is set, so no browser is opened. Log in to meshStack at:\n", envNoBrowser)
	} else {
		fmt.Fprintln(os.Stderr, "Opening your browser to log in to meshStack. If it does not open, visit:")
	}
	fmt.Fprintf(os.Stderr, "\n  %s\n\n", authURL)
	// Said out loud because this is where an unattended run stops for ten minutes. The URL above
	// is the whole of what a person needs, so the login waits whether or not a terminal is
	// attached — --no-input is how a script says nobody is coming.
	fmt.Fprintf(os.Stderr, "Waiting up to %s for you to finish. Pass --no-input to fail at once instead.\n", loginTimeout)
	if suppressed {
		return
	}

	// A platform with no execBrowserOpen fails to build rather than falling back to nothing,
	// because .goreleaser.yml publishes linux, darwin and windows and nothing else. Adding a
	// target means adding the file that opens a browser on it.
	cmd := execBrowserOpen(authURL.String())
	if err := cmd.Start(); err != nil {
		slog.Debug("cannot open a browser, waiting for a manually opened one instead",
			"command", strings.Join(cmd.Args, " "), "error", err)
	}
}
