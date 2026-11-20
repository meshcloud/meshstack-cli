package hal_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/hal"
)

func TestLinksFollowALinkAndGiveTheLinksOfTheAnswerTheSameAction(t *testing.T) {
	var followed []string
	action := func(_ context.Context, method, href string, _ ...http.RequestOption) (io.ReadCloser, error) {
		followed = append(followed, method+" "+href)
		return io.NopCloser(strings.NewReader(`{"_links": {"next": {"href": "/second"}}}`)), nil
	}
	type page struct {
		Links hal.Links `json:"_links"`
	}
	links := hal.Links{"first": hal.NewLink("/first", action)}

	answer, err := links.Get[page](t.Context(), "first")
	require.NoError(t, err)
	_, err = answer.Links.Post[page](t.Context(), "next", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"GET /first", "POST /second"}, followed)

	require.ErrorIs(t, links.Delete(t.Context(), "missing"), hal.ErrNoLink)
}
