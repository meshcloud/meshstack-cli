package browser

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	gohttp "net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/io"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

const (
	startPath    = "/"
	callbackPath = "/callback"
)

// Login supplies the two things oidc.AuthorizationCodeFlow needs and internal/oidc cannot have:
// a loopback address to receive the redirect on, and a person in front of a browser. The browser
// opens on a page of the loopback server first, where the person picks the access level, and that
// page redirects on to the identity provider. It returns the level the identity provider granted,
// which is none on a meshStack older than access levels.
func Login(ctx context.Context, client oidc.Client, previous meshstack.AccessLevel) (oidc.Token, meshstack.AccessLevel, error) {
	// Bound first, because the port it gets is part of the redirect URI, which the flow puts
	// in the authorization request and echoes back in the token request.
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return oidc.Token{}, "", fmt.Errorf("cannot listen on a loopback port for the login redirect: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		// await is what otherwise closes the listener, through the server it hands it to.
		_ = listener.Close()
		return oidc.Token{}, "", fmt.Errorf("cannot determine the port of the loopback listener, got %s", listener.Addr())
	}

	// callbackPath carries its own leading slash, so the format string must not add one: a
	// redirect URI of //callback is not the one keycloak has registered.
	login := &loginPages{
		flow:     client.NewAuthorizationCode(xurl.MustParsef("http://%s%s", addr, callbackPath)),
		previous: previous,
		nonce:    rand.Text(),
		arrived:  make(chan callback, 1),
	}

	code, err := await(ctx, listener, login, xurl.MustParsef("http://%s%s", addr, startPath))
	if err != nil {
		return oidc.Token{}, "", err
	}
	token, err := login.flow.Exchange(ctx, code)
	if err != nil {
		return oidc.Token{}, "", err
	}
	chosen := login.chosenLevel()
	if slices.Contains(strings.Fields(token.Scope), string(chosen.Scope())) {
		return token, chosen, nil
	}
	if chosen != meshstack.AccessFull {
		return oidc.Token{}, "", fmt.Errorf("%s did not grant the access level %q, so this meshStack does not support access levels yet "+
			"and the login would have full access; log in again and choose %q", client.Issuer, chosen.Label(), meshstack.AccessFull.Label())
	}
	return token, "", nil
}

type callback struct {
	code string
	err  error
}

func await(ctx context.Context, listener net.Listener, login *loginPages, startURL xurl.URL) (string, error) {
	server := &gohttp.Server{
		Handler:           login.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, gohttp.ErrServerClosed) {
			login.arrive(callback{err: fmt.Errorf("the loopback listener for the login redirect failed: %w", err)})
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

	openBrowser(ctx, startURL)

	select {
	case result := <-login.arrived:
		return result.code, result.err
	case <-ctx.Done():
		return "", fmt.Errorf("the browser login ended before the redirect arrived: %w", ctx.Err())
	}
}

type loginPages struct {
	flow     oidc.AuthorizationCodeFlow
	previous meshstack.AccessLevel
	// nonce ties a submitted access level to the page this login served, so that another site
	// cannot post a level to the loopback port.
	nonce string
	// Buffered, so the handler never blocks on a caller that has already given up.
	arrived chan callback

	mu     sync.Mutex
	chosen meshstack.AccessLevel
}

func (l *loginPages) handler() gohttp.Handler {
	mux := gohttp.NewServeMux()
	mux.HandleFunc("GET "+startPath+"{$}", l.askForAccessLevel)
	mux.HandleFunc("POST "+startPath+"{$}", l.redirectToIdentityProvider)
	mux.HandleFunc(callbackPath, l.receiveRedirect)
	return mux
}

func (l *loginPages) askForAccessLevel(w gohttp.ResponseWriter, r *gohttp.Request) {
	l.showAccessLevels(w, r, gohttp.StatusOK)
}

func (l *loginPages) showAccessLevels(w gohttp.ResponseWriter, r *gohttp.Request, status int) {
	message := "Choose what the meshStack CLI may do with your meshStack account. It never gets more rights than your roles give you."
	if status != gohttp.StatusOK {
		message = "Choose one of the access levels below."
	}
	page(r.Context(), w, status, pageData{
		Title:   "Choose an access level",
		Message: message,
		Form:    newAccessLevelForm(l.nonce, l.previous),
	})
}

func (l *loginPages) redirectToIdentityProvider(w gohttp.ResponseWriter, r *gohttp.Request) {
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("nonce")), []byte(l.nonce)) != 1 {
		page(r.Context(), w, gohttp.StatusForbidden, pageData{Title: "Login failed", Message: "The form did not come from this login. Start the login again in your terminal."})
		return
	}
	level, ok := meshstack.ParseAccessLevel(r.PostFormValue("access"))
	if !ok {
		l.showAccessLevels(w, r, gohttp.StatusBadRequest)
		return
	}
	l.mu.Lock()
	l.chosen = level
	l.mu.Unlock()
	// Keycloak asks on its own for a level the user has never granted, but not for one granted
	// before, so switching back to that one would otherwise need no click.
	askConsent := l.previous != "" && level != l.previous
	gohttp.Redirect(w, r, l.flow.BrowserUrl(scope.Scopes{level.Scope()}, askConsent).String(), gohttp.StatusSeeOther)
}

func (l *loginPages) chosenLevel() meshstack.AccessLevel {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.chosen
}

func (l *loginPages) receiveRedirect(w gohttp.ResponseWriter, r *gohttp.Request) {
	query := r.URL.Query()
	if refused := query.Get("error"); refused != "" {
		detail := refused
		if description := query.Get("error_description"); description != "" {
			detail += ": " + description
		}
		page(r.Context(), w, gohttp.StatusBadRequest, pageData{Title: "Login failed", Message: detail})
		l.arrive(callback{err: fmt.Errorf("the identity provider refused the login: %s", detail)})
		return
	}
	if err := l.flow.CheckState(query.Get("state")); err != nil {
		page(r.Context(), w, gohttp.StatusBadRequest, pageData{Title: "Login failed", Message: "The redirect carried the wrong state parameter, so it did not belong to this login."})
		l.arrive(callback{err: err})
		return
	}
	if query.Get("code") == "" {
		page(r.Context(), w, gohttp.StatusBadRequest, pageData{Title: "Login failed", Message: "The redirect carried no authorization code."})
		l.arrive(callback{err: errors.New("the login redirect carried no authorization code")})
		return
	}
	page(r.Context(), w, gohttp.StatusOK, pageData{
		Title:   "You are logged in",
		Message: fmt.Sprintf("The meshStack CLI has your login with the access level %q. You can close this tab and return to your terminal.", l.chosenLevel().Label()),
	})
	l.arrive(callback{code: query.Get("code")})
}

// arrive keeps the first result: a second callback, such as a reloaded tab, must not block.
func (l *loginPages) arrive(result callback) {
	select {
	case l.arrived <- result:
	default:
	}
}

// noBrowserEnv lets a caller act as the browser itself, as cmd/internal/testacc does: the
// authorization URL still goes to stderr, and nothing is launched on the machine. It is not a
// setting.Setting, because it has no flag, no profile entry and no help text.
const noBrowserEnv = "MESHSTACK_CLI_NO_BROWSER"

func openBrowser(ctx context.Context, startURL xurl.URL) {
	out := io.Stderr(ctx)
	_, _ = fmt.Fprintln(out, "Opening your browser to log in to meshStack. If it does not open, visit:")
	_, _ = fmt.Fprintf(out, "\n  %s\n\n", startURL)
	if deadline, ok := ctx.Deadline(); ok {
		_, _ = fmt.Fprintf(out, "Waiting up to %s for you to finish.\n", time.Until(deadline).Round(time.Second))
	}

	if _, suppressed := os.LookupEnv(noBrowserEnv); suppressed {
		slog.DebugContext(ctx, "not opening a browser, because "+noBrowserEnv+" is set")
		return
	}

	// A platform with no execBrowserOpen fails to build rather than falling back to nothing, so
	// adding a release target in .goreleaser.yml means adding the file that opens a browser on it.
	cmd := execBrowserOpen(ctx, startURL.String())
	if err := cmd.Start(); err != nil {
		slog.DebugContext(ctx, "cannot open a browser, waiting for a manually opened one instead",
			"command", strings.Join(cmd.Args, " "), "error", err)
	}
}
