package oidc_test

import (
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

func TestARefreshCountsTheSessionEndFromKeycloaksRefreshExpiresIn(t *testing.T) {
	tests := []struct {
		name             string
		refreshExpiresIn string
		want             time.Duration
	}{
		{name: "a limit", refreshExpiresIn: `,"refresh_expires_in":86400`, want: 24 * time.Hour},
		{name: "0 for no limit", refreshExpiresIn: `,"refresh_expires_in":0`},
		{name: "no field at all", refreshExpiresIn: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				server := httptest.NewServer(gohttp.HandlerFunc(func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
					resp.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(resp, `{"access_token":"e30.e30.sig","refresh_token":"rotated"%s}`, tt.refreshExpiresIn)
				}))
				defer server.Close()
				client := oidc.Client{
					Client:        http.NewClient("oidc-test"),
					TokenEndpoint: xurl.MustParsef("%s/token", server.URL),
					Id:            "meshstack-cli",
				}
				requested := time.Now()

				token, err := client.Refresh(t.Context(), "refresh-token", scope.Scopes{scope.OpenId})

				require.NoError(t, err)
				assert.Equal(t, "rotated", token.RefreshToken)
				if tt.want == 0 {
					assert.True(t, token.RefreshExpiresAt.IsZero())
				} else {
					assert.Equal(t, requested.Add(tt.want), token.RefreshExpiresAt)
				}
			})
		})
	}
}
