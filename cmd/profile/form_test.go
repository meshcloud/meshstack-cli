package profile

import (
	"fmt"
	gohttp "net/http"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testserver"
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
		assert.Equal(t, want, suggestedName(profiles, endpoint), endpoint)
	}
}

func TestTheLocalEndpointIsTheLastSuggestion(t *testing.T) {
	profiles := profile.Profiles{Profiles: map[profile.Name]*profile.Profile{
		"b": {Endpoint: endpointB}, "local": {Endpoint: xurl.MustParsef(localEndpoint)}, "a": {Endpoint: endpointA},
	}}

	assert.Equal(t, []string{endpointA.String(), endpointB.String(), localEndpoint}, knownEndpoints(profiles))
}

func TestTheWorkspacesThatTheProfilesAtTheEndpointReachAreSuggested(t *testing.T) {
	apiKey := testserver.ApiKey{ClientId: "11111111-45bf-42ba-a965-2097b9d0d181", ClientSecret: "test-secret"}
	server := testserver.New(t, apiKey)
	server.Route(t, "/api/meshobjects/meshworkspaces", func(resp gohttp.ResponseWriter, req *gohttp.Request) {
		if !assert.NotEmpty(t, req.Header.Get("Authorization")) {
			resp.WriteHeader(gohttp.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(resp, `{"_embedded":{"meshWorkspaces":[{"metadata":{"name":"platform-team"}},{"metadata":{"name":"app-team"}}]},`+
			`"page":{"totalPages":1,"number":0}}`)
	})
	endpoint := xurl.URL{URL: server.Url(t)}
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
	assert.Equal(t, []string{"app-team", "platform-team"}, knownWorkspaces(t.Context(), profilesAt(profiles, endpoint.String())))
	assert.Empty(t, profilesAt(profiles, "https://unknown.example.io"))
	assert.Equal(t, int64(1), server.Counts(t).Logins)
}
