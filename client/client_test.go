package client

import (
	"errors"
	gohttp "net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

type erroringRoundTripper struct{ calls int }

func (rt *erroringRoundTripper) RoundTrip(*gohttp.Request) (*gohttp.Response, error) {
	rt.calls++
	return nil, errors.New("no server is available to handle this request")
}

func TestCheckMeshVersion_SkipsRequestWhenOptedOut(t *testing.T) {
	newUnreachableClient := func() (http.Client, *erroringRoundTripper) {
		transport := new(erroringRoundTripper)
		httpClient := http.NewClient(&url.URL{Scheme: "https", Host: "meshstack.invalid"}, "test-agent", nil)
		httpClient.Transport = transport
		return httpClient, transport
	}

	t.Run("MESHSTACK_SKIP_VERSION_CHECK=true skips the /mesh/info request entirely", func(t *testing.T) {
		t.Setenv("MESHSTACK_SKIP_VERSION_CHECK", "true")
		httpClient, transport := newUnreachableClient()
		require.NoError(t, checkMeshVersion(t.Context(), newMeshInfoClient(httpClient)))
		assert.Zero(t, transport.calls, "opting out of the version check must not send a request that can block on retries")
	})

	t.Run("without the opt-out an unreachable /mesh/info fails", func(t *testing.T) {
		t.Setenv("MESHSTACK_SKIP_VERSION_CHECK", "")
		httpClient, transport := newUnreachableClient()
		err := checkMeshVersion(t.Context(), newMeshInfoClient(httpClient))
		require.ErrorContains(t, err, "failed to retrieve meshStack instance information")
		assert.Equal(t, 1, transport.calls)
	})
}
