package auth_test

import (
	"context"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

func TestLoginRefusesAnUnknownCredential(t *testing.T) {
	newTestServer(t)

	_, _, _, err := auth.Login(t.Context(), "nope", testSessionOpts)
	require.ErrorContains(t, err, "cannot authenticate with credential 'nope'; pick one of [apiKey manual oidcLogin]")

	profiles, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{ExclusiveLock: true})
	require.NoError(t, err, "a login that fails holds no lock on the profiles")
	require.NoError(t, profiles.Unlock())
}

// The source stands in for the workspace selection of 'meshstack login', which needs the list.
func TestLoginListsWorkspacesWithoutWaitingOnItsOwnWorkspace(t *testing.T) {
	server := newTestServer(t)
	server.Route("GET /api/meshobjects/meshworkspaces", gohttp.NotFound)
	setApiKeyEnv(t, testApiKey1)
	opts := testSessionOpts
	opts.SettingSources = setting.Sources{setting.FallbackSource{Source: setting.LookupSource{
		MatchingKey: meshstack.WorkspaceSetting.EnvKey(),
		Description: "a source that needs the workspace list",
		Func: func(ctx context.Context) (string, error) {
			_, err := meshstack.WorkspacesFromContext(ctx)
			return "", err
		},
	}}}
	_, store, unlock, err := auth.Login(t.Context(), credential.ApiKeyName, opts)
	require.NoError(t, err)

	stored := make(chan error, 1)
	go func() { stored <- store(t.Context()) }()
	select {
	case err := <-stored:
		require.Error(t, err)
		require.NotErrorAs(t, err, &meshstack.NoWorkspaceToWorkInError{})
	case <-time.After(10 * time.Second):
		t.Fatal("the store is still waiting for the workspace list")
	}
	require.NoError(t, unlock())
}
