package tfstate

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

const (
	testPassword = "the-password"
	testApiToken = "the-api-token"
	firstState   = `{"version":4,"serial":1,"lineage":"lineage-1"}`
	nextState    = `{"version":4,"serial":2,"lineage":"lineage-1"}`
)

var buildingBlockUuid = uuid.MustParse("b1d2c3e4-0000-4000-8000-000000000001")

type requester struct {
	endpoint *url.URL
}

func (r requester) DoRequest(ctx context.Context, method, path string, opts ...http.RequestOption) ([]byte, error) {
	return http.NewClient(fakemeshstack.UserAgent).WithAuthorization(http.BearerToken(fakemeshstack.Token)).
		DoRequest[[]byte](ctx, method, r.endpoint.JoinPath(path), opts...)
}

type proxyUnderTest struct {
	*Proxy

	meshStack *fakemeshstack.Server
	problems  []error
}

var storedState = fakemeshstack.TfState{Workspace: "my-workspace", BuildingBlockUuid: buildingBlockUuid.String()}

func newProxy(t *testing.T, stored string, writable bool) *proxyUnderTest {
	t.Helper()
	states := map[fakemeshstack.TfState][]byte{}
	if stored != "" {
		states[storedState] = []byte(stored)
	}
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{TfStates: states})
	endpoint, err := url.Parse(meshStack.URL)
	require.NoError(t, err)
	p := &proxyUnderTest{meshStack: meshStack}
	p.Proxy = &Proxy{
		Store:     NewStore(requester{endpoint}, "my-workspace", buildingBlockUuid),
		Writable:  writable,
		Backups:   Backups(filepath.Join(t.TempDir(), "not", "yet", "there")),
		OnProblem: func(_ context.Context, err error) { p.problems = append(p.problems, err) },
		password:  testPassword,
		apiToken:  testApiToken,

		Endpoint: endpoint,
		Api:      http.NewClient(fakemeshstack.UserAgent).WithAuthorization(http.BearerToken(fakemeshstack.Token)),
	}
	return p
}

func (p *proxyUnderTest) requests() (requests []string) {
	for _, request := range p.meshStack.TakeRequests() {
		requests = append(requests, request.Method+" "+request.URL.Path)
	}
	return requests
}

func (p *proxyUnderTest) send(method, body string) *httptest.ResponseRecorder {
	return p.sendTo(method, statePath, body)
}

func (p *proxyUnderTest) sendTo(method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	r.SetBasicAuth(username, testPassword)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func (p *proxyUnderTest) sendToApi(method, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, apiPath+"?page=1", strings.NewReader(`{"some":"body"}`))
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	r.Header.Set("User-Agent", providerUserAgent)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

const (
	apiPath           = "/api/meshobjects/meshworkspaces"
	providerUserAgent = "terraform-provider-meshstack/0.26.0"
)

func (p *proxyUnderTest) backups(t *testing.T) []string {
	t.Helper()
	backups := os.DirFS(string(p.Backups))
	files, err := fs.Glob(backups, "*")
	require.NoError(t, err)
	var contents []string
	for _, file := range files {
		assert.True(t, strings.HasPrefix(file, buildingBlockUuid.String()+"-"), file)
		info, err := fs.Stat(backups, file)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		content, err := fs.ReadFile(backups, file)
		require.NoError(t, err)
		contents = append(contents, string(content))
	}
	return contents
}

func TestAWritableProxy(t *testing.T) {
	p := newProxy(t, "", true)

	t.Run("answers no state with 404, as tofu expects", func(t *testing.T) {
		assert.Equal(t, gohttp.StatusNotFound, p.send(gohttp.MethodGet, "").Code)
		assert.Empty(t, p.problems)
	})

	t.Run("takes only the basic auth it handed out", func(t *testing.T) {
		p.meshStack.TakeRequests()
		for name, authorize := range map[string]func(r *gohttp.Request){
			"no auth":          func(*gohttp.Request) {},
			"another password": func(r *gohttp.Request) { r.SetBasicAuth(username, "guessed") },
			"another user":     func(r *gohttp.Request) { r.SetBasicAuth("admin", testPassword) },
			"a bearer token":   func(r *gohttp.Request) { r.Header.Set("Authorization", "Bearer "+testPassword) },
		} {
			r := httptest.NewRequestWithContext(t.Context(), gohttp.MethodGet, statePath, nil)
			authorize(r)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			assert.Equal(t, gohttp.StatusUnauthorized, w.Code, name)
		}
		assert.Empty(t, p.meshStack.TakeRequests())
		p.problems = nil
	})

	t.Run("stores the first state, and backs up nothing before it", func(t *testing.T) {
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodPost, firstState).Code)
		assert.JSONEq(t, firstState, string(p.meshStack.TfState(storedState)))
		assert.Empty(t, p.backups(t))
		assert.Empty(t, p.problems)
	})

	t.Run("refuses a state that does not follow the stored one", func(t *testing.T) {
		for written, wantProblem := range map[string]string{
			`{"version":4,"serial":2,"lineage":"lineage-2"}`: "the state written has the lineage lineage-2, but the stored state lineage-1, so it is another state",
			firstState: "the state written has the serial 1, which is not above the serial 1 of the stored state, so it misses a write",
			`{"version":4,"serial":0,"lineage":"lineage-1"}`: "the state written has the serial 0, which is not above the serial 1 of the stored state, so it misses a write",
			`{"version":4,"serial":2}`:                       "the state written is no state of tofu, as it has no lineage",
		} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusConflict, p.send(gohttp.MethodPost, written).Code, written)
			assert.Equal(t, []string{wantProblem}, errorTexts(p.problems))
		}
		assert.JSONEq(t, firstState, string(p.meshStack.TfState(storedState)))
		assert.Empty(t, p.backups(t))
	})

	t.Run("refuses a write that BeforeWrite refuses", func(t *testing.T) {
		refused := errors.New("a run is in progress")
		p.BeforeWrite = func(context.Context) error { return refused }
		defer func() { p.BeforeWrite = nil }()
		p.meshStack.TakeRequests()
		for _, method := range []string{gohttp.MethodPost, gohttp.MethodDelete} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusConflict, p.send(method, nextState).Code, method)
			assert.Equal(t, []error{refused}, p.problems, method)
		}
		assert.Empty(t, p.meshStack.TakeRequests())
	})

	t.Run("stores a state of a higher serial, a skipped one included, and backs up the one it replaces", func(t *testing.T) {
		const skipped = `{"version":4,"serial":7,"lineage":"lineage-1"}`
		p.problems = nil
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodPost, nextState).Code)
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodPost, skipped).Code)
		assert.Empty(t, p.problems)

		w := p.send(gohttp.MethodGet, "")
		assert.Equal(t, gohttp.StatusOK, w.Code)
		assert.JSONEq(t, skipped, w.Body.String())
		assert.Equal(t, []string{firstState, nextState}, p.backups(t))
	})

	t.Run("backs up the state it deletes", func(t *testing.T) {
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodDelete, "").Code)
		assert.Nil(t, p.meshStack.TfState(storedState))
		assert.Equal(t, []string{firstState, nextState, `{"version":4,"serial":7,"lineage":"lineage-1"}`}, p.backups(t))
	})

	t.Run("keeps no backup without a directory for them", func(t *testing.T) {
		backups, before := p.Backups, p.backups(t)
		p.Backups = ""
		workingDir := t.TempDir()
		t.Chdir(workingDir)
		p.problems = nil

		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodPost, firstState).Code)
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodPost, nextState).Code)
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodDelete, "").Code)
		assert.Empty(t, p.problems)
		entries, err := os.ReadDir(workingDir)
		require.NoError(t, err)
		assert.Empty(t, entries)
		p.Backups = backups
		assert.Equal(t, before, p.backups(t))
	})

	t.Run("passes a request for the API on with the session's token in place of the one it gave the command", func(t *testing.T) {
		p.meshStack.Route(gohttp.MethodPost+" "+apiPath, func(w gohttp.ResponseWriter, _ *gohttp.Request) { w.WriteHeader(gohttp.StatusCreated) })
		defer p.meshStack.Route(gohttp.MethodPost+" "+apiPath, nil)
		p.meshStack.TakeRequests()

		assert.Equal(t, gohttp.StatusCreated, p.sendToApi(gohttp.MethodPost, "Bearer "+testApiToken).Code)

		forwarded := p.meshStack.TakeRequests()
		require.Len(t, forwarded, 1)
		assert.Equal(t, "Bearer "+fakemeshstack.Token, forwarded[0].Header.Get("Authorization"))
		assert.Equal(t, "page=1", forwarded[0].URL.RawQuery)
		assert.JSONEq(t, `{"some":"body"}`, string(forwarded[0].Body))
		assert.Equal(t, providerUserAgent+" "+fakemeshstack.UserAgent.String(), forwarded[0].Header.Get("User-Agent"),
			"meshStack learns both who built the request and through which front end it came")
		assert.Empty(t, p.problems)
	})

	t.Run("passes a request for the API without a token on without one, as for /mesh/info", func(t *testing.T) {
		p.meshStack.TakeRequests()

		assert.Equal(t, gohttp.StatusUnauthorized, p.sendToApi(gohttp.MethodGet, "").Code, "meshStack refuses it")

		forwarded := p.meshStack.TakeRequests()
		require.Len(t, forwarded, 1)
		assert.Empty(t, forwarded[0].Header.Get("Authorization"))
		assert.Empty(t, p.problems, "the command reports the answer of meshStack itself")
	})

	t.Run("refuses a request for the API with another token, so that no other process works with the session", func(t *testing.T) {
		p.meshStack.TakeRequests()
		for _, authorization := range []string{"Bearer guessed", "Basic " + basicAuth(username, testPassword)} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusUnauthorized, p.sendToApi(gohttp.MethodGet, authorization).Code, authorization)
			assert.Equal(t, []string{"a request for " + apiPath + " came without the MESHSTACK_API_TOKEN it was given"}, errorTexts(p.problems))
		}
		assert.Empty(t, p.meshStack.TakeRequests())
		p.problems = nil
	})

	t.Run("passes on the refusal of meshStack", func(t *testing.T) {
		p.meshStack.Route("/", func(w gohttp.ResponseWriter, _ *gohttp.Request) { w.WriteHeader(gohttp.StatusForbidden) })
		p.problems = nil

		assert.Equal(t, gohttp.StatusForbidden, p.send(gohttp.MethodGet, "").Code)
		require.Len(t, p.problems, 1)
		httpErr, ok := errors.AsType[http.Error](p.problems[0])
		require.True(t, ok, "the problem is the refusal of meshStack: %v", p.problems[0])
		assert.True(t, httpErr.IsForbidden())
	})
}

func TestAReadOnlyProxy(t *testing.T) {
	p := newProxy(t, firstState, false)

	t.Run("serves the stored state of the building block", func(t *testing.T) {
		w := p.send(gohttp.MethodGet, "")

		assert.Equal(t, gohttp.StatusOK, w.Code)
		assert.JSONEq(t, firstState, w.Body.String())
		assert.Equal(t, []string{"GET /api/terraform/state/workspace/my-workspace/buildingBlock/" + buildingBlockUuid.String()}, p.requests())
	})

	t.Run("answers tofu's lock calls itself, so that a plan never makes a run wait", func(t *testing.T) {
		p.meshStack.TakeRequests()
		for _, method := range []string{gohttp.MethodPost, gohttp.MethodDelete} {
			assert.Equal(t, gohttp.StatusOK, p.sendTo(method, lockPath, lockInfo("tofu-lock")).Code, method)
		}
		assert.Empty(t, p.meshStack.TakeRequests())
		assert.Empty(t, p.problems)
	})

	t.Run("passes a read of the API on", func(t *testing.T) {
		p.meshStack.TakeRequests()

		assert.Equal(t, gohttp.StatusOK, p.sendToApi(gohttp.MethodGet, "Bearer "+testApiToken).Code)
		assert.Len(t, p.meshStack.TakeRequests(), 1)
		assert.Empty(t, p.problems)
	})

	t.Run("refuses every other request for the API before it reaches meshStack", func(t *testing.T) {
		p.meshStack.TakeRequests()
		for _, method := range []string{gohttp.MethodPost, gohttp.MethodPut, gohttp.MethodPatch, gohttp.MethodDelete} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusForbidden, p.sendToApi(method, "Bearer "+testApiToken).Code, method)
			require.Len(t, p.problems, 1)
			require.ErrorIs(t, p.problems[0], ErrReadOnlyApi)
		}
		assert.Empty(t, p.meshStack.TakeRequests())
		p.problems = nil
	})

	t.Run("refuses every write before it reaches meshStack", func(t *testing.T) {
		p.meshStack.TakeRequests()
		for _, method := range []string{gohttp.MethodPost, gohttp.MethodDelete} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusForbidden, p.send(method, nextState).Code, method)
			require.Len(t, p.problems, 1)
			require.ErrorIs(t, p.problems[0], ErrReadOnly)
		}
		assert.Empty(t, p.meshStack.TakeRequests())
	})
}

func TestServeCountsTheRequestsUntilTheCommandEnds(t *testing.T) {
	p := newProxy(t, firstState, false)
	var served []byte

	requests, err := p.Serve(t.Context(), func(env []string) {
		vars := map[string]string{}
		for _, variable := range env {
			key, value, _ := strings.Cut(variable, "=")
			vars[key] = value
		}
		address, err := url.Parse(vars["TF_HTTP_ADDRESS"])
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", address.Hostname())
		assert.Equal(t, "http://"+address.Host, vars["MESHSTACK_ENDPOINT"])
		var apiToken jwt.JWT
		require.NoError(t, apiToken.UnmarshalText([]byte(vars["MESHSTACK_API_TOKEN"])), "the provider since v0.26.0 takes only a JWT")
		assert.False(t, apiToken.GetClaim(jwt.ExpiryClaim).Expired(time.Hour))
		authorization := gohttp.Header{}
		authorization.Set("Authorization", "Basic "+basicAuth(vars["TF_HTTP_USERNAME"], vars["TF_HTTP_PASSWORD"]))
		served, err = http.NewClient(fakemeshstack.UserAgent).DoRequest[[]byte](t.Context(), http.MethodGet, address, http.WithHeaders(authorization))
		require.NoError(t, err)
	})

	require.NoError(t, err)
	assert.Equal(t, 1, requests)
	assert.JSONEq(t, firstState, string(served))
	assert.NotEqual(t, testPassword, p.password, "every Serve hands out a password of its own")
}

func errorTexts(errs []error) []string {
	texts := make([]string, 0, len(errs))
	for _, err := range errs {
		texts = append(texts, err.Error())
	}
	return texts
}

func basicAuth(user, password string) string {
	r := httptest.NewRequestWithContext(context.Background(), gohttp.MethodGet, "/", nil)
	r.SetBasicAuth(user, password)
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Basic ")
}

func lockInfo(id string) string {
	return `{"ID":"` + id + `","Operation":"OperationTypeApply","Who":"someone@somewhere"}`
}

// lockedLog takes the warning, which the proxy logs from a timer of its own.
type lockedLog struct {
	mu      sync.Mutex
	written strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.written.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.written.String()
}

// lockOf answers as meshStack's lock endpoint does, with a lock that a test may hold for someone else.
type lockOf struct {
	held []byte
}

func (l *lockOf) serve(w gohttp.ResponseWriter, r *gohttp.Request) {
	body, _ := io.ReadAll(r.Body)
	switch {
	case l.held == nil && r.Method == gohttp.MethodPost:
		l.held = body
	case r.Method == gohttp.MethodPost:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(gohttp.StatusLocked)
		_, _ = w.Write(l.held)
	case r.Method == gohttp.MethodDelete:
		l.held = nil
	}
}

func TestAWritableProxyPassesTheLockOn(t *testing.T) {
	p := newProxy(t, firstState, true)
	lock := &lockOf{}
	p.meshStack.Route("/api/terraform/state/workspace/my-workspace/buildingBlock/"+buildingBlockUuid.String()+"/lock", lock.serve)
	var log lockedLog
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))

	t.Run("takes the lock in meshStack", func(t *testing.T) {
		assert.Equal(t, gohttp.StatusOK, p.sendTo(gohttp.MethodPost, lockPath, lockInfo("mine")).Code)
		assert.JSONEq(t, lockInfo("mine"), string(lock.held))
	})

	t.Run("answers a lock that someone else holds with 423 and the lock info of the holder, as tofu expects", func(t *testing.T) {
		lock.held = []byte(lockInfo("theirs"))
		defer func() { lock.held = []byte(lockInfo("mine")) }()

		w := p.sendTo(gohttp.MethodPost, lockPath, lockInfo("mine"))

		assert.Equal(t, gohttp.StatusLocked, w.Code)
		assert.JSONEq(t, lockInfo("theirs"), w.Body.String())
		assert.Empty(t, p.problems, "tofu waits for the lock and reports it itself")
	})

	t.Run("stores the state with the ID of tofu's lock", func(t *testing.T) {
		p.meshStack.TakeRequests()

		assert.Equal(t, gohttp.StatusOK, p.sendTo(gohttp.MethodPost, statePath+"?ID=mine", nextState).Code)

		stored := p.meshStack.TakeRequests()
		require.NotEmpty(t, stored)
		put := stored[len(stored)-1]
		assert.Equal(t, gohttp.MethodPost, put.Method)
		assert.Equal(t, "mine", put.URL.Query().Get("ID"))
	})

	t.Run("passes a write that meshStack refuses for another's lock back as 423, and logs the holder rather than a problem", func(t *testing.T) {
		writeInMeshStack := "POST /api/terraform/state/workspace/my-workspace/buildingBlock/" + buildingBlockUuid.String()
		p.meshStack.Route(writeInMeshStack, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(gohttp.StatusLocked)
			_, _ = w.Write([]byte(lockInfo("theirs")))
		})
		defer p.meshStack.Route(writeInMeshStack, nil)

		w := p.sendTo(gohttp.MethodPost, statePath+"?ID=stale", `{"version":4,"serial":3,"lineage":"lineage-1"}`)

		assert.Equal(t, gohttp.StatusLocked, w.Code)
		assert.JSONEq(t, lockInfo("theirs"), w.Body.String())
		assert.Empty(t, p.problems, "the client reports the refusal itself")
		assert.Contains(t, log.String(), `as \"someone@somewhere\" holds the lock theirs`)
	})

	t.Run("with Force, stores a state that does not follow the stored one", func(t *testing.T) {
		p.Force = true
		defer func() { p.Force = false }()
		const pushed = `{"version":4,"serial":1,"lineage":"lineage-2"}`

		assert.Equal(t, gohttp.StatusOK, p.sendTo(gohttp.MethodPost, statePath+"?ID=mine", pushed).Code)
		assert.JSONEq(t, pushed, string(p.meshStack.TfState(storedState)))
		assert.Empty(t, p.problems)
	})

	t.Run("passes a 5xx of meshStack on for tofu to retry, and logs it rather than a problem", func(t *testing.T) {
		lockInMeshStack := "POST /api/terraform/state/workspace/my-workspace/buildingBlock/" + buildingBlockUuid.String() + "/lock"
		p.meshStack.Route(lockInMeshStack, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(gohttp.StatusInternalServerError)
		})
		defer p.meshStack.Route(lockInMeshStack, nil)

		assert.Equal(t, gohttp.StatusInternalServerError, p.sendTo(gohttp.MethodPost, lockPath, lockInfo("mine")).Code)
		assert.Empty(t, p.problems, "tofu fails the command itself where its retries fail as well")
		assert.Contains(t, log.String(), "cannot lock the state of building block "+buildingBlockUuid.String())
	})

	t.Run("releases the lock in meshStack", func(t *testing.T) {
		assert.Equal(t, gohttp.StatusOK, p.sendTo(gohttp.MethodDelete, lockPath, lockInfo("mine")).Code)
		assert.Nil(t, lock.held)
	})

	t.Run("warns where tofu holds the lock longer than LockWarning, and not after it released it", func(t *testing.T) {
		p.LockWarning = 20 * time.Millisecond
		defer func() { p.LockWarning = 0 }()
		const warning = "every run of the building block waits until it is released"

		p.sendTo(gohttp.MethodPost, lockPath, lockInfo("released"))
		p.sendTo(gohttp.MethodDelete, lockPath, lockInfo("released"))
		p.sendTo(gohttp.MethodPost, lockPath, lockInfo("held"))
		defer p.sendTo(gohttp.MethodDelete, lockPath, lockInfo("held"))

		assert.Eventually(t, func() bool { return strings.Contains(log.String(), warning) }, time.Second, 5*time.Millisecond)
		assert.Equal(t, 1, strings.Count(log.String(), warning), "the lock released first warns as well")
	})
}
