package setting

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mapSource struct {
	name    string
	values  map[string]string
	lookups int
}

func (s *mapSource) Lookup(key string) (string, bool) {
	s.lookups++
	v, ok := s.values[key]
	return v, ok
}

func (s *mapSource) Describe(key string) string { return s.name + " " + key }

var endpoint = Value[string]{
	EnvKey: "MESHSTACK_ENDPOINT",
	Short:  "the meshStack API endpoint",
	Parse:  Text,
}

var port = Value[int]{
	EnvKey: "MESHSTACK_PORT",
	Short:  "the port to listen on",
	Parse:  strconv.Atoi,
}

func TestResolve(t *testing.T) {
	t.Run("the first hit wins and no later source is consulted", func(t *testing.T) {
		first := &mapSource{name: "flag", values: map[string]string{endpoint.EnvKey: "https://first.example.com"}}
		second := &mapSource{name: "env", values: map[string]string{endpoint.EnvKey: "https://second.example.com"}}

		value, from, err := Resolve(endpoint, first, second)

		require.NoError(t, err)
		assert.Equal(t, "https://first.example.com", value)
		assert.Same(t, first, from)
		assert.Zero(t, second.lookups, "a source below the winner is never asked")
	})

	t.Run("a nil source is skipped", func(t *testing.T) {
		env := &mapSource{name: "env", values: map[string]string{endpoint.EnvKey: "https://env.example.com"}}

		value, from, err := Resolve(endpoint, nil, env)

		require.NoError(t, err)
		assert.Equal(t, "https://env.example.com", value)
		assert.Same(t, env, from)
	})

	t.Run("an empty hit has not answered", func(t *testing.T) {
		unset := &mapSource{name: "flag", values: map[string]string{endpoint.EnvKey: ""}}
		env := &mapSource{name: "env", values: map[string]string{endpoint.EnvKey: "https://env.example.com"}}

		value, from, err := Resolve(endpoint, unset, env)

		require.NoError(t, err)
		assert.Equal(t, "https://env.example.com", value, "an unset flag does not silence the environment below it")
		assert.Same(t, env, from)
	})

	t.Run("nothing configured is the zero value, no source and no error", func(t *testing.T) {
		empty := &mapSource{name: "env", values: map[string]string{}}

		value, from, err := Resolve(endpoint, empty, nil)

		require.NoError(t, err, `"not configured" is the caller's message to write, not this function's error`)
		assert.Empty(t, value)
		assert.Nil(t, from)
	})
}

func TestResolveReportsWhichSourceCarriedAValueThatWillNotParse(t *testing.T) {
	env := &mapSource{name: "env", values: map[string]string{port.EnvKey: "eight"}}

	value, from, err := Resolve(port, env)

	require.Error(t, err)
	assert.Zero(t, value)
	assert.Same(t, env, from, "the source that answered is the one the message blames")
	assert.Contains(t, err.Error(), "invalid "+port.EnvKey)
	assert.Contains(t, err.Error(), "env "+port.EnvKey)
	assert.Contains(t, err.Error(), "eight", "the parse error is wrapped, so its own detail survives")

	var numErr *strconv.NumError
	assert.ErrorAs(t, err, &numErr, "the parse error stays reachable through errors.As")
}

func TestHelpFallsBackToShort(t *testing.T) {
	assert.Equal(t, endpoint.Short, endpoint.Help())
	assert.Equal(t, "# The endpoint", Value[string]{Short: "the endpoint", Long: "# The endpoint"}.Help())
}

func TestTextTrimsSurroundingWhitespace(t *testing.T) {
	value, err := Text("  https://api.example.com \n")

	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", value)
}
