package setting_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

// declaration drops the type parameter, because one []setting.Value[T] cannot hold the nine
// different T the declarations produce.
type declaration struct {
	name     string
	envKey   string
	short    string
	hasParse bool
}

func declared[T any](name string, v setting.Value[T]) declaration {
	return declaration{name: name, envKey: v.EnvKey, short: v.Short, hasParse: v.Parse != nil}
}

var declarations = []declaration{
	declared("meshstack.Endpoint", meshstack.Endpoint),
	declared("meshstack.Workspace", meshstack.Workspace),
	declared("credential.ApiKeyId", credential.ApiKeyId),
	declared("credential.ApiSecret", credential.ApiSecret),
	declared("credential.ApiToken", credential.ApiToken),
	declared("profile.Name", profile.Name),
	declared("profile.ConfigFile", profile.ConfigFile),
	declared("profile.CredentialsDir", profile.CredentialsDir),
	declared("tty.NoInput", tty.NoInput),
}

func TestEveryDeclarationIsComplete(t *testing.T) {
	for _, d := range declarations {
		t.Run(d.name, func(t *testing.T) {
			assert.NotEmpty(t, d.short, "Short is what a front end with no markdown renders")
			assert.NotContains(t, d.short, "\n", "Short is one line")
			assert.NotContains(t, d.short, "`", "Short is plain text, and Long is where the markdown goes")
			assert.True(t, d.hasParse, "a nil Parse panics inside setting.Resolve")
			assert.True(t, strings.HasPrefix(d.envKey, "MESHSTACK_"), "EnvKey is %q", d.envKey)
		})
	}
}

func TestEveryEnvKeyIsDeclaredOnce(t *testing.T) {
	declaredBy := make(map[string]string, len(declarations))
	for _, d := range declarations {
		if other, taken := declaredBy[d.envKey]; taken {
			t.Errorf("%s and %s both declare %s, and EnvKey is the setting's identity", other, d.name, d.envKey)
		}
		declaredBy[d.envKey] = d.name
	}
}

func TestNoInputTakesBooleanText(t *testing.T) {
	value, err := tty.NoInput.Parse("true")

	require.NoError(t, err)
	assert.True(t, value)
}

func TestNoInputRejectsTextThatIsNotTrueOrFalse(t *testing.T) {
	_, err := tty.NoInput.Parse("maybe")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "maybe")
	assert.Contains(t, err.Error(), "true, false, 1 and 0", "strconv.ParseBool names none of them")
}

func TestResolveNamesTheVariableWhoseValueWouldNotParse(t *testing.T) {
	_, _, err := setting.Resolve(tty.NoInput, envSource{tty.NoInput.EnvKey: "maybe"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MESHSTACK_NO_INPUT")
}

type envSource map[string]string

func (s envSource) Lookup(key string) (string, bool) {
	value, ok := s[key]
	return value, ok
}

func (s envSource) Describe(key string) string { return "environment " + key }
