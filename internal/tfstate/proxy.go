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
	lockPath  = statePath + "/lock"
)

var ErrReadOnly = errors.New("the state is read-only")

// Proxy stands between tofu and meshStack, rather than giving tofu the session's token as
// TF_HTTP_PASSWORD, which meshfed's TfStateRunTokenBasicAuthFilter accepts. That token can expire
// in the middle of an apply, and nothing would check tofu's writes against the stored state.
type Proxy struct {
	Store Store
	// Writable passes tofu's lock calls on to meshStack. A proxy that is not writable answers them
	// itself, so that a command that stores nothing never makes a run of the building block wait.
	Writable    bool
	BeforeWrite func(ctx context.Context) error
	// Force stores a state that does not follow the stored one, such as the one of a
	// tofu state push -force.
	Force   bool
	Backups Backups
	// LockWarning is how long tofu may hold meshStack's lock before the proxy warns that the runs of
	// the building block wait for it. Zero warns never.
	LockWarning time.Duration
	// OnProblem gets the error of each request that the proxy refused, because tofu prints only the
	// status code of the response. A 5xx of meshStack, which tofu retries, is logged as a warning
	// instead.
	OnProblem func(ctx context.Context, err error)

	password string
	// mu is held for the whole request, so that no other write comes between a write's check of the
	// stored state and the write itself.
	mu        sync.Mutex
	requests  int
	lockTimer *time.Timer
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

	address := "http://" + listener.Addr().String()
	run([]string{
		"TF_HTTP_ADDRESS=" + address + statePath,
		"TF_HTTP_LOCK_ADDRESS=" + address + lockPath,
		"TF_HTTP_LOCK_METHOD=" + gohttp.MethodPost,
		"TF_HTTP_UNLOCK_ADDRESS=" + address + lockPath,
		"TF_HTTP_UNLOCK_METHOD=" + gohttp.MethodDelete,
		"TF_HTTP_USERNAME=" + username,
		"TF_HTTP_PASSWORD=" + p.password,
	})

	err = server.Shutdown(context.WithoutCancel(ctx))
	if serveErr := <-served; !errors.Is(serveErr, gohttp.ErrServerClosed) {
		err = errors.Join(err, serveErr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLockTimer()
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
	switch {
	case r.URL.Path == statePath && r.Method == gohttp.MethodGet:
		p.get(w, r)
	case r.URL.Path == statePath && (r.Method == gohttp.MethodPost || r.Method == gohttp.MethodDelete):
		p.write(w, r)
	case r.URL.Path == lockPath && (r.Method == gohttp.MethodPost || r.Method == gohttp.MethodDelete):
		p.lock(w, r)
	case r.URL.Path == statePath || r.URL.Path == lockPath:
		w.Header().Set("Allow", map[string]string{statePath: "GET, POST, DELETE", lockPath: "POST, DELETE"}[r.URL.Path])
		p.refuse(w, r, gohttp.StatusMethodNotAllowed, fmt.Errorf("a request asked to %s %s, which meshStack does not offer", r.Method, r.URL.Path))
	default:
		p.refuse(w, r, gohttp.StatusNotFound, fmt.Errorf("a request asked for %s, but the state is at %s", r.URL.Path, statePath))
	}
}

func (p *Proxy) lock(w gohttp.ResponseWriter, r *gohttp.Request) {
	if !p.Writable {
		return
	}
	ctx := r.Context()
	info, err := io.ReadAll(r.Body)
	if err != nil {
		p.refuse(w, r, gohttp.StatusBadRequest, err)
		return
	}
	if r.Method == gohttp.MethodDelete {
		if err = p.Store.Unlock(ctx, info); err != nil {
			p.failOrPassLock(w, r, err, "release its lock")
			return
		}
		p.stopLockTimer()
		return
	}

	err = p.Store.Lock(ctx, info)
	// tofu prints the holder of a 423 itself, and tries again until its -lock-timeout has passed.
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsLocked() {
		passLock(w, httpErr)
		return
	} else if err != nil {
		p.fail(w, r, err)
		return
	}
	if p.LockWarning > 0 {
		p.stopLockTimer()
		warnCtx := context.WithoutCancel(ctx)
		p.lockTimer = time.AfterFunc(p.LockWarning, func() {
			slog.WarnContext(warnCtx, fmt.Sprintf("The command has held the lock on the state of building block %s for %s, and every run of the building block waits until it is released",
				p.Store.BuildingBlock, p.LockWarning))
		})
	}
}

func (p *Proxy) stopLockTimer() {
	if p.lockTimer != nil {
		p.lockTimer.Stop()
		p.lockTimer = nil
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
	if r.Method == gohttp.MethodPost && !p.Force {
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

	lockId := r.URL.Query().Get("ID")
	if r.Method == gohttp.MethodPost {
		err = p.Store.Put(ctx, written, lockId)
	} else {
		err = p.Store.Delete(ctx, lockId)
	}
	if err != nil {
		p.failOrPassLock(w, r, err, "write")
	}
}

// failOrPassLock passes a 423 back to the client as it came, which reports the refusal itself, so
// it is no problem of the proxy. tofu prints only the status code of it, so the holder goes to the log.
func (p *Proxy) failOrPassLock(w gohttp.ResponseWriter, r *gohttp.Request, err error, action string) {
	httpErr, ok := errors.AsType[http.Error](err)
	if !ok || !httpErr.IsLocked() {
		p.fail(w, r, err)
		return
	}
	var holder LockInfo
	_ = json.Unmarshal(httpErr.ResponseBody, &holder)
	slog.WarnContext(r.Context(), fmt.Sprintf("meshStack refused to %s the state of building block %s, as %q holds the lock %s on it",
		action, p.Store.BuildingBlock, holder.Who, holder.ID))
	passLock(w, httpErr)
}

// passLock answers with the lock info of the holder, the 423 that tofu's http backend expects.
func passLock(w gohttp.ResponseWriter, locked http.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(gohttp.StatusLocked)
	_, _ = w.Write(locked.ResponseBody)
}

type version struct {
	Lineage string `json:"lineage"`
	Serial  uint64 `json:"serial"`
}

// follows catches a state that tofu did not read from the stored one, such as an old copy that
// tofu state push sends. tofu writes a state with the lineage of the state it read and a higher
// serial. A state with another lineage is another state, and a state with a serial that is not
// higher misses a write.
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

// fail answers with the status of meshStack, or 502 for an error that has none. tofu's http backend
// retries a 5xx, and fails the command where the retries fail as well, so a 5xx is no problem of
// the proxy: a meshStack that failed once must not fail a tofu run that succeeded.
func (p *Proxy) fail(w gohttp.ResponseWriter, r *gohttp.Request, err error) {
	status := gohttp.StatusBadGateway
	if httpErr, ok := errors.AsType[http.Error](err); ok {
		status = httpErr.StatusCode
	}
	if status < gohttp.StatusInternalServerError {
		p.refuse(w, r, status, err)
		return
	}
	slog.WarnContext(r.Context(), err.Error())
	gohttp.Error(w, err.Error(), status)
}

func (p *Proxy) refuse(w gohttp.ResponseWriter, r *gohttp.Request, status int, err error) {
	if p.OnProblem != nil {
		p.OnProblem(r.Context(), err)
	}
	gohttp.Error(w, err.Error(), status)
}
