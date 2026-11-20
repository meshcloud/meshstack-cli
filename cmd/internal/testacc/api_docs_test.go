package testacc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	gohttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Shopify/toxiproxy/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

var pathParameterRe = regexp.MustCompile(`\{([^}]+)}`)

const fallbackWarning = "which may not match it"

func TestAccApiDocs(t *testing.T) {
	endpoint := requireLocalStack(t)
	proxy := newEndpointProxy(t, endpoint)
	c := newCLI(t, proxy.endpoint)
	var spec openapi.Spec

	t.Run("with the endpoint unreachable and nothing stored, api-docs reads the published docs of develop", func(t *testing.T) {
		proxy.breakConnections(t)
		defer proxy.heal(t)

		output, err := c.run("", "api-docs", "--dev")

		require.NoErrorf(t, err, "output:\n%s", output)
		assert.Contains(t, output, "https://docs.dev.meshcloud.io/api/meshstack-openapi-docs.json, "+fallbackWarning)
		assert.Empty(t, c.storedApiDocs(t), "the published docs are not stored")
	})

	t.Run("api-docs stores the docs of the endpoint under their hash, and asks for newer ones with their ETag", func(t *testing.T) {
		output, err := c.run("", "api-docs")

		require.NoErrorf(t, err, "output:\n%s", output)
		assert.NotContains(t, output, "level=WARN")
		document := servedApiDocs(t, endpoint)
		spec, err = openapi.Parse(bytes.NewReader(document))
		require.NoError(t, err)
		hash := sha256.Sum256(document)
		assert.Equal(t, []string{hex.EncodeToString(hash[:]) + ".json"}, c.storedApiDocs(t))
		index := c.apiDocsIndex(t)
		assert.Equal(t, proxy.endpoint, index.Endpoint)
		assert.Equal(t, hex.EncodeToString(hash[:]), index.Document)
		require.NotEmptyf(t, index.ETag, "%s/api/openapi sends no ETag, so every run downloads the docs again; "+
			"meshfed-release 3a2224431fa adds it", endpoint)

		// A 304 fits into the limit, and the document does not.
		proxy.addToxic(t, "limit_data", "downstream", map[string]any{"bytes": 8192})
		defer proxy.heal(t)
		output, err = c.run("", "api-docs")

		require.NoErrorf(t, err, "output:\n%s", output)
		assert.NotContains(t, output, "level=WARN", "the stored docs are up to date")
	})

	t.Run("with the endpoint unreachable, api-docs reads the docs stored of it", func(t *testing.T) {
		proxy.breakConnections(t)
		defer proxy.heal(t)

		output, err := c.run("", "api-docs")

		require.NoErrorf(t, err, "output:\n%s", output)
		assert.Contains(t, output, "so this command reads the ones stored before")
		assert.NotContains(t, output, fallbackWarning)
	})

	t.Run("every operation has a doc whose fields and parameters have a type, and documents the parameters of its path", func(t *testing.T) {
		require.NotEmpty(t, spec.Operations)
		for _, operation := range spec.Operations {
			doc, err := spec.Doc(operation)
			require.NoErrorf(t, err, "%s %s", operation.Method, operation.PathTemplate)
			var documented []string
			for _, parameter := range doc.Parameters {
				assert.NotEmptyf(t, parameter.Type, "%s %s: parameter %s", operation.Method, operation.PathTemplate, parameter.Name)
				if parameter.In == "path" {
					documented = append(documented, parameter.Name)
				}
			}
			for _, match := range pathParameterRe.FindAllStringSubmatch(string(operation.PathTemplate), -1) {
				assert.Containsf(t, documented, match[1], "%s %s", operation.Method, operation.PathTemplate)
			}
			for _, body := range slices.Concat(doc.RequestBodies, responseBodies(doc)) {
				for _, field := range body.Fields {
					assert.NotRegexpf(t, `^$| of$`, field.Type, "%s %s: %s field %s", operation.Method, operation.PathTemplate, body.MediaType, field.Path)
				}
			}
		}
	})

	t.Run("every meshObject operation has an API version, except a delete and an artifact", func(t *testing.T) {
		for _, operation := range spec.Operations {
			if !strings.HasPrefix(string(operation.PathTemplate), "/api/meshobjects/") || operation.Method == gohttp.MethodDelete ||
				slices.ContainsFunc(operation.MediaTypes, func(m openapi.MediaType) bool { return m.Name == "application/octet-stream" }) {
				continue
			}
			assert.Falsef(t, operation.LatestApiVersion().IsZero(), "%s %s", operation.Method, operation.PathTemplate)
		}
	})

	t.Run("api-docs describes every kind", func(t *testing.T) {
		kinds := spec.Kinds()
		require.NotEmpty(t, kinds)
		for _, kind := range kinds {
			output, err := c.run("", "api-docs", "--describe", string(internal.KindCommandOf(kind)))
			require.NoErrorf(t, err, "%s, output:\n%s", kind, output)
		}
	})
}

func responseBodies(doc openapi.OperationDoc) []openapi.Body {
	var bodies []openapi.Body
	for _, response := range doc.Responses {
		bodies = append(bodies, response.Bodies...)
	}
	return bodies
}

func servedApiDocs(t *testing.T, endpoint string) []byte {
	t.Helper()
	req, err := gohttp.NewRequestWithContext(t.Context(), gohttp.MethodGet, endpoint+"/api/openapi", nil)
	require.NoError(t, err)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	document, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return document
}

func (c *cli) storedApiDocs(t *testing.T) []string {
	t.Helper()
	documents, err := filepath.Glob(filepath.Join(c.configDir, "api-docs", "*.json"))
	require.NoError(t, err)
	for i, document := range documents {
		documents[i] = filepath.Base(document)
	}
	return documents
}

type apiDocsIndex struct {
	Endpoint string `json:"endpoint"`
	Document string `json:"document"`
	ETag     string `json:"etag"`
}

func (c *cli) apiDocsIndex(t *testing.T) apiDocsIndex {
	t.Helper()
	indexes, err := filepath.Glob(filepath.Join(c.configDir, "api-docs", "endpoints", "*.json"))
	require.NoError(t, err)
	require.Len(t, indexes, 1)
	content, err := os.ReadFile(indexes[0])
	require.NoError(t, err)
	var index apiDocsIndex
	require.NoError(t, json.Unmarshal(content, &index))
	return index
}

type endpointProxy struct {
	proxy    *toxiproxy.Proxy
	endpoint string
}

func newEndpointProxy(t *testing.T, endpoint string) endpointProxy {
	t.Helper()
	upstream, err := url.Parse(endpoint)
	require.NoError(t, err)
	proxy := toxiproxy.NewProxy(toxiproxy.NewServer(toxiproxy.NewMetricsContainer(nil), zerolog.Nop()), t.Name(), "127.0.0.1:0", upstream.Host)
	require.NoError(t, proxy.Start())
	t.Cleanup(proxy.Stop)
	return endpointProxy{proxy: proxy, endpoint: "http://" + proxy.Listen}
}

func (p endpointProxy) addToxic(t *testing.T, toxicType, stream string, attributes map[string]any) {
	t.Helper()
	toxic, err := json.Marshal(map[string]any{"type": toxicType, "stream": stream, "attributes": attributes})
	require.NoError(t, err)
	_, err = p.proxy.Toxics.AddToxicJson(bytes.NewReader(toxic))
	require.NoError(t, err)
}

func (p endpointProxy) breakConnections(t *testing.T) {
	t.Helper()
	p.addToxic(t, "reset_peer", "upstream", map[string]any{"timeout": 0})
}

func (p endpointProxy) heal(t *testing.T) {
	t.Helper()
	p.proxy.Toxics.ResetToxics(t.Context())
}
