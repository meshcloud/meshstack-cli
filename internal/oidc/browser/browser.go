package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	gohttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/io"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
)

const callbackPath = "/callback"

// Login supplies the two things oidc.AuthorizationCodeFlow needs and internal/oidc cannot have:
// a loopback address to receive the redirect on, and a person in front of a browser.
func Login(ctx context.Context, client oidc.Client) (oidc.Token, error) {
	// Bound first, because the port it gets is part of the redirect URI, which the flow puts
	// in the authorization request and echoes back in the token request.
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
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

type callback struct {
	code string
	err  error
}

func await(ctx context.Context, listener net.Listener, flow oidc.AuthorizationCodeFlow) (string, error) {
	// Buffered, so the handler never blocks on a caller that has already given up.
	arrived := make(chan callback, 1)
	server := &gohttp.Server{
		Handler:           handler(flow, arrived),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, gohttp.ErrServerClosed) {
			arrived <- callback{err: fmt.Errorf("the loopback listener for the login redirect failed: %w", err)}
		}
	}()
	defer func() {
		// A fresh context: the caller's may already be cancelled, and the browser is still
		// reading the page the handler wrote.
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.DebugContext(shutdown, "the loopback listener did not shut down cleanly", "error", err)
		}
	}()

	openBrowser(ctx, flow.BrowserUrl())

	select {
	case result := <-arrived:
		return result.code, result.err
	case <-ctx.Done():
		return "", fmt.Errorf("the browser login ended before the redirect arrived: %w", ctx.Err())
	}
}

func handler(flow oidc.AuthorizationCodeFlow, arrived chan<- callback) gohttp.Handler {
	return gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		if r.URL.Path != callbackPath {
			gohttp.NotFound(w, r)
			return
		}
		query := r.URL.Query()

		if refused := query.Get("error"); refused != "" {
			detail := refused
			if description := query.Get("error_description"); description != "" {
				detail += ": " + description
			}
			page(r.Context(), w, gohttp.StatusBadRequest, "Login failed", detail)
			arrived <- callback{err: fmt.Errorf("the identity provider refused the login: %s", detail)}
			return
		}
		if err := flow.CheckState(query.Get("state")); err != nil {
			page(r.Context(), w, gohttp.StatusBadRequest, "Login failed", "The redirect carried the wrong state parameter, so it did not belong to this login.")
			arrived <- callback{err: err}
			return
		}
		if query.Get("code") == "" {
			page(r.Context(), w, gohttp.StatusBadRequest, "Login failed", "The redirect carried no authorization code.")
			arrived <- callback{err: errors.New("the login redirect carried no authorization code")}
			return
		}
		page(r.Context(), w, gohttp.StatusOK, "You are logged in", "The meshStack CLI has your login. You can close this tab and return to your terminal.")
		arrived <- callback{code: query.Get("code")}
	})
}

// noBrowserEnv lets a caller act as the browser itself, as cmd/internal/testacc does: the
// authorization URL still goes to stderr, and nothing is launched on the machine. It is not a
// setting.Setting, because it has no flag, no profile entry and no help text.
const noBrowserEnv = "MESHSTACK_CLI_NO_BROWSER"

func openBrowser(ctx context.Context, authURL xurl.URL) {
	out := io.Stderr(ctx)
	_, _ = fmt.Fprintln(out, "Opening your browser to log in to meshStack. If it does not open, visit:")
	_, _ = fmt.Fprintf(out, "\n  %s\n\n", authURL)
	if deadline, ok := ctx.Deadline(); ok {
		_, _ = fmt.Fprintf(out, "Waiting up to %s for you to finish.\n", time.Until(deadline).Round(time.Second))
	}

	if _, suppressed := os.LookupEnv(noBrowserEnv); suppressed {
		slog.DebugContext(ctx, "not opening a browser, because "+noBrowserEnv+" is set")
		return
	}

	// A platform with no execBrowserOpen fails to build rather than falling back to nothing, so
	// adding a release target in .goreleaser.yml means adding the file that opens a browser on it.
	cmd := execBrowserOpen(ctx, authURL.String())
	if err := cmd.Start(); err != nil {
		slog.DebugContext(ctx, "cannot open a browser, waiting for a manually opened one instead",
			"command", strings.Join(cmd.Args, " "), "error", err)
	}
}
