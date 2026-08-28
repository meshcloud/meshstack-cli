// Package http_test drives the client from the outside, which is what lets it use the types the
// callers of this package parse their answers into — internal/http may not import any of them.
package http_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
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
		resp, err := client.DoRequest[string](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		require.NoError(t, err)
		assert.Equal(t, "some-answer", resp)
		assert.Equal(t, []string{
			fmt.Sprintf("request [url %s/get method GET headers User-Agent=test-agent body <empty>]", client.ServerUrl),
			`response [status 200 body "some-answer"]`,
		}, testLogger.Debugs)
		assert.Empty(t, testLogger.Warns)
	})

	t.Run("DoRequest object call with empty 2xx body errors", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusOK)
		})
		_, err := client.DoRequest[*string](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected empty response body")
	})

	t.Run("DoRequest no-content call (any) tolerates an empty 2xx body", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusAccepted) // empty body by design (trigger-run/delete)
		})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("trigger-run"))
		require.NoError(t, err)
	})

	t.Run("DoRequest with successful retry", func(t *testing.T) {
		for _, retryableStatusCode := range []int{429, 502, 503, 504} {
			t.Run(fmt.Sprintf("after code %d", retryableStatusCode), func(t *testing.T) {
				testLogger := installTestLogger(t)
				retryTestBackoff := retryTestBackoff{WaitTime: 1 * time.Second}
				retried := false
				client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
					if !retried {
						if retryableStatusCode == 429 {
							// In delay-seconds form. Its HTTP-date form is read against a mocked
							// clock, which only a test inside the package can install, so that
							// case lives in retry_test.go.
							resp.Header().Set("Retry-After", "1")
						}
						resp.WriteHeader(retryableStatusCode)
						retried = true
						return
					}
					resp.WriteHeader(gohttp.StatusOK)
					_, _ = resp.Write([]byte(`{}`))
				}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff})

				_, err := client.DoRequest[any](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
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
		}), http.RetryOptions{MaxRetries: 2, Backoff: &retryTestBackoff})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		var httpErr http.Error
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, 502, httpErr.StatusCode)
		assert.Equal(t, 2, retryTestBackoff.Called)
		assert.Equal(t, []string{
			"retrying request [status 502 method GET path /get attempt 1/2 waitTime 0s]",
			"retrying request [status 502 method GET path /get attempt 2/2 waitTime 0s]",
		}, testLogger.Warns)
		assert.Equal(t, []string{
			fmt.Sprintf("request [url %s/get method GET headers User-Agent=test-agent body <empty>]", client.ServerUrl),
			"response [status 502 body <empty>]",
		}, testLogger.Debugs)

	})

	t.Run("DoRequest with context cancelled during backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			resp.WriteHeader(502)
			cancel() // cancel context so the backoff wait is interrupted
		}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{WaitTime: 10 * time.Second}})
		_, err := client.DoRequest[any](ctx, gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DoRequest with PATCH (not retried)", func(t *testing.T) {
		attempts := 0
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			attempts++
			resp.WriteHeader(502)
		}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{WaitTime: 10 * time.Second}})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodPatch, client.ServerUrl)
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
			}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
			_, err := client.DoRequest[any](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("grant"))
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
			}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
			_, err := client.DoRequest[any](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("login"), http.Retryable())
			require.NoError(t, err)
			assert.Equal(t, 2, attempts)
		})
	})

	// PUT and DELETE are idempotent, but they are not retried on their own any more: GET is the
	// only method the client replays unasked. MeshObjectClient marks both with Retryable.
	t.Run("DoRequest with DELETE marked Retryable", func(t *testing.T) {
		attempts := 0
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			attempts++
			if attempts == 1 {
				resp.WriteHeader(503)
				return
			}
			resp.WriteHeader(gohttp.StatusNoContent)
		}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodDelete, client.ServerUrl.JoinPath("delete"), http.Retryable())
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
		}), http.RetryOptions{MaxRetries: 2, Backoff: &retryTestBackoff{}})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl,
			http.WithJsonPayload(map[string]string{"key": "value"}, "application/json"), http.Retryable())
		require.NoError(t, err)
		assert.Equal(t, 2, attempt)
	})

	t.Run("DoAuthorizedRequest with BearerTokenAuthorization", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			assert.Equal(t, "Bearer my-static-token", req.Header.Get("Authorization"))
			resp.WriteHeader(gohttp.StatusAccepted)
		})
		client.Authorization = http.BearerTokenAuthorization{Token: "my-static-token"}
		_, err := client.DoAuthorizedRequest[any](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("create"),
			http.WithJsonPayload("content", "text/plain"))
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
			_, err := client.DoAuthorizedRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
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
			_, err := client.DoAuthorizedRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			var httpErr http.Error
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
			_, err := client.DoAuthorizedRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			var httpErr http.Error
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
			client.Authorization = http.BearerTokenAuthorization{Token: "static"}
			_, err := client.DoAuthorizedRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			var httpErr http.Error
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
		_, err := client.DoRequest[string](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("list"),
			http.WithUrlQuery(query),
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

// TestFormPayloadOption covers what the OIDC grants send: a struct declared where the grant is
// built, turned into an url-encoded body. The two content types disagree here where
// WithJsonPayload has them agree — a grant is a form, and the token endpoint answers JSON.
func TestFormPayloadOption(t *testing.T) {
	postForm := func(t *testing.T, payload any) (gohttp.Header, url.Values) {
		t.Helper()
		var gotHeader gohttp.Header
		var gotForm url.Values
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			assert.NoError(t, req.ParseForm())
			gotHeader, gotForm = req.Header, req.PostForm
			resp.WriteHeader(gohttp.StatusOK)
			_, _ = resp.Write([]byte(`"ok"`))
		})
		_, err := client.DoRequest[string](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("token"),
			http.WithFormPayload(payload),
		)
		require.NoError(t, err)
		return gotHeader, gotForm
	}

	t.Run("a struct becomes a form named by its json tags", func(t *testing.T) {
		type refreshGrant struct {
			GrantType    string `json:"grant_type"`
			RefreshToken string `json:"refresh_token"`
			ClientId     string `json:"client_id"`
			CodeVerifier string `json:"code_verifier"`
		}
		header, got := postForm(t, refreshGrant{
			GrantType:    "refresh_token",
			RefreshToken: "the-rotating-one",
			ClientId:     "meshstack-cli",
		})
		assert.Equal(t, "application/x-www-form-urlencoded", header.Get("Content-Type"))
		assert.Equal(t, "application/json", header.Get("Accept"))
		assert.Equal(t, "refresh_token", got.Get("grant_type"))
		assert.Equal(t, "the-rotating-one", got.Get("refresh_token"))
		assert.Equal(t, "meshstack-cli", got.Get("client_id"))
		assert.False(t, got.Has("code_verifier"), "a field the grant does not use must not be sent empty")
	})

	t.Run("a map is sent verbatim", func(t *testing.T) {
		_, got := postForm(t, map[string]string{"grant_type": "authorization_code", "code": ""})
		assert.Equal(t, "authorization_code", got.Get("grant_type"))
		assert.True(t, got.Has("code"), "a map entry is sent even when empty")
	})

	t.Run("no payload sends no body", func(t *testing.T) {
		header, got := postForm(t, nil)
		assert.Empty(t, got)
		assert.Empty(t, header.Get("Content-Type"))
	})

	t.Run("a form sends those types as the text they came from", func(t *testing.T) {
		type logoutRequest struct {
			RedirectUri xurl.URL  `json:"post_logout_redirect_uri"`
			IdToken     jwt.JWT   `json:"id_token_hint"`
			ClientId    string    `json:"client_id"`
			Unset       *xurl.URL `json:"unset_uri"`
		}
		redirectUri, err := url.Parse("http://127.0.0.1:31234/callback")
		require.NoError(t, err)
		var idToken jwt.JWT
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"someone"}`))
		require.NoError(t, idToken.UnmarshalText([]byte("e30."+claims+".not-a-signature")))

		_, got := postForm(t, logoutRequest{
			RedirectUri: xurl.URL{URL: redirectUri},
			IdToken:     idToken,
			ClientId:    "meshstack-cli",
		})
		assert.Equal(t, "http://127.0.0.1:31234/callback", got.Get("post_logout_redirect_uri"))
		assert.Equal(t, idToken.String, got.Get("id_token_hint"))
		assert.False(t, got.Has("unset_uri"), "a URL nobody set is dropped like any other zero value")
	})

	// The answer of a grant is where xurl.URL and jwt.JWT earn their keep. Both parse from a JSON
	// string through UnmarshalText, so a caller declares the field it wants and reads a *url.URL
	// or the token's claims, rather than a string every reader has to parse again.
	t.Run("the answer parses into the types the caller declared", func(t *testing.T) {
		type tokenResponse struct {
			Issuer      xurl.URL `json:"issuer"`
			AccessToken jwt.JWT  `json:"access_token"`
		}
		// Only the middle part is ever read, so the header is {} and the signature is not one.
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"MC_CUSTOMER":"my-workspace"}`))
		accessToken := "e30." + claims + ".not-a-signature"

		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			assert.NoError(t, req.ParseForm())
			assert.Equal(t, "refresh_token", req.PostForm.Get("grant_type"))
			resp.WriteHeader(gohttp.StatusOK)
			_, _ = resp.Write(fmt.Appendf(nil,
				`{"issuer":"https://sso.example.com/realms/meshfed","access_token":%q}`, accessToken))
		})
		got, err := client.DoRequest[tokenResponse](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("token"),
			http.WithFormPayload(map[string]string{"grant_type": "refresh_token"}),
		)
		require.NoError(t, err)

		assert.Equal(t, "https://sso.example.com/realms/meshfed", got.Issuer.String())
		assert.Equal(t, "sso.example.com", got.Issuer.Host, "the field is a parsed URL, not the text it came from")
		assert.Equal(t, accessToken, got.AccessToken.String)
		assert.Equal(t, "my-workspace", string(jwt.WorkspaceClaim.GetFrom(got.AccessToken)),
			"the claims come with the token, so nothing has to decode it a second time")
	})

	// One case is enough here: that a declared type refusing the answer fails the whole call.
	// Which texts jwt.JWT refuses is pinned in the jwt package, against its own testdata.
	t.Run("an answer that is not what those types accept fails the call", func(t *testing.T) {
		type tokenResponse struct {
			AccessToken jwt.JWT `json:"access_token"`
		}
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusOK)
			_, _ = resp.Write([]byte(`{"access_token":"an-opaque-token"}`))
		})
		_, err := client.DoRequest[tokenResponse](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("token"),
			http.WithFormPayload(map[string]string{"grant_type": "refresh_token"}),
		)
		assert.ErrorContains(t, err, "not a JWT")
	})
}

type TestClient struct {
	http.Client
	ServerUrl *url.URL
}

func newTestClientWithServer(t *testing.T, handlerFunc gohttp.HandlerFunc) TestClient {
	t.Helper()
	server := httptest.NewServer(handlerFunc)
	t.Cleanup(server.Close)
	serverUrl, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := server.Client()
	return TestClient{http.Client{Client: client, UserAgent: "test-agent"}, serverUrl}
}

// withTestRetry gives one test client its own retry policy. Production code has exactly one
// retrying client and no way to configure it, which is the point; a test needs a backoff it can
// count and drive, so it builds its own.
func withTestRetry(c TestClient, options http.RetryOptions) TestClient {
	options.ApplyTo(c.Client.Client)
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
