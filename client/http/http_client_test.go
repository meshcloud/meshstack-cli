package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shopify/toxiproxy/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/logs"
)

var testUserAgent = UserAgent{GitHubRepo: "meshcloud/meshstack-cli", Version: "test"}

func TestMain(m *testing.M) {
	stallTimeout = 5 * time.Second
	goClient = newGoClient()
	m.Run()
}

func TestDoRequest(t *testing.T) {
	var answer struct {
		status int
		body   string
	}
	var received []string
	server := newTestServer(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, _ := io.ReadAll(r.Body)
		received = append(received, r.Method+" "+r.Header.Get("User-Agent")+" "+string(body))
		w.Header()["Date"] = nil
		w.WriteHeader(answer.status)
		_, _ = io.WriteString(w, answer.body)
	})
	client := NewClient(testUserAgent)

	t.Run("sends the user agent and parses the answer", func(t *testing.T) {
		answer.status, answer.body = gohttp.StatusOK, `{"name":"answered"}`

		got, err := UnmarshalJsonBody[map[string]string](client.DoRequest(t.Context(), MethodPost, server.Url,
			WithJsonRequest(map[string]string{"name": "sent"}, "")))

		require.NoError(t, err)
		assert.Equal(t, map[string]string{"name": "answered"}, got)
		assert.Equal(t, []string{`POST meshstack-cli/test {"name":"sent"}`}, received)
	})

	t.Run("a non-2xx is an Error with the whole body", func(t *testing.T) {
		answer.status, answer.body = gohttp.StatusNotFound, strings.Repeat("x", 2*loggedBodyLimit)

		err := IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url))

		httpErr, ok := errors.AsType[Error](err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, Error{Method: MethodGet, Url: server.Url.String(), StatusCode: gohttp.StatusNotFound, ResponseBody: []byte(answer.body)}, httpErr)
	})

	t.Run("WithoutRedirects answers with the 3xx", func(t *testing.T) {
		answer.status, answer.body = gohttp.StatusFound, ""

		err := IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url, WithoutRedirects()))

		httpErr, ok := errors.AsType[Error](err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, gohttp.StatusFound, httpErr.StatusCode)
	})
}

func TestRetry(t *testing.T) {
	var attempts atomic.Int32
	var answers []int
	var bodies []string
	server := newTestServer(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		status := answers[min(int(attempts.Add(1))-1, len(answers)-1)]
		w.WriteHeader(status)
	})
	reset := func(statuses ...int) *testBackoff {
		attempts.Store(0)
		answers, bodies = statuses, nil
		return &testBackoff{}
	}
	client := NewClient(testUserAgent)

	t.Run("a GET outlasts a busy or failing gateway", func(t *testing.T) {
		for _, status := range []int{429, 502, 503, 504} {
			captured := logs.Capture(t)
			backoff := reset(status, gohttp.StatusOK)

			err := IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url, backoff.option()))

			require.NoError(t, err, status)
			assert.Equal(t, int32(2), attempts.Load(), status)
			assert.Equal(t, []string{
				fmt.Sprintf(`level=WARN msg="retrying request" status=%d method=GET path=/ attempt=1/2 waitTime=0s`, status),
			}, captured.Lines(slog.LevelWarn))
		}
	})

	t.Run("gives up after the last retry with the last answer", func(t *testing.T) {
		backoff := reset(gohttp.StatusBadGateway)

		err := IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url, backoff.option()))

		httpErr, ok := errors.AsType[Error](err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, gohttp.StatusBadGateway, httpErr.StatusCode)
		assert.Equal(t, int32(3), attempts.Load())
	})

	t.Run("another method than GET is replayed after a gateway error only when Retryable, body included", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plan")
		require.NoError(t, os.WriteFile(path, []byte("a plan"), 0o600))
		for name, payload := range map[string]RequestOption{
			`{"key":"value"}`: WithJsonRequest(map[string]string{"key": "value"}, ""),
			"a plan":          WithFileRequest(path, ""),
		} {
			backoff := reset(gohttp.StatusBadGateway, gohttp.StatusNoContent)
			err := IgnoreResponse(client.DoRequest(t.Context(), MethodPut, server.Url, payload, backoff.option()))
			require.Error(t, err)
			assert.Equal(t, []string{name}, bodies, "the server may have acted on a PUT it answered with 502, so replaying it needs the caller's word")

			backoff = reset(gohttp.StatusBadGateway, gohttp.StatusNoContent)
			err = IgnoreResponse(client.DoRequest(t.Context(), MethodPut, server.Url, payload, backoff.option(), Retryable(RetryOnTransient)))
			require.NoError(t, err)
			assert.Equal(t, []string{name, name}, bodies)
		}
	})

	t.Run("another method than GET is replayed by default after a status that says the server did not act", func(t *testing.T) {
		for _, status := range []int{gohttp.StatusServiceUnavailable, gohttp.StatusLocked} {
			backoff := reset(status, gohttp.StatusNoContent)
			err := IgnoreResponse(client.DoRequest(t.Context(), MethodPatch, server.Url, WithJsonRequest(map[string]string{"key": "value"}, ""), backoff.option()))
			require.NoError(t, err)
			assert.Len(t, bodies, 2)
		}
	})

	t.Run("a reset connection is retried", func(t *testing.T) {
		backoff := reset(gohttp.StatusOK)
		server.addToxic(t, "reset_peer", "downstream", map[string]any{"timeout": 0})
		backoff.onRetry = func() { server.removeToxics(t) }

		err := IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url, backoff.option()))

		require.NoError(t, err)
		assert.Equal(t, int32(1), backoff.calls.Load())
	})

	t.Run("a refused connection is retried for any method", func(t *testing.T) {
		backoff := reset(gohttp.StatusOK)
		listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		address := listener.Addr().String()
		require.NoError(t, listener.Close())
		backoff.onRetry = func() {
			started := httptest.NewUnstartedServer(gohttp.NotFoundHandler())
			require.NoError(t, started.Listener.Close())
			started.Listener, err = new(net.ListenConfig).Listen(t.Context(), "tcp", address)
			require.NoError(t, err)
			started.Start()
			t.Cleanup(started.Close)
		}

		err = IgnoreResponse(client.DoRequest(t.Context(), MethodPost, &url.URL{Scheme: "http", Host: address},
			WithJsonRequest(map[string]string{"key": "value"}, ""), backoff.option()))

		httpErr, ok := errors.AsType[Error](err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, gohttp.StatusNotFound, httpErr.StatusCode)
		assert.Equal(t, int32(1), backoff.calls.Load())
	})

	t.Run("a server that does not answer is given up after stallTimeout, and not retried", func(t *testing.T) {
		backoff := reset(gohttp.StatusOK)
		server.addToxic(t, "timeout", "upstream", map[string]any{"timeout": 0})
		t.Cleanup(func() { server.removeToxics(t) })
		start := time.Now()

		err := IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url, backoff.option()))

		require.ErrorContains(t, err, "timeout awaiting response headers")
		assert.GreaterOrEqual(t, time.Since(start), stallTimeout)
		assert.Zero(t, backoff.calls.Load())
	})

	t.Run("stops once the context is done", func(t *testing.T) {
		backoff := reset(gohttp.StatusBadGateway)
		ctx, cancel := context.WithCancel(t.Context())
		backoff.onRetry = cancel

		err := IgnoreResponse(client.DoRequest(ctx, MethodGet, server.Url, backoff.option()))

		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int32(1), backoff.calls.Load())
	})
}

func TestStallGuard(t *testing.T) {
	readBody := func(t *testing.T, server testServer) (string, time.Duration, error) {
		t.Helper()
		start := time.Now()
		response, err := NewClient(testUserAgent).DoRequest(t.Context(), MethodGet, server.Url)
		require.NoError(t, err)
		body, err := io.ReadAll(response)
		_ = response.Close()
		return string(body), time.Since(start), err
	}

	t.Run("a body that stops arriving ends after stallTimeout", func(t *testing.T) {
		server := newTestServer(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
			_, _ = io.WriteString(w, "partial")
			_ = gohttp.NewResponseController(w).Flush()
			<-r.Context().Done()
		})

		body, took, err := readBody(t, server)

		require.ErrorContains(t, err, "response body stalled: nothing received for 5s")
		assert.Equal(t, "partial", body)
		assert.GreaterOrEqual(t, took, stallTimeout)
	})

	t.Run("a body that takes longer than stallTimeout but keeps arriving is read to the end", func(t *testing.T) {
		answer := strings.Repeat(".", 8*1000)
		server := newTestServer(t, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			_, _ = io.WriteString(w, answer)
		})
		server.addToxic(t, "bandwidth", "downstream", map[string]any{"rate": 1})

		body, took, err := readBody(t, server)

		require.NoError(t, err)
		assert.Equal(t, answer, body)
		assert.Greater(t, took, stallTimeout)
	})
}

func TestAuthorizedClient(t *testing.T) {
	var seen []string
	server := newTestServer(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(gohttp.StatusUnauthorized)
			_, _ = io.WriteString(w, "expired")
			return
		}
		_, _ = io.WriteString(w, `"answer"`)
	})
	doRequest := func(t *testing.T, auth Authorization) (string, error) {
		t.Helper()
		seen = nil
		client := NewClient(testUserAgent).WithAuthorization(auth)
		return UnmarshalJsonBody[string](client.DoRequest(t.Context(), MethodPut, server.Url))
	}

	t.Run("a 401 is retried once with the freshly minted token", func(t *testing.T) {
		auth := &refreshableAuthorization{token: "stale"}
		got, err := doRequest(t, auth)
		require.NoError(t, err)
		assert.Equal(t, "answer", got)
		assert.Equal(t, []string{"Bearer stale", "Bearer fresh"}, seen)
		assert.Equal(t, []BearerToken{"stale"}, auth.rejected, "the token that was refused is what the refresh is told about")
	})

	t.Run("a re-mint that changes nothing reports the 401", func(t *testing.T) {
		_, err := doRequest(t, &refreshableAuthorization{token: "stale", keepToken: true})
		httpErr, ok := errors.AsType[Error](err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
		assert.Len(t, seen, 1)
	})

	t.Run("a failing re-mint reports both errors", func(t *testing.T) {
		auth := &refreshableAuthorization{token: "stale", refreshErr: errors.New("the login expired")}
		_, err := doRequest(t, auth)
		_, ok := errors.AsType[Error](err)
		assert.True(t, ok, "the 401 the request ran into must stay reachable: %v", err)
		require.ErrorIs(t, err, auth.refreshErr, "and so must the reason nothing better could be tried")
		assert.Len(t, seen, 1)
	})

	t.Run("a static token is not re-minted", func(t *testing.T) {
		_, err := doRequest(t, BearerToken("static"))
		require.Error(t, err)
		assert.Len(t, seen, 1)
	})
}

func TestConvertStructOrMapToUrlValues(t *testing.T) {
	convert := func(t *testing.T, structOrMap any) url.Values {
		t.Helper()
		values, err := convertStructOrMapToUrlValues(structOrMap)
		require.NoError(t, err)
		return values
	}

	t.Run("a map goes as given, a zero value and a repeated parameter included", func(t *testing.T) {
		query := url.Values{"dry": {"true"}, "tag": {"a", "b"}, "empty": {""}}
		assert.Equal(t, query, convert(t, query))
		assert.Equal(t, url.Values{"page": {"0"}, "at": {"1234567890123456789"}}, convert(t, map[string]any{"page": 0, "at": 1234567890123456789}),
			"a large number stays the literal it marshalled to")
	})

	t.Run("a struct names its fields by json tag and drops the zero ones", func(t *testing.T) {
		type filter struct {
			Identifier   *string   `json:"identifier"`
			Name         string    `json:"name"`
			Restricted   *bool     `json:"restricted"`
			ExcludeTitle []string  `json:"excludeTitle"`
			RedirectUri  xurl.URL  `json:"redirect_uri"`
			Unset        *xurl.URL `json:"unset_uri"`
		}
		assert.Equal(t, url.Values{
			"identifier":   {"abc"},
			"excludeTitle": {"Workspace Created", "Tenant Deleted"},
			"redirect_uri": {"http://127.0.0.1:31234/callback"},
		}, convert(t, filter{
			Identifier:   new("abc"),
			ExcludeTitle: []string{"Workspace Created", "", "Tenant Deleted"},
			RedirectUri:  xurl.MustParsef("http://127.0.0.1:31234/callback"),
		}))
		assert.Empty(t, convert(t, &struct {
			Identifier *string `json:"identifier"`
		}{}), "a pointer to a struct drops its nil fields")
	})
}

type testServer struct {
	Url   *url.URL
	proxy *toxiproxy.Proxy
}

func newTestServer(t *testing.T, handler gohttp.HandlerFunc) testServer {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	proxy := toxiproxy.NewProxy(toxiproxy.NewServer(toxiproxy.NewMetricsContainer(nil), zerolog.Nop()), t.Name(), "127.0.0.1:0", upstream.Listener.Addr().String())
	require.NoError(t, proxy.Start())
	t.Cleanup(proxy.Stop)
	return testServer{Url: &url.URL{Scheme: "http", Host: proxy.Listen, Path: "/"}, proxy: proxy}
}

func (s testServer) addToxic(t *testing.T, toxicType, stream string, attributes map[string]any) {
	t.Helper()
	toxic, err := json.Marshal(map[string]any{"type": toxicType, "stream": stream, "attributes": attributes})
	require.NoError(t, err)
	_, err = s.proxy.Toxics.AddToxicJson(bytes.NewReader(toxic))
	require.NoError(t, err)
}

func (s testServer) removeToxics(t *testing.T) {
	t.Helper()
	s.proxy.Toxics.ResetToxics(t.Context())
}

type testBackoff struct {
	calls   atomic.Int32
	onRetry func()
}

func (b *testBackoff) Calculate(int) time.Duration {
	b.calls.Add(1)
	if b.onRetry != nil {
		b.onRetry()
	}
	return 0
}

func (b *testBackoff) option() RequestOption {
	return func(opts *requestOptions) {
		opts.retry.maxRetries = 2
		opts.retry.backoff = b
	}
}

type refreshableAuthorization struct {
	token      BearerToken
	keepToken  bool
	refreshErr error
	rejected   []BearerToken
}

func (a *refreshableAuthorization) GetBearerToken(context.Context) (BearerToken, error) {
	return a.token, nil
}

func (a *refreshableAuthorization) String() string {
	return "refreshableAuthorization"
}

func (a *refreshableAuthorization) RefreshBearerToken(_ context.Context, rejected BearerToken) (BearerToken, error) {
	a.rejected = append(a.rejected, rejected)
	if a.refreshErr != nil {
		return "", a.refreshErr
	}
	if !a.keepToken {
		a.token = "fresh"
	}
	return a.token, nil
}
