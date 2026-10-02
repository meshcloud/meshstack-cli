// Package http_test drives the client from the outside, so that it can parse answers into the
// types real callers declare. internal/http may not import any of them.
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
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

func TestHttpClient(t *testing.T) {
	t.Run("DoRequest success", func(t *testing.T) {
		captured := logs.Capture(t)
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
			fmt.Sprintf(`level=DEBUG msg=request url=%s/get method=GET headers="User-Agent=test-agent" body=<empty>`, client.ServerUrl),
			`level=DEBUG msg=response status=200 body="\"some-answer\""`,
		}, captured.Lines(slog.LevelDebug))
	})

	t.Run("DoRequest object call with empty 2xx body errors", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusOK)
		})
		_, err := client.DoRequest[*string](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected empty response body")
	})

	t.Run("DoRequest no-content call (any) tolerates an empty 2xx body", func(t *testing.T) {
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusAccepted)
		})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("trigger-run"))
		require.NoError(t, err)
	})

	t.Run("DoRequest with successful retry", func(t *testing.T) {
		for _, retryableStatusCode := range []int{429, 502, 503, 504} {
			t.Run(fmt.Sprintf("after code %d", retryableStatusCode), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					captured := logs.Capture(t)
					retryTestBackoff := retryTestBackoff{WaitTime: 1 * time.Second}
					retried := false
					client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
						if !retried {
							if retryableStatusCode == 429 {
								// Delay-seconds form. The HTTP-date form needs a mocked clock, which
								// only a test inside the package can install, so TestRetryAfterBackoff
								// covers it.
								resp.Header().Set("Retry-After", "1")
							}
							resp.WriteHeader(retryableStatusCode)
							retried = true
							return
						}
						resp.WriteHeader(gohttp.StatusOK)
						_, _ = resp.Write([]byte(`{}`))
					}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff})
					start := time.Now()

					_, err := client.DoRequest[any](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))

					require.NoError(t, err)
					assert.Equal(t, time.Second, time.Since(start), "the retry waited as long as it logged")
					if retryableStatusCode == 429 {
						assert.Equal(t, 0, retryTestBackoff.Called)
					} else {
						assert.Equal(t, 1, retryTestBackoff.Called)
					}
					assert.Equal(t, []string{
						fmt.Sprintf(`level=WARN msg="retrying request" status=%d method=GET path=/get attempt=1/3 waitTime=1s`, retryableStatusCode),
					}, captured.Lines(slog.LevelWarn))
				})
			})
		}
	})

	t.Run("DoRequest with 2 retries exhausted", func(t *testing.T) {
		captured := logs.Capture(t)
		backoff := retryTestBackoff{}
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
			resp.WriteHeader(gohttp.StatusBadGateway)
		}), http.RetryOptions{MaxRetries: 2, Backoff: &backoff})
		_, err := client.DoRequest[any](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		var httpErr http.Error
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, 502, httpErr.StatusCode)
		assert.Equal(t, 2, backoff.Called)
		assert.Equal(t, []string{
			fmt.Sprintf(`level=DEBUG msg=request url=%s/get method=GET headers="User-Agent=test-agent" body=<empty>`, client.ServerUrl),
			`level=WARN msg="retrying request" status=502 method=GET path=/get attempt=1/2 waitTime=0s`,
			`level=WARN msg="retrying request" status=502 method=GET path=/get attempt=2/2 waitTime=0s`,
			`level=DEBUG msg=response status=502 body=<empty>`,
		}, captured.Lines(slog.LevelDebug))
	})

	t.Run("DoRequest with context cancelled during backoff", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const cancelAfter = 1 * time.Second
			backoff := retryTestBackoff{WaitTime: 10 * time.Second}
			client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
				resp.WriteHeader(gohttp.StatusBadGateway)
			}), http.RetryOptions{MaxRetries: 3, Backoff: &backoff})
			// The bubble's clock only moves once the 502 has arrived and the backoff waits, so this
			// cancels inside the wait. Cancelling from the handler raced the 502 back to the client.
			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(cancelAfter, cancel)
			start := time.Now()

			_, err := client.DoRequest[any](ctx, gohttp.MethodGet, client.ServerUrl.JoinPath("get"))

			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 1, backoff.Called)
			assert.Equal(t, cancelAfter, time.Since(start), "the cancellation ended the backoff rather than waiting it out")
		})
	})

	// GET is the only method the client replays unasked, so MeshObjectClient marks its
	// idempotent PUT and DELETE with Retryable.
	t.Run("DoRequest replays another method than GET only when the caller marked it Retryable, body included", func(t *testing.T) {
		var bodies []string
		client := withTestRetry(newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			body, _ := io.ReadAll(req.Body)
			bodies = append(bodies, string(body))
			if len(bodies) == 1 {
				resp.WriteHeader(gohttp.StatusServiceUnavailable)
				return
			}
			resp.WriteHeader(gohttp.StatusNoContent)
		}), http.RetryOptions{MaxRetries: 3, Backoff: &retryTestBackoff{}})
		payload := http.WithJsonPayload(map[string]string{"key": "value"}, "application/json")

		for _, method := range []string{gohttp.MethodPatch, gohttp.MethodPost, gohttp.MethodPut, gohttp.MethodDelete} {
			bodies = nil
			_, err := client.DoRequest[any](t.Context(), method, client.ServerUrl, payload)
			require.Error(t, err, method)
			assert.Len(t, bodies, 1, "%s may change something, so replaying it needs the caller's word", method)

			bodies = nil
			_, err = client.DoRequest[any](t.Context(), method, client.ServerUrl, payload, http.Retryable())
			require.NoError(t, err, method)
			assert.Equal(t, []string{`{"key":"value"}`, `{"key":"value"}`}, bodies, method)
		}
	})

	t.Run("DoRequest re-mints once on 401", func(t *testing.T) {
		t.Run("retries with the freshly minted token", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale"}
			var seen []string
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
				seen = append(seen, req.Header.Get("Authorization"))
				if req.Header.Get("Authorization") == "Bearer stale" {
					resp.WriteHeader(gohttp.StatusUnauthorized)
					return
				}
				resp.WriteHeader(gohttp.StatusAccepted)
			})
			_, err := client.WithAuthorization(auth).DoRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			require.NoError(t, err)
			assert.Equal(t, []string{"Bearer stale", "Bearer fresh"}, seen)
			assert.Equal(t, []http.BearerToken{"stale"}, auth.rejected, "the token that was refused is what the refresh is told about")
		})

		t.Run("reports the 401 when the re-mint changes nothing", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale", keepToken: true}
			attempts := 0
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
				attempts++
				resp.WriteHeader(gohttp.StatusUnauthorized)
			})
			_, err := client.WithAuthorization(auth).DoRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			var httpErr http.Error
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
			assert.Equal(t, 1, attempts, "a re-mint that produced the same token has nothing new to try")
		})

		t.Run("reports both errors when the re-mint fails", func(t *testing.T) {
			auth := &refreshableAuthorization{token: "stale", refreshErr: errors.New("the login expired")}
			attempts := 0
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
				attempts++
				resp.WriteHeader(gohttp.StatusUnauthorized)
			})
			_, err := client.WithAuthorization(auth).DoRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			var httpErr http.Error
			require.ErrorAs(t, err, &httpErr, "the 401 the request ran into must stay reachable")
			assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
			require.ErrorIs(t, err, auth.refreshErr, "and so must the reason nothing better could be tried")
			assert.Equal(t, 1, attempts)
		})

		t.Run("leaves an authorization that cannot re-mint alone", func(t *testing.T) {
			attempts := 0
			client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
				attempts++
				resp.WriteHeader(gohttp.StatusUnauthorized)
			})
			_, err := client.WithAuthorization(http.BearerToken("static")).DoRequest[any](t.Context(), gohttp.MethodPut, client.ServerUrl.JoinPath("edit"))
			var httpErr http.Error
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, gohttp.StatusUnauthorized, httpErr.StatusCode)
			assert.Equal(t, 1, attempts)
		})
	})

	t.Run("DoRequest of []byte returns the retried answer as it came, empty included", func(t *testing.T) {
		auth := &refreshableAuthorization{token: "stale"}
		answer := "plain text"
		client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
			if req.Header.Get("Authorization") == "Bearer stale" {
				resp.WriteHeader(gohttp.StatusUnauthorized)
				_, _ = io.WriteString(resp, "expired")
				return
			}
			_, _ = io.WriteString(resp, answer)
		})
		body, err := client.WithAuthorization(auth).DoRequest[[]byte](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("get"))
		require.NoError(t, err)
		assert.Equal(t, "plain text", string(body))

		answer = ""
		body, err = client.DoRequest[[]byte](t.Context(), gohttp.MethodDelete, client.ServerUrl.JoinPath("delete"))
		require.NoError(t, err)
		assert.Empty(t, body)
	})
}

type refreshableAuthorization struct {
	token      http.BearerToken
	keepToken  bool
	refreshErr error
	rejected   []http.BearerToken
}

func (a *refreshableAuthorization) GetBearerToken(context.Context) (http.BearerToken, error) {
	return a.token, nil
}

func (a *refreshableAuthorization) RefreshBearerToken(_ context.Context, rejected http.BearerToken) (http.BearerToken, error) {
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
	var got url.Values
	client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
		got = req.URL.Query()
		_, _ = resp.Write([]byte(`"ok"`))
	})
	queryFrom := func(t *testing.T, queries ...any) url.Values {
		t.Helper()
		var options []http.RequestOption
		for _, query := range queries {
			options = append(options, http.WithUrlQuery(query))
		}
		_, err := client.DoRequest[string](t.Context(), gohttp.MethodGet, client.ServerUrl.JoinPath("list"), options...)
		require.NoError(t, err)
		return got
	}

	t.Run("a map or url.Values goes as given, a zero value and a repeated parameter included", func(t *testing.T) {
		query := url.Values{"dry": {"true"}, "tag": {"a", "b"}, "empty": {""}}
		assert.Equal(t, query, queryFrom(t, query))
		assert.Equal(t, url.Values{"page": {"0"}, "status": {"SUCCEEDED"}}, queryFrom(t, map[string]any{"page": 0, "status": "SUCCEEDED"}))
	})

	t.Run("a second query adds to the first", func(t *testing.T) {
		assert.Equal(t, url.Values{"buildingBlockDefinitionUuid": {"abc"}, "page": {"2"}},
			queryFrom(t, map[string]string{"buildingBlockDefinitionUuid": "abc"}, map[string]any{"page": 2}))
	})

	t.Run("a struct names its fields by json tag and drops the zero ones", func(t *testing.T) {
		type filter struct {
			Identifier   *string  `json:"identifier"`
			Name         string   `json:"name"`
			Restricted   *bool    `json:"restricted"`
			ExcludeTitle []string `json:"excludeTitle"`
			Other        []string `json:"other"`
		}
		assert.Equal(t, url.Values{"identifier": {"abc"}, "excludeTitle": {"Workspace Created", "Tenant Deleted"}},
			queryFrom(t, filter{Identifier: new("abc"), ExcludeTitle: []string{"Workspace Created", "", "Tenant Deleted"}}),
			"a slice is the parameter repeated, and drops an empty string like a zero field")
		assert.Empty(t, queryFrom(t, &struct {
			Identifier *string `json:"identifier"`
		}{}), "a pointer to a struct drops its nil fields")
	})
}

func TestFormPayloadOption(t *testing.T) {
	var header gohttp.Header
	var form url.Values
	var answer string
	client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, req *gohttp.Request) {
		assert.NoError(t, req.ParseForm())
		header, form = req.Header, req.PostForm
		_, _ = io.WriteString(resp, answer)
	})
	postForm := func(t *testing.T, payload any) {
		t.Helper()
		answer = `"ok"`
		_, err := client.DoRequest[string](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("token"), http.WithFormPayload(payload))
		require.NoError(t, err)
	}

	t.Run("a struct becomes a form named by its json tags", func(t *testing.T) {
		type refreshGrant struct {
			GrantType    string `json:"grant_type"`
			RefreshToken string `json:"refresh_token"`
			ClientId     string `json:"client_id"`
			CodeVerifier string `json:"code_verifier"`
		}
		postForm(t, refreshGrant{GrantType: "refresh_token", RefreshToken: "the-rotating-one", ClientId: "meshstack-cli"})
		assert.Equal(t, "application/x-www-form-urlencoded", header.Get("Content-Type"))
		assert.Equal(t, "application/json", header.Get("Accept"))
		assert.Equal(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"the-rotating-one"}, "client_id": {"meshstack-cli"}}, form,
			"a field the grant does not use must not be sent empty")
	})

	t.Run("no payload sends no body", func(t *testing.T) {
		postForm(t, nil)
		assert.Empty(t, form)
		assert.Empty(t, header.Get("Content-Type"))
	})

	t.Run("a form sends a URL and a JWT as the text they came from", func(t *testing.T) {
		type logoutRequest struct {
			RedirectUri xurl.URL  `json:"post_logout_redirect_uri"`
			IdToken     jwt.JWT   `json:"id_token_hint"`
			Unset       *xurl.URL `json:"unset_uri"`
		}
		var idToken jwt.JWT
		require.NoError(t, idToken.UnmarshalText([]byte(unsignedJwt(`{"sub":"someone"}`))))

		postForm(t, logoutRequest{RedirectUri: xurl.MustParsef("http://127.0.0.1:31234/callback"), IdToken: idToken})
		assert.Equal(t, url.Values{"post_logout_redirect_uri": {"http://127.0.0.1:31234/callback"}, "id_token_hint": {idToken.String()}}, form,
			"a URL nobody set is dropped like any other zero value")
	})

	type tokenResponse struct {
		Issuer      xurl.URL `json:"issuer"`
		AccessToken jwt.JWT  `json:"access_token"`
	}
	t.Run("the answer parses into the types the caller declared", func(t *testing.T) {
		accessToken := unsignedJwt(`{"MC_CUSTOMER":"my-workspace"}`)
		answer = fmt.Sprintf(`{"issuer":"https://sso.example.com/realms/meshfed","access_token":%q}`, accessToken)

		got, err := client.DoRequest[tokenResponse](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("token"))
		require.NoError(t, err)
		assert.Equal(t, "sso.example.com", got.Issuer.Host, "the field is a parsed URL, not the text it came from")
		assert.Equal(t, accessToken, got.AccessToken.String())
	})

	// Which texts jwt.JWT refuses is pinned in the jwt package, against its own testdata.
	t.Run("an answer that is not what those types accept fails the call", func(t *testing.T) {
		answer = `{"access_token":"an-opaque-token"}`

		_, err := client.DoRequest[tokenResponse](t.Context(), gohttp.MethodPost, client.ServerUrl.JoinPath("token"))
		assert.ErrorContains(t, err, "not a JWT")
	})
}

func unsignedJwt(claims string) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".not-a-signature"
}

type TestClient struct {
	http.Client

	ServerUrl *url.URL
}

func newTestClientWithServer(t *testing.T, handlerFunc gohttp.HandlerFunc) TestClient {
	t.Helper()
	// In memory, so that a test can run inside a synctest bubble.
	server := httptest.NewTestServer(t, handlerFunc)
	client := server.Client()
	serverUrl, err := url.Parse(server.URL)
	require.NoError(t, err)
	return TestClient{http.Client{Client: client, UserAgent: "test-agent"}, serverUrl}
}

// withTestRetry gives one test client its own retry policy. The shipped client is the one shared
// one and takes no configuration, so a test that needs a backoff it can count builds its own.
func withTestRetry(c TestClient, options http.RetryOptions) TestClient {
	options.ApplyTo(c.Client.Client)
	return c
}

type retryTestBackoff struct {
	WaitTime time.Duration
	Called   int
}

func (b *retryTestBackoff) Calculate(int) time.Duration {
	b.Called++
	return b.WaitTime
}
