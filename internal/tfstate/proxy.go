package tfstate

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	gohttp "net/http"
	"sync"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

const (
	username  = "meshstack"
	statePath = "/state"
)

var ErrReadOnly = errors.New("the state is read-only")

// Proxy stands between tofu and meshStack, rather than giving tofu the session's token as
// TF_HTTP_PASSWORD, which meshfed's TfStateRunTokenBasicAuthFilter accepts. That token can expire
// in the middle of an apply, and nothing would check tofu's writes against the stored state.
type Proxy struct {
	Store       Store
	Writable    bool
	BeforeWrite func(ctx context.Context) error
	Backups     Backups
	// OnProblem gets the error of each request that failed, because tofu prints only the status code
	// of the response.
	OnProblem func(ctx context.Context, err error)

	password string
	// mu is held for the whole request, so that no other write comes between a write's check of the
	// stored state and the write itself.
	mu       sync.Mutex
	requests int
}

func (p *Proxy) Serve(ctx context.Context, run func(env []string)) (requests int, err error) {
	p.password = rand.Text()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("cannot serve the state on a loopback port: %w", err)
	}
	server := &gohttp.Server{
		Handler:           p,
		ReadHeaderTimeout: time.Minute,
		// Ctrl-C ends ctx, and tofu then stores what it has done so far.
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	run([]string{
		"TF_HTTP_ADDRESS=http://" + listener.Addr().String() + statePath,
		"TF_HTTP_USERNAME=" + username,
		"TF_HTTP_PASSWORD=" + p.password,
	})

	err = server.Shutdown(context.WithoutCancel(ctx))
	if serveErr := <-served; !errors.Is(serveErr, gohttp.ErrServerClosed) {
		err = errors.Join(err, serveErr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests, err
}

func (p *Proxy) ServeHTTP(w gohttp.ResponseWriter, r *gohttp.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests++
	user, password, ok := r.BasicAuth()
	if !ok || user != username || subtle.ConstantTimeCompare([]byte(password), []byte(p.password)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="meshstack"`)
		p.refuse(w, r, gohttp.StatusUnauthorized, errors.New("a request for the state came without the TF_HTTP_USERNAME and TF_HTTP_PASSWORD it was given"))
		return
	}
	if r.URL.Path != statePath {
		p.refuse(w, r, gohttp.StatusNotFound, fmt.Errorf("a request asked for %s, but the state is at %s", r.URL.Path, statePath))
		return
	}
	switch r.Method {
	case gohttp.MethodGet:
		p.get(w, r)
	case gohttp.MethodPost, gohttp.MethodDelete:
		p.write(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		p.refuse(w, r, gohttp.StatusMethodNotAllowed, fmt.Errorf("a request asked to %s the state, which meshStack does not offer", r.Method))
	}
}

func (p *Proxy) get(w gohttp.ResponseWriter, r *gohttp.Request) {
	state, err := p.Store.Get(r.Context())
	switch {
	case errors.Is(err, ErrNoState):
		w.WriteHeader(gohttp.StatusNotFound)
	case err != nil:
		p.fail(w, r, err)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(state)
	}
}

func (p *Proxy) write(w gohttp.ResponseWriter, r *gohttp.Request) {
	ctx := r.Context()
	if !p.Writable {
		p.refuse(w, r, gohttp.StatusForbidden, ErrReadOnly)
		return
	}
	if p.BeforeWrite != nil {
		if err := p.BeforeWrite(ctx); err != nil {
			p.refuse(w, r, gohttp.StatusConflict, err)
			return
		}
	}
	var written []byte
	if r.Method == gohttp.MethodPost {
		var err error
		if written, err = io.ReadAll(r.Body); err != nil {
			p.refuse(w, r, gohttp.StatusBadRequest, err)
			return
		}
	}

	stored, err := p.Store.Get(ctx)
	switch {
	case errors.Is(err, ErrNoState) && r.Method == gohttp.MethodDelete:
		w.WriteHeader(gohttp.StatusNotFound)
		return
	case errors.Is(err, ErrNoState):
		stored = nil
	case err != nil:
		p.fail(w, r, err)
		return
	}
	if r.Method == gohttp.MethodPost {
		var next version
		var previous *version
		if next, previous, err = versionsOf(written, stored); err == nil {
			err = next.follows(previous)
		}
		if err != nil {
			p.refuse(w, r, gohttp.StatusConflict, err)
			return
		}
	}
	if stored != nil {
		var path string
		if path, err = p.Backups.save(p.Store.BuildingBlock, stored, time.Now()); err != nil {
			p.refuse(w, r, gohttp.StatusInternalServerError, fmt.Errorf("cannot back up the stored state, so it stays as it is: %w", err))
			return
		}
		slog.InfoContext(ctx, "Saved the stored state of building block "+p.Store.BuildingBlock.String()+" to "+path)
	}

	if r.Method == gohttp.MethodPost {
		err = p.Store.Put(ctx, written)
	} else {
		err = p.Store.Delete(ctx)
	}
	if err != nil {
		p.fail(w, r, err)
	}
}

type version struct {
	Lineage string `json:"lineage"`
	Serial  uint64 `json:"serial"`
}

// follows replaces the lock that meshStack's state API does not have. tofu writes a state with the
// lineage of the state it read and a higher serial. A state with another lineage is another state,
// and a state with a serial that is not higher misses a write.
func (next version) follows(previous *version) error {
	if previous == nil {
		return nil
	}
	if next.Lineage != previous.Lineage {
		return fmt.Errorf("the state written has the lineage %s, but the stored state %s, so it is another state", next.Lineage, previous.Lineage)
	}
	if next.Serial <= previous.Serial {
		return fmt.Errorf("the state written has the serial %d, which is not above the serial %d of the stored state, so it misses a write", next.Serial, previous.Serial)
	}
	return nil
}

func versionsOf(written, stored []byte) (next version, previous *version, err error) {
	if err = json.Unmarshal(written, &next); err != nil {
		return next, nil, fmt.Errorf("the state written is no state of tofu: %w", err)
	}
	if next.Lineage == "" {
		return next, nil, errors.New("the state written is no state of tofu, as it has no lineage")
	}
	if stored == nil {
		return next, nil, nil
	}
	previous = new(version)
	if err = json.Unmarshal(stored, previous); err != nil {
		return next, nil, fmt.Errorf("the stored state is no state of tofu: %w", err)
	}
	return next, previous, nil
}

// fail answers 502, which tofu's http backend retries, for every error except a 403 of meshStack,
// which a retry does not fix.
func (p *Proxy) fail(w gohttp.ResponseWriter, r *gohttp.Request, err error) {
	status := gohttp.StatusBadGateway
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsForbidden() {
		status = gohttp.StatusForbidden
	}
	p.refuse(w, r, status, err)
}

func (p *Proxy) refuse(w gohttp.ResponseWriter, r *gohttp.Request, status int, err error) {
	if p.OnProblem != nil {
		p.OnProblem(r.Context(), err)
	}
	gohttp.Error(w, err.Error(), status)
}
