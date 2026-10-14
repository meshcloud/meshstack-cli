// Package fakemeshstack stands in for one meshStack: its API, its Keycloak and its API docs.
//
// Its main consumer is the demo in docs/demo, which records the CLI against it. Tests use it
// rarely, as the rule "Test against a live meshStack" in [AGENTS.md] says.
//
// It serves only what its callers need. A test that needs another answer sets it with
// [Server.Route], so that this package does not grow into a meshStack simulator.
//
// [AGENTS.md]: https://github.com/meshcloud/meshstack-cli/blob/main/AGENTS.md#always-on-rules
package fakemeshstack

import (
	"bytes"
	"cmp"
	"encoding/json/v2"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

// DefaultVersion is at least client.MinMeshStackVersion, or the CLI refuses the backend.
const DefaultVersion = "2026.39.0"

// Token is an unsigned JWT that expires in 2100, so the CLI sends it rather than asking for a new
// one: a token without an expiry counts as expired. Every Server honors it.
const Token = "eyJhbGciOiJub25lIn0.eyJleHAiOjQxMDI0NDQ4MDB9." //nolint:gosec // G101: unsigned, it authorizes nothing but a fake meshStack

// UserAgent is for the client of a test, which no front end builds.
var UserAgent = http.UserAgent{GitHubRepo: "meshcloud/fakemeshstack", Version: "test"}

const (
	ApiDocsPath     = "/api/meshstack-openapi-docs.json"
	defaultPageSize = 50
)

type Options struct {
	// Version is what /mesh/info reports, DefaultVersion if empty.
	Version        string
	AdminWorkspace string
	// Issuer makes the Server the Keycloak of meshStack as well, under the path of the issuer. A
	// caller that serves the issuer's host elsewhere routes that host to the Server too.
	Issuer  string
	ApiKeys []ApiKey

	// The objects are marshaled on every request, so a test may change one between steps. A
	// workspace is found by metadata.name, every other object by metadata.uuid.
	Workspaces        []any
	BuildingBlocks    []any
	BuildingBlockRuns []any
	EventLogs         []any
	// PageSize is the largest page a list serves, defaultPageSize if 0, as meshStack caps the
	// size a client asks for.
	PageSize int

	TfStates map[TfState][]byte
	// ApiDocs is served at ApiDocsPath as last modified when New was called, so a conditional GET
	// gets a 304.
	ApiDocs []byte
}

type ApiKey struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

type TfState struct {
	Workspace, BuildingBlockUuid string
}

type Request struct {
	Method string
	Host   string
	URL    *url.URL
	Header gohttp.Header
	Body   []byte
}

type Counts struct {
	Logins        int64
	Authorized    int64
	RevokedTokens int64
	// UnknownTokens counts the 401s for a token the Server never minted, or for no token at all.
	UnknownTokens int64
}

type Server struct {
	// URL is set by Start, and empty for a Server that New made.
	URL string

	options  Options
	routes   *gohttp.ServeMux
	identity identityProvider

	mu        sync.Mutex
	overrides map[string]gohttp.HandlerFunc
	overrider *gohttp.ServeMux
	requests  []Request
	minted    map[string]bool
	mintOrder []string
	counts    Counts
}

func New(options Options) *Server {
	options.Version = cmp.Or(options.Version, DefaultVersion)
	options.PageSize = cmp.Or(options.PageSize, defaultPageSize)
	if options.TfStates == nil {
		options.TfStates = map[TfState][]byte{}
	}
	s := &Server{
		options:   options,
		routes:    gohttp.NewServeMux(),
		identity:  identityProvider{codes: map[string]authorization{}},
		overrides: map[string]gohttp.HandlerFunc{},
		overrider: gohttp.NewServeMux(),
		minted:    map[string]bool{Token: true},
	}
	s.routes.HandleFunc("GET /mesh/info", s.meshInfo)
	s.routes.HandleFunc("POST /api/login", s.login)
	for _, kind := range []struct {
		path, embedded, id string
		objects            *[]any
	}{
		{"meshworkspaces", "meshWorkspaces", "name", &s.options.Workspaces},
		{"meshbuildingblocks", "meshBuildingBlocks", "uuid", &s.options.BuildingBlocks},
		{"meshbuildingblockruns", "meshBuildingBlockRuns", "uuid", &s.options.BuildingBlockRuns},
		{"mesheventlogs", "meshEventLogs", "uuid", &s.options.EventLogs},
	} {
		s.routes.HandleFunc("GET /api/meshobjects/"+kind.path, s.authorized(func(w gohttp.ResponseWriter, r *gohttp.Request) {
			s.list(w, r, kind.embedded, *kind.objects)
		}))
		s.routes.HandleFunc("GET /api/meshobjects/"+kind.path+"/{id}", s.authorized(func(w gohttp.ResponseWriter, r *gohttp.Request) {
			s.show(w, r, kind.id, *kind.objects)
		}))
	}
	s.routes.HandleFunc("/api/terraform/state/workspace/{workspace}/buildingBlock/{uuid}", s.authorized(s.tfState))
	if options.ApiDocs != nil {
		modified := time.Now()
		s.routes.HandleFunc("GET "+ApiDocsPath, func(w gohttp.ResponseWriter, r *gohttp.Request) {
			w.Header().Set("Content-Type", "application/json")
			gohttp.ServeContent(w, r, "", modified, bytes.NewReader(options.ApiDocs))
		})
	}
	if options.Issuer != "" {
		s.registerKeycloak()
	}
	return s
}

func Start(t *testing.T, options Options) *Server {
	t.Helper()
	s := New(options)
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

// Route answers the requests that match pattern with handler, whatever token they carry, ahead
// of the Server's own answers. It replaces an earlier handler for the same pattern, and a nil
// handler removes it.
func (s *Server) Route(pattern string, handler gohttp.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if handler == nil {
		delete(s.overrides, pattern)
	} else {
		s.overrides[pattern] = handler
	}
	// A ServeMux panics on a pattern registered twice, so replacing a handler takes a new one.
	s.overrider = gohttp.NewServeMux()
	for pattern, handler := range s.overrides {
		s.overrider.HandleFunc(pattern, handler)
	}
}

// TakeRequests returns the requests since the last call.
func (s *Server) TakeRequests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := s.requests
	s.requests = nil
	return taken
}

func (s *Server) Counts() Counts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts
}

func (s *Server) TfState(key TfState) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.options.TfStates[key]
}

func (s *Server) MintToken(validFor time.Duration) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The jti tells two tokens minted in the same second apart.
	claims, _ := json.Marshal(map[string]any{"exp": time.Now().Add(validFor).Unix(), "jti": strconv.Itoa(len(s.mintOrder) + 1)})
	token := "e30." + base64Url(claims) + ".test-signature"
	s.honor(token)
	return token
}

// RevokeNewestToken revokes the newest token still honored, as that is the one a cache most
// likely holds, so revoking it makes a caller refresh.
//
// No request may be in flight while this runs. An authorized client retries a 401 once, and the
// token it retries with can be one it took from the cache of another process, so revoking during
// a request can leave a 401 that nothing recovers from.
func (s *Server) RevokeNewestToken() (revoked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, token := range slices.Backward(s.mintOrder) {
		if s.minted[token] {
			s.minted[token] = false
			return true
		}
	}
	return false
}

func (s *Server) ServeHTTP(w gohttp.ResponseWriter, r *gohttp.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Host: r.Host, URL: r.URL, Header: r.Header.Clone(), Body: body})
	overrider := s.overrider
	s.mu.Unlock()
	if handler, pattern := overrider.Handler(r); pattern != "" {
		handler.ServeHTTP(w, r)
		return
	}
	s.routes.ServeHTTP(w, r)
}

func (s *Server) honor(token string) {
	s.minted[token] = true
	s.mintOrder = append(s.mintOrder, token)
}

func (s *Server) authorized(next gohttp.HandlerFunc) gohttp.HandlerFunc {
	return func(w gohttp.ResponseWriter, r *gohttp.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		honored, minted := s.minted[token]
		switch {
		case honored:
			s.counts.Authorized++
		case minted:
			s.counts.RevokedTokens++
		default:
			s.counts.UnknownTokens++
		}
		s.mu.Unlock()
		if !honored {
			gohttp.Error(w, "the token is revoked, or was never minted", gohttp.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) meshInfo(w gohttp.ResponseWriter, _ *gohttp.Request) {
	info := map[string]any{"version": s.options.Version, "adminWorkspaceIdentifier": s.options.AdminWorkspace, "metadata": map[string]string{}}
	if s.options.Issuer != "" {
		info["issuer"], info["cliClientId"] = s.options.Issuer, cliClientId
	}
	writeJson(w, gohttp.StatusOK, info)
}

func (s *Server) login(w gohttp.ResponseWriter, r *gohttp.Request) {
	var offered ApiKey
	if err := json.UnmarshalRead(r.Body, &offered); err != nil {
		gohttp.Error(w, err.Error(), gohttp.StatusBadRequest)
		return
	}
	if !slices.Contains(s.options.ApiKeys, offered) {
		gohttp.Error(w, "no such API key", gohttp.StatusUnauthorized)
		return
	}
	token := s.MintToken(time.Hour)
	s.mu.Lock()
	s.counts.Logins++
	s.mu.Unlock()
	writeJson(w, gohttp.StatusOK, map[string]string{"access_token": token})
}

func (s *Server) list(w gohttp.ResponseWriter, r *gohttp.Request, embedded string, objects []any) {
	size, err := strconv.Atoi(r.URL.Query().Get("size"))
	if err != nil || size < 1 || size > s.options.PageSize {
		size = s.options.PageSize
	}
	number, _ := strconv.Atoi(r.URL.Query().Get("page"))
	start := min(number*size, len(objects))
	writeJson(w, gohttp.StatusOK, map[string]any{
		"_embedded": map[string]any{embedded: objects[start:min(start+size, len(objects))]},
		"page": map[string]int{
			"size": size, "totalElements": len(objects), "totalPages": (len(objects) + size - 1) / size, "number": number,
		},
	})
}

func (s *Server) show(w gohttp.ResponseWriter, r *gohttp.Request, idField string, objects []any) {
	for _, object := range objects {
		content, err := json.Marshal(object)
		if err != nil {
			gohttp.Error(w, err.Error(), gohttp.StatusInternalServerError)
			return
		}
		var identified struct {
			Metadata map[string]any `json:"metadata"`
		}
		if json.Unmarshal(content, &identified) == nil && identified.Metadata[idField] == r.PathValue("id") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(content)
			return
		}
	}
	gohttp.NotFound(w, r)
}

func (s *Server) tfState(w gohttp.ResponseWriter, r *gohttp.Request) {
	key := TfState{Workspace: r.PathValue("workspace"), BuildingBlockUuid: r.PathValue("uuid")}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case gohttp.MethodGet:
		state, found := s.options.TfStates[key]
		if !found {
			gohttp.NotFound(w, r)
			return
		}
		_, _ = w.Write(state)
	case gohttp.MethodPost:
		s.options.TfStates[key], _ = io.ReadAll(r.Body)
	case gohttp.MethodDelete:
		delete(s.options.TfStates, key)
	default:
		w.WriteHeader(gohttp.StatusMethodNotAllowed)
	}
}

func writeJson(w gohttp.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Sorted, as meshStack writes _embedded before page.
	_ = json.MarshalWrite(w, body, json.Deterministic(true))
}
