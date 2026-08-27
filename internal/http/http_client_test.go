package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpClient(t *testing.T) {
	t.Run("DoRequest success", func(t *testing.T) {
		testLogger := installTestLogger(t)
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusOK)
			_, _ = resp.Write([]byte(`"some-answer"`))
			assert.Equal(t, "/get", req.URL.Path)
			assert.Equal(t, gohttp.MethodGet, req.Method)
			assert.Equal(t, "test-agent", req.Header.Get("User-Agent"))
		})
		resp, err := DoRequest[string](t.Context(), client, gohttp.MethodGet, client.RootUrl.JoinPath("get"))
		require.NoError(t, err)
		assert.Equal(t, "some-answer", resp)
		assert.Equal(t, []string{
			fmt.Sprintf("request [url %s/get method GET headers User-Agent=test-agent body <empty>]", client.RootUrl),
			`response [status 200 body "some-answer"]`,
		}, testLogger.Debugs)
		assert.Empty(t, testLogger.Warns)
	})

	t.Run("DoRequest object call with empty 2xx body errors", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusOK)
		})
		_, err := DoRequest[*string](t.Context(), client, gohttp.MethodGet, client.RootUrl.JoinPath("get"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected empty response body")
	})

	t.Run("DoRequest no-content call (any) tolerates an empty 2xx body", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusAccepted) // empty body by design (trigger-run/delete)
		})
		_, err := DoRequest[any](t.Context(), client, gohttp.MethodPost, client.RootUrl.JoinPath("trigger-run"))
		require.NoError(t, err)
	})

	t.Run("DoRequest with successful retry", func(t *testing.T) {
		for _, retryableStatusCode := range []int{429, 502, 503, 504} {
			t.Run(fmt.Sprintf("after code %d", retryableStatusCode), func(t *testing.T) {
				nowUTC := mockTimeNowAsUTC(t)

				testLogger := installTestLogger(t)
				retryTestBackoff := retryTestBackoff{WaitTime: 1 * time.Second}
				retried := false
				client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
					if !retried {
						if retryableStatusCode == 429 {
							resp.Header().Set("Retry-After", nowUTC.Add(1*time.Second).Format(gohttp.TimeFormat))
						}
						resp.WriteHeader(retryableStatusCode)
						retried = true
						return
					}
					resp.WriteHeader(gohttp.StatusOK)
					_, _ = resp.Write([]byte(`{}`))
				}), retryOptions{MaxRetries: 3, Backoff: &retryTestBackoff})

				_, err := DoRequest[any](t.Context(), client, gohttp.MethodGet, client.RootUrl.JoinPath("get"))
				require.NoError(t, err)
				if retryableStatusCode == 429 {
					assert.Equal(t, 0, retryTestBackoff.Called)
				} else {
					assert.Equal(t, 1, retryTestBackoff.Called)
				}
				assert.Equal(t, []string{
					fmt.Sprintf("retrying request [status %d method GET path /get attempt 1/3 waitTime 1s]", retryableStatusCode),
				}, testLogger.Warns)
			})
		}
	})

	t.Run("DoRequest with 2 retries exhausted", func(t *testing.T) {
		testLogger := installTestLogger(t)
		retryTestBackoff := retryTestBackoff{}
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(502)
		}), retryOptions{MaxRetries: 2, Backoff: &retryTestBackoff})
		_, err := DoRequest[any](t.Context(), client, gohttp.MethodGet, client.RootUrl.JoinPath("get"))
		var httpErr Error
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, 502, httpErr.StatusCode)
		assert.Equal(t, 2, retryTestBackoff.Called)
		assert.Equal(t, []string{
			"retrying request [status 502 method GET path /get attempt 1/2 waitTime 0s]",
			"retrying request [status 502 method GET path /get attempt 2/2 waitTime 0s]",
		}, testLogger.Warns)
		assert.Equal(t, []string{
			fmt.Sprintf("request [url %s/get method GET headers User-Agent=test-agent body <empty>]", client.RootUrl),
			"response [status 502 body <empty>]",
		}, testLogger.Debugs)

	})

	t.Run("DoRequest with context cancelled during backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(502)
			cancel() // cancel context so the backoff wait is interrupted
		}), retryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{WaitTime: 10 * time.Second}})
		_, err := DoRequest[any](ctx, client, gohttp.MethodGet, client.RootUrl.JoinPath("get"))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DoRequest with PATCH (not retried)", func(t *testing.T) {
		attempts := 0
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			attempts++
			resp.WriteHeader(502)
		}), retryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{WaitTime: 10 * time.Second}})
		_, err := DoRequest[any](t.Context(), client, gohttp.MethodPatch, client.RootUrl)
		require.Error(t, err)
		assert.Equal(t, 1, attempts, "PATCH must not be retried")
	})

	// A POST is what an OIDC grant and /api/login both are, and only one of them may be
	// replayed. Retryable is what tells them apart, and it has to survive being turned into a
	// request: it travels in the context, which is what the transport sees.
	t.Run("DoRequest with POST", func(t *testing.T) {
		t.Run("is not retried by default", func(t *testing.T) {
			attempts := 0
			client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				attempts++
				resp.WriteHeader(503)
			}), retryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
			_, err := DoRequest[any](t.Context(), client, gohttp.MethodPost, client.RootUrl.JoinPath("grant"))
			require.Error(t, err)
			assert.Equal(t, 1, attempts, "a POST may create something, so replaying it needs the caller's word")
		})

		t.Run("is retried when the caller marked it Retryable", func(t *testing.T) {
			attempts := 0
			client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				attempts++
				if attempts == 1 {
					resp.WriteHeader(503)
					return
				}
				resp.WriteHeader(gohttp.StatusOK)
				_, _ = resp.Write([]byte(`{}`))
			}), retryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
			_, err := DoRequest[any](t.Context(), client, gohttp.MethodPost, client.RootUrl.JoinPath("login"), Retryable())
			require.NoError(t, err)
			assert.Equal(t, 2, attempts)
		})
	})

	t.Run("DoRequest with DELETE (retried, idempotent)", func(t *testing.T) {
		attempts := 0
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			attempts++
			if attempts == 1 {
				resp.WriteHeader(503)
				return
			}
			resp.WriteHeader(gohttp.StatusNoContent)
		}), retryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
		_, err := DoRequest[any](t.Context(), client, gohttp.MethodDelete, client.RootUrl.JoinPath("delete"))
		require.NoError(t, err)
		assert.Equal(t, 2, attempts, "DELETE must be retried after a 503")
	})

	t.Run("DoRequest with PUT replays body on retry", func(t *testing.T) {
		attempt := 0
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			body, _ := io.ReadAll(req.Body)
			assert.JSONEq(t, `{"key":"value"}`, string(body))
			attempt++
			if attempt == 1 {
				resp.WriteHeader(502)
				return
			}
			resp.WriteHeader(200)
		}), retryOptions{MaxRetries: 2, Backoff: &retryTestBackoff{}})
		_, err := DoRequest[any](t.Context(), client, gohttp.MethodPut, client.RootUrl, WithPayload(map[string]string{"key": "value"}, "application/json"))
		require.NoError(t, err)
		assert.Equal(t, 2, attempt)
	})

	t.Run("DoAuthorizedRequest with BearerTokenAuthorization", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			assert.Equal(t, "Bearer my-static-token", req.Header.Get("Authorization"))
			resp.WriteHeader(gohttp.StatusAccepted)
		})
		client.Authorization = BearerTokenAuthorization{Token: "my-static-token"}
		_, err := DoAuthorizedRequest[any](t.Context(), client, gohttp.MethodPost, client.RootUrl.JoinPath("create"), WithPayload("content", "text/plain"))
		require.NoError(t, err)
	})

	t.Run("DoAuthorizedRequest re-mints once on 401", func(t *testing.T) {
		t.Run("retries with the freshly minted token", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale"}
			seen := []string{}
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				seen = append(seen, req.Header.Get("Authorization"))
				if req.Header.Get("Authorization") == "Bearer stale" {
					resp.WriteHeader(gohttp.StatusUnauthorized)
					return
				}
				resp.WriteHeader(gohttp.StatusAccepted)
			})
			client.Authorization = auth
			_, err := DoAuthorizedRequest[any](t.Context(), client, gohttp.MethodPut, client.RootUrl.JoinPath("edit"))
			require.NoError(t, err)
			assert.Equal(t, []string{"Bearer stale", "Bearer fresh"}, seen)
			assert.Equal(t, []string{"stale"}, auth.rejected, "the token that was refused is what the refresh is told about")
		})

		t.Run("reports the 401 when the re-mint changes nothing", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale", keepToken: true}
			attempts := 0
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				attempts++
				resp.WriteHeader(gohttp.StatusUnauthorized)
			})
			client.Authorization = auth
			_, err := DoAuthorizedRequest[any](t.Context(), client, gohttp.MethodPut, client.RootUrl.JoinPath("edit"))
			var httpErr Error
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
			assert.Equal(t, 1, attempts, "a re-mint that produced the same token has nothing new to try")
		})

		t.Run("reports both errors when the re-mint fails", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale", refreshErr: errors.New("the login expired")}
			attempts := 0
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				attempts++
				resp.WriteHeader(gohttp.StatusUnauthorized)
			})
			client.Authorization = auth
			_, err := DoAuthorizedRequest[any](t.Context(), client, gohttp.MethodPut, client.RootUrl.JoinPath("edit"))
			var httpErr Error
			require.ErrorAs(t, err, &httpErr, "the 401 the request ran into must stay reachable")
			assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
			require.ErrorIs(t, err, auth.refreshErr, "and so must the reason nothing better could be tried")
			assert.Equal(t, 1, attempts)
		})

		t.Run("leaves an authorization that cannot re-mint alone", func(t *testing.T) {
			attempts := 0
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				attempts++
				resp.WriteHeader(gohttp.StatusUnauthorized)
			})
			client.Authorization = BearerTokenAuthorization{Token: "static"}
			_, err := DoAuthorizedRequest[any](t.Context(), client, gohttp.MethodPut, client.RootUrl.JoinPath("edit"))
			var httpErr Error
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
			assert.Equal(t, 1, attempts)
		})
	})
}

// refreshableAuthorization mints "fresh" once it has been told its token was refused, and
// records every token it was told about.
type refreshableAuthorization struct {
	token      string
	keepToken  bool
	refreshErr error
	rejected   []string
}

func (a *refreshableAuthorization) BearerToken(context.Context) (string, error) {
	return a.token, nil
}

func (a *refreshableAuthorization) RefreshBearerToken(_ context.Context, rejected string) (string, error) {
	a.rejected = append(a.rejected, rejected)
	if a.refreshErr != nil {
		return "", a.refreshErr
	}
	if !a.keepToken {
		a.token = "fresh"
	}
	return a.token, nil
}

func TestUrlQueryOptions(t *testing.T) {
	queryFrom := func(t *testing.T, query any) url.Values {
		t.Helper()
		var gotQuery url.Values
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			gotQuery = req.URL.Query()
			resp.WriteHeader(gohttp.StatusOK)
			_, _ = resp.Write([]byte(`"ok"`))
		})
		_, err := DoRequest[string](t.Context(), client, gohttp.MethodGet, client.RootUrl.JoinPath("list"),
			WithUrlQuery(query),
		)
		require.NoError(t, err)
		return gotQuery
	}

	t.Run("a map is sent verbatim", func(t *testing.T) {
		got := queryFrom(t, map[string]string{"definitionUuid": "abc", "status": "SUCCEEDED"})
		assert.Equal(t, "abc", got.Get("definitionUuid"))
		assert.Equal(t, "SUCCEEDED", got.Get("status"))
	})

	t.Run("map values are kept even when zero", func(t *testing.T) {
		got := queryFrom(t, map[string]any{"page": 0})
		assert.Equal(t, "0", got.Get("page"))
	})

	t.Run("struct fields are named by json tag and zero fields are dropped", func(t *testing.T) {
		type filter struct {
			Identifier *string `json:"identifier"`
			Name       string  `json:"name"`
			Restricted *bool   `json:"restricted"`
		}
		got := queryFrom(t, filter{Identifier: new("abc")})
		assert.Equal(t, "abc", got.Get("identifier"))
		assert.False(t, got.Has("name"), "zero string field must be dropped")
		assert.False(t, got.Has("restricted"), "nil pointer field must be dropped")
	})

	t.Run("a zero-value struct adds no params", func(t *testing.T) {
		type filter struct {
			Identifier *string `json:"identifier"`
		}
		got := queryFrom(t, &filter{})
		assert.Empty(t, got)
	})
}

func mockTimeNowAsUTC(t *testing.T) time.Time {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	timeNow = func() time.Time { return now }
	t.Cleanup(func() {
		timeNow = time.Now
	})
	return now
}

func newTestClientWithServer(t *testing.T, handlerFunc gohttp.HandlerFunc) Client {
	t.Helper()
	server := httptest.NewServer(handlerFunc)
	t.Cleanup(server.Close)
	rootUrl, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := server.Client()
	return Client{
		Client:    client,
		RootUrl:   rootUrl,
		UserAgent: "test-agent",
	}
}

// withTestRetry gives one test client its own retry policy. Production code has exactly one
// retrying client and no way to configure it, which is the point; a test needs a backoff it can
// count and drive, so it builds its own.
func withTestRetry(c Client, options retryOptions) Client {
	c.Client = withRetry(c.Client, options)
	return c
}

func installTestLogger(t *testing.T) *testLogger {
	t.Helper()
	testLogger := &testLogger{}
	previous := slog.Default()
	slog.SetDefault(slog.New(testLogger))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	return testLogger
}

// testLogger is the slog handler these tests read records back from. It formats a record the way
// the old Logger interface did — the message, then the attributes as a bracketed list — so that
// what a test asserts is still the line a person would read in the terraform log.
type testLogger struct {
	Debugs []string
	Infos  []string
	Warns  []string
}

var _ slog.Handler = (*testLogger)(nil)

func (c *testLogger) Enabled(context.Context, slog.Level) bool { return true }

func (c *testLogger) Handle(_ context.Context, record slog.Record) error {
	var args []any
	record.Attrs(func(attr slog.Attr) bool {
		args = append(args, attr.Key, attr.Value.Any())
		return true
	})
	line := fmt.Sprintf("%s %v", record.Message, args)
	switch {
	case record.Level >= slog.LevelWarn:
		c.Warns = append(c.Warns, line)
	case record.Level >= slog.LevelInfo:
		c.Infos = append(c.Infos, line)
	default:
		c.Debugs = append(c.Debugs, line)
	}
	return nil
}

func (c *testLogger) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *testLogger) WithGroup(string) slog.Handler      { return c }

type retryTestBackoff struct {
	WaitTime time.Duration
	Called   int
}

func (b *retryTestBackoff) Calculate(int) time.Duration {
	b.Called++
	return b.WaitTime
}

func TestExcerptFitsOnOneLine(t *testing.T) {
	assert.Equal(t, "a b c", excerpt([]byte(" a\n\tb  c ")))

	long := excerpt(bytes.Repeat([]byte("x"), 500))
	assert.Len(t, long, 203)
	assert.True(t, strings.HasSuffix(long, "..."))
}
