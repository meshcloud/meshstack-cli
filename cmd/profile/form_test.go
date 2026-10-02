package profile

import (
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestTheSuggestedNameIsTheHostWithoutTheLabelsEveryMeshStackHas(t *testing.T) {
	profiles := profile.Profiles{Profiles: map[profile.Name]*profile.Profile{"dev-local": {}, "dev-local-2": {}, "demo-meshcloud": {}}}
	for endpoint, want := range map[string]string{
		"https://federation.acme.meshcloud.io": "acme-meshcloud",
		"https://api.acme.example.de":          "acme-example",
		"https://meshstack.acme.com:8443":      "meshstack-acme",
		"https://localhost":                    "localhost",
		"https://federation.demo.meshcloud.io": "demo-meshcloud-2",
		"http://localhost:8080":                "dev-local-3",
		"not a url":                            "",
	} {
		assert.Equal(t, want, (&draft{endpoint: endpoint}).suggestedName(profiles), endpoint)
	}
}

func TestTheLocalEndpointIsTheLastSuggestion(t *testing.T) {
	profiles := profile.Profiles{Profiles: map[profile.Name]*profile.Profile{
		"b": {Endpoint: endpointB}, "local": {Endpoint: xurl.MustParsef(localEndpoint)}, "a": {Endpoint: endpointA},
	}}

	assert.Equal(t, []string{endpointA.String(), endpointB.String(), localEndpoint}, (&draft{}).questions(profiles)[0].suggestions)
}

func TestTheWorkspacesThatTheProfilesAtTheEndpointReachAreSuggested(t *testing.T) {
	apiKey := fakemeshstack.ApiKey{ClientId: "11111111-45bf-42ba-a965-2097b9d0d181", ClientSecret: "test-secret"}
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		ApiKeys: []fakemeshstack.ApiKey{apiKey},
		Workspaces: []any{
			map[string]any{"metadata": map[string]string{"name": "platform-team"}},
			map[string]any{"metadata": map[string]string{"name": "app-team"}},
		},
	})
	endpoint := xurl.MustParsef("%s", server.URL)
	profiles := storedProfiles(t,
		profile.Profile{Name: "ci", Endpoint: endpoint, Credential: credential.ApiKeyName},
		profile.Profile{Name: "logged-out", Endpoint: endpoint},
		profile.Profile{Name: "elsewhere", Endpoint: endpointA, Credential: credential.ManualName},
	)
	credentials, err := profiles.Profiles["ci"].Credentials(t.Context())
	require.NoError(t, err)
	credentials.Set(&credential.ApiKey{Endpoint: endpoint, ClientId: uuid.MustParse(apiKey.ClientId), ClientSecret: apiKey.ClientSecret})
	require.NoError(t, credentials.Store(t.Context()))

	// The logged-out profile is asked as well, and fails without a call.
	assert.Equal(t, []string{"app-team", "platform-team"}, knownWorkspaces(t.Context(), profiles.MatchingEndpoint(endpoint)))
	assert.Empty(t, profiles.MatchingEndpoint(xurl.MustParsef("https://unknown.example.io")))
	assert.Equal(t, int64(1), server.Counts().Logins)
}
