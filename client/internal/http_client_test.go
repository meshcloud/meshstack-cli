package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpClient(t *testing.T) {
	t.Run("DoRequest success", func(t *testing.T) {
		testLogger := installTestLogger(t)
		client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			resp.WriteHeader(http.StatusOK)
			_, _ = resp.Write([]byte(`"some-answer"`))
			assert.Equal(t, "/get", req.URL.Path)
			assert.Equal(t, http.MethodGet, req.Method)
			assert.Equal(t, "test-agent", req.Header.Get("User-Agent"))
		})
		resp, err := DoRequest[string](t.Context(), client, http.MethodGet, client.RootUrl.JoinPath("get"))
		require.NoError(t, err)
		assert.Equal(t, "some-answer", resp)
		assert.Equal(t, []string{
			fmt.Sprintf("request [url %s/get method GET headers User-Agent=test-agent body <empty>]", client.RootUrl),
			`response [status 200 body "some-answer"]`,
		}, testLogger.Debugs)
		assert.Empty(t, testLogger.Warns)
	})

	t.Run("DoRequest object call with empty 2xx body errors", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			resp.WriteHeader(http.StatusOK)
		})
		_, err := DoRequest[*string](t.Context(), client, http.MethodGet, client.RootUrl.JoinPath("get"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected empty response body")
	})

	t.Run("DoRequest no-content call (any) tolerates an empty 2xx body", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			resp.WriteHeader(http.StatusAccepted) // empty body by design (trigger-run/delete)
		})
		_, err := DoRequest[any](t.Context(), client, http.MethodPost, client.RootUrl.JoinPath("trigger-run"))
		require.NoError(t, err)
	})

	t.Run("DoRequest with successful retry", func(t *testing.T) {
		for _, retryableStatusCode := range []int{429, 502, 503, 504} {
			t.Run(fmt.Sprintf("after code %d", retryableStatusCode), func(t *testing.T) {
				nowUTC := mockTimeNowAsUTC(t)

				testLogger := installTestLogger(t)
				retryTestBackoff := retryTestBackoff{WaitTime: 1 * time.Second}
				retried := false
				client := WithRetry(newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
					if !retried {
						if retryableStatusCode == 429 {
							resp.Header().Set("Retry-After", nowUTC.Add(1*time.Second).Format(http.TimeFormat))
						}
						resp.WriteHeader(retryableStatusCode)
						retried = true
						return
					}
					resp.WriteHeader(http.StatusOK)
					_, _ = resp.Write([]byte(`{}`))
				}), RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff})

				_, err := DoRequest[any](t.Context(), client, http.MethodGet, client.RootUrl.JoinPath("get"))
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
		client := WithRetry(newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			resp.WriteHeader(502)
		}), RetryOptions{MaxRetries: 2, Backoff: &retryTestBackoff})
		_, err := DoRequest[any](t.Context(), client, http.MethodGet, client.RootUrl.JoinPath("get"))
		var httpErr HttpError
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
		client := WithRetry(newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			resp.WriteHeader(502)
			cancel() // cancel context so the backoff wait is interrupted
		}), RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{WaitTime: 10 * time.Second}})
		_, err := DoRequest[any](ctx, client, http.MethodGet, client.RootUrl.JoinPath("get"))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DoRequest with PATCH (not retried)", func(t *testing.T) {
		attempts := 0
		client := WithRetry(newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			attempts++
			resp.WriteHeader(502)
		}), RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{WaitTime: 10 * time.Second}})
		_, err := DoRequest[any](t.Context(), client, http.MethodPatch, client.RootUrl)
		require.Error(t, err)
		assert.Equal(t, 1, attempts, "PATCH must not be retried")
	})

	t.Run("DoRequest with DELETE (retried, idempotent)", func(t *testing.T) {
		attempts := 0
		client := WithRetry(newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			attempts++
			if attempts == 1 {
				resp.WriteHeader(503)
				return
			}
			resp.WriteHeader(http.StatusNoContent)
		}), RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
		_, err := DoRequest[any](t.Context(), client, http.MethodDelete, client.RootUrl.JoinPath("delete"))
		require.NoError(t, err)
		assert.Equal(t, 2, attempts, "DELETE must be retried after a 503")
	})

	t.Run("DoRequest with PUT replays body on retry", func(t *testing.T) {
		attempt := 0
		client := WithRetry(newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			body, _ := io.ReadAll(req.Body)
			assert.JSONEq(t, `{"key":"value"}`, string(body))
			attempt++
			if attempt == 1 {
				resp.WriteHeader(502)
				return
			}
			resp.WriteHeader(200)
		}), RetryOptions{MaxRetries: 2, Backoff: &retryTestBackoff{}})
		_, err := DoRequest[any](t.Context(), client, http.MethodPut, client.RootUrl, withPayload(map[string]string{"key": "value"}, "application/json"))
		require.NoError(t, err)
		assert.Equal(t, 2, attempt)
	})

	t.Run("DoAuthorizedRequest with BearerTokenAuthorization", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			assert.Equal(t, "Bearer my-static-token", req.Header.Get("Authorization"))
			resp.WriteHeader(http.StatusAccepted)
		})
		client.Authorization = BearerTokenAuthorization{Token: "my-static-token"}
		_, err := DoAuthorizedRequest[any](t.Context(), client, http.MethodPost, client.RootUrl.JoinPath("create"), withPayload("content", "text/plain"))
		require.NoError(t, err)
	})

	t.Run("DoAuthorizedRequest re-mints once on 401", func(t *testing.T) {
		t.Run("retries with the freshly minted token", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale"}
			seen := []string{}
			client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
				seen = append(seen, req.Header.Get("Authorization"))
				if req.Header.Get("Authorization") == "Bearer stale" {
					resp.WriteHeader(http.StatusUnauthorized)
					return
				}
				resp.WriteHeader(http.StatusAccepted)
			})
			client.Authorization = auth
			_, err := DoAuthorizedRequest[any](t.Context(), client, http.MethodPut, client.RootUrl.JoinPath("edit"))
			require.NoError(t, err)
			assert.Equal(t, []string{"Bearer stale", "Bearer fresh"}, seen)
			assert.Equal(t, []string{"stale"}, auth.rejected, "the token that was refused is what the refresh is told about")
		})

		t.Run("reports the 401 when the re-mint changes nothing", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale", keepToken: true}
			attempts := 0
			client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
				attempts++
				resp.WriteHeader(http.StatusUnauthorized)
			})
			client.Authorization = auth
			_, err := DoAuthorizedRequest[any](t.Context(), client, http.MethodPut, client.RootUrl.JoinPath("edit"))
			var httpErr HttpError
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
			assert.Equal(t, 1, attempts, "a re-mint that produced the same token has nothing new to try")
		})

		t.Run("reports both errors when the re-mint fails", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale", refreshErr: errors.New("the login expired")}
			attempts := 0
			client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
				attempts++
				resp.WriteHeader(http.StatusUnauthorized)
			})
			client.Authorization = auth
			_, err := DoAuthorizedRequest[any](t.Context(), client, http.MethodPut, client.RootUrl.JoinPath("edit"))
			var httpErr HttpError
			require.ErrorAs(t, err, &httpErr, "the 401 the request ran into must stay reachable")
			assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
			require.ErrorIs(t, err, auth.refreshErr, "and so must the reason nothing better could be tried")
			assert.Equal(t, 1, attempts)
		})

		t.Run("leaves an authorization that cannot re-mint alone", func(t *testing.T) {
			attempts := 0
			client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
				attempts++
				resp.WriteHeader(http.StatusUnauthorized)
			})
			client.Authorization = BearerTokenAuthorization{Token: "static"}
			_, err := DoAuthorizedRequest[any](t.Context(), client, http.MethodPut, client.RootUrl.JoinPath("edit"))
			var httpErr HttpError
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
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
		client := newTestClientWithServer(t, func(resp http.ResponseWriter, req *http.Request) {
			gotQuery = req.URL.Query()
			resp.WriteHeader(http.StatusOK)
			_, _ = resp.Write([]byte(`"ok"`))
		})
		_, err := DoRequest[string](t.Context(), client, http.MethodGet, client.RootUrl.JoinPath("list"),
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

func newTestClientWithServer(t *testing.T, handlerFunc http.HandlerFunc) HttpClient {
	t.Helper()
	server := httptest.NewServer(handlerFunc)
	t.Cleanup(server.Close)
	rootUrl, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := server.Client()
	return HttpClient{
		Client:    client,
		RootUrl:   rootUrl,
		UserAgent: "test-agent",
	}
}

func installTestLogger(t *testing.T) *testLogger {
	t.Helper()
	testLogger := &testLogger{}
	previousLog := Log
	Log = testLogger
	t.Cleanup(func() {
		Log = previousLog
	})
	return testLogger
}

type testLogger struct {
	Debugs []string
	Infos  []string
	Warns  []string
}

func (c *testLogger) Debug(_ context.Context, msg string, args ...any) {
	c.Debugs = append(c.Debugs, fmt.Sprintf("%s %v", msg, args))
}

func (c *testLogger) Info(_ context.Context, msg string, args ...any) {
	c.Infos = append(c.Infos, fmt.Sprintf("%s %v", msg, args))
}

func (c *testLogger) Warn(_ context.Context, msg string, args ...any) {
	c.Warns = append(c.Warns, fmt.Sprintf("%s %v", msg, args))
}

type retryTestBackoff struct {
	WaitTime time.Duration
	Called   int
}

func (b *retryTestBackoff) Calculate(int) time.Duration {
	b.Called++
	return b.WaitTime
}
