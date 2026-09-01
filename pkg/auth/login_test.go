package auth

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/credential"
)

// TestALoginWithNoBrowserFailsAndNamesMeshstackLogin pins the message that makes a nil
// LoginOptions.Browser a supported case rather than a crash.
func TestALoginWithNoBrowserFailsAndNamesMeshstackLogin(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{
		Issuer: mustUrl(stack.URL.String()), RefreshToken: "refresh-old",
	}))
	stack.answerRefreshWith(func(url.Values) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant"}
	})

	session := resolved(t, ResolveSessionOptions{
		DemandMethod: credential.MethodLogin, Store: profileStore(t, testProfile),
	})

	_, err := session.Login(t.Context(), LoginOptions{})

	problem := problemOf(t, err)
	require.Equal(t, "this front end cannot create a login", problem.Summary())
	require.Contains(t, problem.Detail(), "meshstack login")
}
