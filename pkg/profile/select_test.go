package profile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

type flags map[string]string

func (f flags) Lookup(key string) (string, bool) {
	value, ok := f[key]
	return value, ok && value != ""
}

func (flags) Describe(key string) string { return "--" + key }

func configured(t *testing.T) {
	t.Helper()
	require.NoError(t, SaveConfig(Config{
		CurrentProfile: "current",
		Profiles: map[string]Profile{
			"named":   {Endpoint: mustUrl("https://named.example.com")},
			"live":    {Endpoint: mustUrl("https://api.live.example.com")},
			"current": {Endpoint: mustUrl("https://current.example.com")},
		},
	}))
}

// Exists is reported rather than judged, and Named is what tells a typo from a machine
// nobody has configured — the two failures a caller words differently.
func TestSelectReportsHowItGotTheName(t *testing.T) {
	tests := []struct {
		name       string
		flags      flags
		want       string
		wantNamed  bool
		wantExists bool
		wantFrom   string
	}{
		{
			name: "a name from a source above", flags: flags{Name.EnvKey: "named"},
			want: "named", wantNamed: true, wantExists: true, wantFrom: "--" + Name.EnvKey,
		},
		{
			name: "a name that is not configured is still the selection", flags: flags{Name.EnvKey: "typo"},
			want: "typo", wantNamed: true, wantFrom: "--" + Name.EnvKey,
		},
		{
			name: "the only profile for the endpoint", flags: flags{meshstack.Endpoint.EnvKey: "https://api.live.example.com"},
			want: "live", wantExists: true, wantFrom: "the only profile for https://api.live.example.com",
		},
		{
			name: "currentProfile", want: "current", wantExists: true,
			wantFrom: "currentProfile in ",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolate(t)
			configured(t)

			selection, err := Select(t.Context(), test.flags)

			require.NoError(t, err)
			assert.Equal(t, test.want, selection.Name)
			assert.Equal(t, test.wantNamed, selection.Named)
			assert.Equal(t, test.wantExists, selection.Exists)
			require.NotEmpty(t, selection.Origins)
			assert.Equal(t, Name.EnvKey, selection.Origins[0].Key, "the profile is the first thing resolved")
			assert.Contains(t, selection.Origins[0].Source, test.wantFrom)
		})
	}
}

func TestSelectFallsToTheBuiltInDefault(t *testing.T) {
	isolate(t)

	selection, err := Select(t.Context())

	require.NoError(t, err)
	assert.Equal(t, DefaultName, selection.Name)
	assert.False(t, selection.Exists, "a fresh install has no config.json at all")
	assert.Equal(t, []setting.Origin{{Key: Name.EnvKey, Source: "built-in default"}}, selection.Origins)
}

// The endpoint's ranked list is walked here because the profile is what it is being used to
// pick, so the prefix comes back for the caller to finish rather than being walked twice.
func TestSelectReportsTheEndpointASourceAboveNamed(t *testing.T) {
	isolate(t)
	configured(t)

	selection, err := Select(t.Context(), flags{
		Name.EnvKey: "named", meshstack.Endpoint.EnvKey: "https://flag.example.com",
	})

	require.NoError(t, err)
	assert.Equal(t, "https://flag.example.com", selection.Endpoint)
	assert.Equal(t, "https://named.example.com", selection.Entry.Endpoint.String())
	assert.Contains(t, selection.Origins, setting.Origin{
		Key: meshstack.Endpoint.EnvKey, Source: "--" + meshstack.Endpoint.EnvKey,
	})
}

func TestSelectRefusesAnEndpointSeveralProfilesMatch(t *testing.T) {
	isolate(t)
	require.NoError(t, SaveConfig(Config{Profiles: map[string]Profile{
		"alpha": {Endpoint: mustUrl("https://api.live.example.com")},
		"beta":  {Endpoint: mustUrl("https://api.live.example.com/")},
	}}))

	_, err := Select(t.Context(), flags{meshstack.Endpoint.EnvKey: "https://api.live.example.com"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"alpha"`)
	assert.Contains(t, err.Error(), `"beta"`)
	assert.Contains(t, err.Error(), "--profile")
}
