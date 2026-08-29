package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The devLocalCredentials field is absent from every /mesh/info a user could reach, so the
// case that matters most is the one where nothing is there: the pointer stays nil and
// nothing else in the document changes meaning.
func TestMeshInfoDevLocalCredentials(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     *DevLocalCredentials
	}{
		{
			name:     "an ordinary /mesh/info carries none",
			document: `{"version":"2026.34.0","issuer":"https://login.example.com/auth/realms/meshfed","cliClientId":"meshstack-cli","adminWorkspaceIdentifier":"my-partner"}`,
		},
		{
			name:     "an explicit null carries none either",
			document: `{"version":"2026.34.0","issuer":"https://login.example.com/auth/realms/meshfed","devLocalCredentials":null}`,
		},
		{
			name:     "a local dev stack carries the api key and the seeded logins",
			document: `{"version":"2026.34.0","issuer":"http://localhost:5050/auth/realms/meshfed","cliClientId":"meshstack-cli","devLocalCredentials":{"apiKeyClientId":"37abbe45-aba7-4617-b87d-93f4cbf95832","apiKeyClientSecret":"eUp1jPMfM2RyNOjdVRuLmHGOYCvzZrN5","users":[{"username":"partner@meshcloud.io","password":"sample123","workspace":"demo-partner"},{"username":"customer@meshcloud.io","password":"sample123","workspace":"demo-customer"},{"username":"customer-e@meshcloud.io","password":"sample123"}]}}`,
			want: &DevLocalCredentials{
				ApiKeyClientId:     "37abbe45-aba7-4617-b87d-93f4cbf95832",
				ApiKeyClientSecret: "eUp1jPMfM2RyNOjdVRuLmHGOYCvzZrN5",
				Users: []DevLocalUser{
					{Username: "partner@meshcloud.io", Password: "sample123", Workspace: "demo-partner"},
					{Username: "customer@meshcloud.io", Password: "sample123", Workspace: "demo-customer"},
					// No workspace attribute in keycloak, so a browser login for this one
					// cannot act and the field is absent rather than empty-and-usable.
					{Username: "customer-e@meshcloud.io", Password: "sample123"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info MeshInfo
			require.NoError(t, json.Unmarshal([]byte(tt.document), &info))
			assert.Equal(t, tt.want, info.DevLocalCredentials)
			assert.Equal(t, "2026.34.0", info.Version, "the new field changes nothing about the rest of the document")

			// Round-trip: what this client encodes decodes back to the same thing, and
			// omitempty keeps an absent field absent rather than turning it into a null.
			encoded, err := json.Marshal(info)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &fields))
			_, present := fields["devLocalCredentials"]
			assert.Equal(t, tt.want != nil, present)

			var again MeshInfo
			require.NoError(t, json.Unmarshal(encoded, &again))
			assert.Equal(t, info, again)
		})
	}
}
