package client

import (
	"errors"
	gohttp "net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type erroringRoundTripper struct{ calls int }

func (rt *erroringRoundTripper) RoundTrip(*gohttp.Request) (*gohttp.Response, error) {
	rt.calls++
	return nil, errors.New("no server is available to handle this request")
}

func TestCheckMeshVersion_SkipsRequestWhenOptedOut(t *testing.T) {
	newUnreachableClient := func() (internal.HttpClient, *erroringRoundTripper) {
		transport := new(erroringRoundTripper)
		// A client of its own rather than http.NewClient: that one hands out the process-wide
		// shared client, and replacing its transport would take the retries away from every
		// other test in this binary.
		return internal.HttpClient{
			Client:  http.Client{Client: &gohttp.Client{Transport: transport}, UserAgent: "test-agent"},
			RootUrl: &url.URL{Scheme: "https", Host: "meshstack.invalid"},
		}, transport
	}

	t.Run("MESHSTACK_SKIP_VERSION_CHECK=true skips the /mesh/info request entirely", func(t *testing.T) {
		t.Setenv("MESHSTACK_SKIP_VERSION_CHECK", "true")
		httpClient, transport := newUnreachableClient()
		require.NoError(t, newMeshInfoClient(httpClient).checkMeshVersion(t.Context()))
		assert.Zero(t, transport.calls, "opting out of the version check must not send a request that can block on retries")
	})

	t.Run("without the opt-out an unreachable /mesh/info fails", func(t *testing.T) {
		t.Setenv("MESHSTACK_SKIP_VERSION_CHECK", "")
		httpClient, transport := newUnreachableClient()
		err := newMeshInfoClient(httpClient).checkMeshVersion(t.Context())
		require.ErrorContains(t, err, "https://meshstack.invalid/mesh/info")
		assert.Equal(t, 1, transport.calls)
	})
}
