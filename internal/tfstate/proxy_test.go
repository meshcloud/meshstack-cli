package tfstate

import (
	"context"
	"errors"
	"io"
	"io/fs"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

const (
	testPassword = "the-password"
	firstState   = `{"version":4,"serial":1,"lineage":"lineage-1"}`
	nextState    = `{"version":4,"serial":2,"lineage":"lineage-1"}`
)

var buildingBlockUuid = uuid.MustParse("b1d2c3e4-0000-4000-8000-000000000001")

type fakeMeshStack struct {
	state     []byte
	forbidden bool
	requests  []string
}

func (f *fakeMeshStack) ServeHTTP(w gohttp.ResponseWriter, r *gohttp.Request) {
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	switch {
	case f.forbidden:
		w.WriteHeader(gohttp.StatusForbidden)
	case r.Method == gohttp.MethodPost:
		f.state, _ = io.ReadAll(r.Body)
	case r.Method == gohttp.MethodDelete:
		f.state = nil
	case f.state == nil:
		w.WriteHeader(gohttp.StatusNotFound)
	default:
		_, _ = w.Write(f.state)
	}
}

type requester struct {
	endpoint *url.URL
}

func (r requester) DoRequest(ctx context.Context, method, path string, opts ...http.RequestOption) ([]byte, error) {
	return http.NewClient("").DoRequest[[]byte](ctx, method, r.endpoint.JoinPath(path), opts...)
}

type proxyUnderTest struct {
	*Proxy

	meshStack *fakeMeshStack
	problems  []error
}

func newProxy(t *testing.T, stored string, writable bool) *proxyUnderTest {
	t.Helper()
	meshStack := &fakeMeshStack{}
	if stored != "" {
		meshStack.state = []byte(stored)
	}
	server := httptest.NewServer(meshStack)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	p := &proxyUnderTest{meshStack: meshStack}
	p.Proxy = &Proxy{
		Store:     NewStore(requester{endpoint}, "my-workspace", buildingBlockUuid),
		Writable:  writable,
		Backups:   Backups(filepath.Join(t.TempDir(), "tfstate-backups")),
		OnProblem: func(_ context.Context, err error) { p.problems = append(p.problems, err) },
		password:  testPassword,
	}
	return p
}

func (p *proxyUnderTest) send(method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, statePath, strings.NewReader(body))
	r.SetBasicAuth(username, testPassword)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

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
		p.meshStack.requests = nil
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
		assert.Empty(t, p.meshStack.requests)
		p.problems = nil
	})

	t.Run("stores the first state, and backs up nothing before it", func(t *testing.T) {
		assert.Equal(t, gohttp.StatusOK, p.send(gohttp.MethodPost, firstState).Code)
		assert.JSONEq(t, firstState, string(p.meshStack.state))
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
		assert.JSONEq(t, firstState, string(p.meshStack.state))
		assert.Empty(t, p.backups(t))
	})

	t.Run("refuses a write that BeforeWrite refuses", func(t *testing.T) {
		refused := errors.New("a run is in progress")
		p.BeforeWrite = func(context.Context) error { return refused }
		defer func() { p.BeforeWrite = nil }()
		p.meshStack.requests = nil
		for _, method := range []string{gohttp.MethodPost, gohttp.MethodDelete} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusConflict, p.send(method, nextState).Code, method)
			assert.Equal(t, []error{refused}, p.problems, method)
		}
		assert.Empty(t, p.meshStack.requests)
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
		assert.Nil(t, p.meshStack.state)
		assert.Equal(t, []string{firstState, nextState, `{"version":4,"serial":7,"lineage":"lineage-1"}`}, p.backups(t))
	})

	t.Run("passes on the refusal of meshStack", func(t *testing.T) {
		p.meshStack.forbidden = true
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
		assert.Equal(t, []string{"GET /api/terraform/state/workspace/my-workspace/buildingBlock/" + buildingBlockUuid.String()}, p.meshStack.requests)
	})

	t.Run("refuses every write before it reaches meshStack", func(t *testing.T) {
		p.meshStack.requests = nil
		for _, method := range []string{gohttp.MethodPost, gohttp.MethodDelete} {
			p.problems = nil
			assert.Equal(t, gohttp.StatusForbidden, p.send(method, nextState).Code, method)
			require.Len(t, p.problems, 1)
			require.ErrorIs(t, p.problems[0], ErrReadOnly)
		}
		assert.Empty(t, p.meshStack.requests)
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
		authorization := gohttp.Header{}
		authorization.Set("Authorization", "Basic "+basicAuth(vars["TF_HTTP_USERNAME"], vars["TF_HTTP_PASSWORD"]))
		served, err = http.NewClient("").DoRequest[[]byte](t.Context(), http.MethodGet, address, http.WithHeaders(authorization))
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
