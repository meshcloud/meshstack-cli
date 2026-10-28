package apidocs

import (
	gohttp "net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestLoad(t *testing.T) {
	apiDocs, err := os.ReadFile("../../client/openapi/testdata/spec.json")
	require.NoError(t, err)
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{ApiDocs: apiDocs})
	docsSite := fakemeshstack.Start(t, fakemeshstack.Options{ApiDocs: apiDocs})
	docsOfDevelop := devUrl
	devUrl = xurl.MustParsef("%s%s", docsSite.URL, fakemeshstack.ApiDocsPath)
	t.Cleanup(func() { devUrl = docsOfDevelop })
	t.Setenv(urlEnv, "")

	load := func(t *testing.T, dev bool) error {
		t.Helper()
		t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
		_, wait, err := Load(t.Context(), Options{
			Client:   http.NewClient(fakemeshstack.UserAgent),
			Endpoint: xurl.MustParsef("%s", meshStack.URL),
			Dev:      dev,
		})
		wait()
		return err
	}
	answer := func(status int) {
		meshStack.Route("GET "+fakemeshstack.MeshStackApiDocsPath, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(status)
		})
	}
	paths := func(s *fakemeshstack.Server) (paths []string) {
		for _, request := range s.TakeRequests() {
			paths = append(paths, request.URL.Path)
		}
		return paths
	}

	t.Run("reads the API docs of the meshStack it acts against", func(t *testing.T) {
		require.NoError(t, load(t, false))

		assert.Equal(t, []string{fakemeshstack.MeshStackApiDocsPath}, paths(meshStack))
		assert.Empty(t, paths(docsSite))
	})

	t.Run("reads those of the docs site where the meshStack is older than its API docs", func(t *testing.T) {
		for _, status := range []int{gohttp.StatusUnauthorized, gohttp.StatusNotFound} {
			answer(status)

			require.NoError(t, load(t, false))

			assert.Equal(t, []string{fakemeshstack.MeshStackApiDocsPath}, paths(meshStack))
			assert.Equal(t, []string{fakemeshstack.ApiDocsPath}, paths(docsSite))
		}
	})

	t.Run("fails where the meshStack refuses its API docs otherwise", func(t *testing.T) {
		answer(gohttp.StatusForbidden)

		require.ErrorContains(t, load(t, false), "403")

		assert.Empty(t, paths(docsSite))
	})

	t.Run("reads those of the docs site with --dev, without asking the meshStack", func(t *testing.T) {
		meshStack.TakeRequests()

		require.NoError(t, load(t, true))

		assert.Empty(t, paths(meshStack))
		assert.Equal(t, []string{fakemeshstack.ApiDocsPath}, paths(docsSite))
	})
}
