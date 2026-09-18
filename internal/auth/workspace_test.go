package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

func TestAWorkspaceSourceReadsTheResolvedCredentialFromTheContext(t *testing.T) {
	for _, testCase := range []struct {
		resolved credential.Credential
		expected credential.Name
	}{
		{&credential.OidcLogin{}, credential.OidcLoginName},
		{&credential.ApiKey{}, credential.ApiKeyName},
	} {
		t.Run(testCase.expected.String(), func(t *testing.T) {
			session := Session{Credential: testCase.resolved}
			opts := ResolveSessionOptions{SettingSources: setting.Sources{workspaceNamedAfterTheCredential()}}

			workspace, err := session.resolveWorkspace(t.Context(), profile.Profile{}, opts)

			require.NoError(t, err)
			assert.EqualValues(t, testCase.expected, workspace)
		})
	}
}

// workspaceNamedAfterTheCredential answers with the credential it found, so that the workspace the
// resolution returns is what the source read out of the context.
func workspaceNamedAfterTheCredential() setting.FrontendSource {
	return setting.FrontendSource{Source: setting.LookupSource{
		MatchingKey: meshstack.WorkspaceSetting.EnvKey(),
		Description: "the credential in the context",
		Func: func(ctx context.Context) (string, error) {
			name, err := credential.NameFromContext(ctx)
			return name.String(), err
		},
	}}
}
