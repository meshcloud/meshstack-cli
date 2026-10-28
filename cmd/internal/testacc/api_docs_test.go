package testacc

import (
	gohttp "net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/openapi"
)

var pathParameterRe = regexp.MustCompile(`\{([^}]+)}`)

func TestAccApiDocs(t *testing.T) {
	requireLocalStack(t)
	spec := devApiDocs(t)
	require.NotEmpty(t, spec.Operations)

	t.Run("every operation documents the parameters of its path", func(t *testing.T) {
		for _, operation := range spec.Operations {
			doc, err := spec.Doc(operation)
			require.NoError(t, err)
			var documented []string
			for _, parameter := range doc.Parameters {
				if parameter.In == "path" {
					documented = append(documented, parameter.Name)
				}
			}
			for _, match := range pathParameterRe.FindAllStringSubmatch(string(operation.PathTemplate), -1) {
				assert.Containsf(t, documented, match[1], "%s %s", operation.Method, operation.PathTemplate)
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
}
