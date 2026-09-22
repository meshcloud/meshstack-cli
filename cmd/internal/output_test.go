package internal

import (
	"bytes"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type logStep struct {
	DisplayName   string `json:"displayName"`
	SystemMessage string `json:"systemMessage"`
}

func writeOne(t *testing.T, format OutputFormat, item logStep) string {
	t.Helper()
	var out bytes.Buffer
	one := iter.Seq2[logStep, error](func(yield func(logStep, error) bool) { yield(item, nil) })
	require.NoError(t, WriteList(&out, format, one))
	return out.String()
}

// A step's log is multi-line and often opens with terraform's `{` banner, which is what makes the
// difference between a block scalar and one escaped line.
func TestWriteListYamlWritesAMultilineStringAsABlockScalar(t *testing.T) {
	document := writeOne(t, OutputYaml, logStep{
		DisplayName:   "Run Terraform",
		SystemMessage: "{\n  \"terraform_version\": \"1.9.0\"\n}\n\nInitializing the backend...\n",
	})

	assert.Equal(t, `displayName: Run Terraform
systemMessage: |
  {
    "terraform_version": "1.9.0"
  }
  
  Initializing the backend...
`, document)
}

func TestWriteListYamlKeepsTheMemberOrderOfTheJson(t *testing.T) {
	document := writeOne(t, OutputYaml, logStep{DisplayName: "Initialize Inputs", SystemMessage: ""})

	assert.Equal(t, "displayName: Initialize Inputs\nsystemMessage: \"\"\n", document)
}
