package markdown

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

var credentialOnly = Parse("test", `{{template "credential" .}}`)

func TestABrowserLoginShowsItsUserAccessAndSessionEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		status := auth.CredentialStatus{
			Profile: "dev", Endpoint: xurl.MustParsef("https://meshstack.example.com"),
			Kind: credential.OidcLoginName, Sources: []string{"file credentials/dev.json"},
			Workspace: "ops",
			Token: &auth.TokenStatus{
				ExpiresAt: time.Now().UTC().Add(-time.Minute), User: "jane", Email: "jane@example.com", AccessLevel: meshstack.AccessRead,
			},
			OidcLogin: &auth.OidcLoginStatus{AccessLevel: meshstack.AccessFull, SessionEndsAt: time.Now().UTC().Add(3 * time.Hour)},
		}

		shown, err := Execute(credentialOnly, status)

		require.NoError(t, err)
		assert.Equal(t, "| Profile | dev |\n"+
			"| --- | --- |\n"+
			"| Endpoint | https://meshstack.example.com |\n"+
			"| Workspace | ops |\n"+
			"| Credential | Browser login, from file credentials/dev.json |\n"+
			"|  | User jane (jane@example.com) with Full access, but the token grants Read-only |\n"+
			"|  | Session ends at the latest 2000-01-01 03:00 (in 3h), then run `meshstack login -p dev` |\n"+
			"|  | Token expired 1999-12-31 23:59 (1m ago), renewed on next use |", shown)
	})
}

func TestAnApiKeyShowsItsPermissionsBelowTheCredential(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clientId := uuid.MustParse("11111111-45bf-42ba-a965-2097b9d0d181")
		status := auth.CredentialStatus{
			Profile: "ci", Endpoint: xurl.MustParsef("https://meshstack.example.com"),
			Kind: credential.ApiKeyName, Sources: []string{"env MESHSTACK_API_KEY", "env MESHSTACK_API_SECRET"},
			ApiKey: &auth.ApiKeyStatus{ClientId: clientId, Details: &auth.ApiKeyDetails{
				DisplayName: "CI | nightly", OwnedByWorkspace: "ops",
				PermissionGroups: client.ApiKeyPermissions{{Name: "Tenants", Actions: [][]client.ApiPermission{{"TENANT_LIST", "ADM_TENANT_LIST"}, {"TENANT_SAVE", "ADM_TENANT_SAVE"}}}},
			}},
			Unused: []auth.UnusedCredential{
				{Kind: credential.ApiKeyName, ClientId: "22222222-45bf-42ba-a965-2097b9d0d181"},
				{Kind: credential.OidcLoginName, Selected: true, Token: &auth.TokenStatus{User: "jane", Email: "jane@example.com"}},
				{Kind: credential.ManualName, Token: &auth.TokenStatus{ExpiresAt: time.Now().UTC().Add(-time.Hour)}},
			},
		}

		shown, err := Execute(credentialOnly, status)

		require.NoError(t, err)
		assert.Equal(t, "| Profile | ci |\n"+
			"| --- | --- |\n"+
			"| Endpoint | https://meshstack.example.com |\n"+
			"| Workspace | none |\n"+
			"| Credential | API key 11111111-45bf-42ba-a965-2097b9d0d181, from env MESHSTACK_API_KEY, env MESHSTACK_API_SECRET |\n"+
			`|  | CI \| nightly, owned by workspace ops, never expires |`+"\n"+
			"|  | No token cached yet, minted on next use |\n"+
			"|  | • Tenants: `[ADM_]TENANT_(LIST\\|SAVE)` |\n"+
			"| Unused credentials | • API key 22222222-45bf-42ba-a965-2097b9d0d181 |\n"+
			"|  | • Browser login jane (jane@example.com), selected by the profile |\n"+
			"|  | • API token, expired 1999-12-31 23:00 (1h ago) |", shown)
	})
}

func TestAnExpiredApiTokenSaysItCannotBeRenewed(t *testing.T) {
	status := auth.CredentialStatus{
		Profile: "run", Endpoint: xurl.MustParsef("https://meshstack.example.com"),
		Kind: credential.ManualName, Sources: []string{"env MESHSTACK_API_TOKEN"},
		Token: &auth.TokenStatus{ExpiresAt: time.Now().Add(-time.Hour), Workspace: "ops", ClientId: "11111111-45bf-42ba-a965-2097b9d0d181"},
	}

	shown, err := Execute(credentialOnly, status)

	require.NoError(t, err)
	assert.Contains(t, shown, "cannot be renewed, run `meshstack login -p run --apitoken`, for workspace ops |")
	assert.Contains(t, shown, "| Credential | API token of API key 11111111-45bf-42ba-a965-2097b9d0d181, from env MESHSTACK_API_TOKEN |")
	assert.NotContains(t, shown, "| Workspace |", "the token carries its workspace")
}

func TestAnApiKeyShowsWhenItExpires(t *testing.T) {
	for _, tt := range []struct {
		name      string
		expiresIn time.Duration
		expiresOn string
		want      string
	}{
		{name: "a key with an expiry time", expiresIn: 30 * 24 * time.Hour, want: "expires 2000-01-31 00:00 (in 30d)"},
		{name: "a meshStack that sends only the expiry date", expiresOn: "2000-01-31", want: "expires on 2000-01-31"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				details := &auth.ApiKeyDetails{DisplayName: "CI", OwnedByWorkspace: "ops", ExpiresOn: tt.expiresOn}
				if tt.expiresIn != 0 {
					details.ExpiresAt = time.Now().UTC().Add(tt.expiresIn)
				}
				status := auth.CredentialStatus{
					Profile: "ci", Endpoint: xurl.MustParsef("https://meshstack.example.com"), Kind: credential.ApiKeyName,
					ApiKey: &auth.ApiKeyStatus{Details: details},
				}

				shown, err := Execute(credentialOnly, status)

				require.NoError(t, err)
				assert.Contains(t, shown, "\n|  | CI, owned by workspace ops, "+tt.want+" |\n")
			})
		})
	}
}

func TestATimeIsShownWithHowFarAwayItIs(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:    "now",
		4 * time.Minute:     "in 4m",
		-3 * time.Hour:      "3h ago",
		47 * time.Hour:      "in 47h",
		-72 * time.Hour:     "3d ago",
		24*time.Hour + 30:   "in 24h",
		90 * 24 * time.Hour: "in 90d",
	} {
		assert.Equal(t, want, relative(d), d.String())
	}
}

func TestRenderWrapsAtMostAtMaxWidthOnAWideTerminal(t *testing.T) {
	rendered, err := Render(strings.Repeat("word ", 100), 300)

	require.NoError(t, err)
	for line := range strings.Lines(rendered) {
		assert.LessOrEqual(t, ansi.StringWidth(strings.TrimSuffix(line, "\n")), maxWidth)
	}
}

func TestRenderFittingWrapsAtTheNarrowestWidthThatWrapsNoMoreThanTheTerminal(t *testing.T) {
	table := "| | Profile | Endpoint | Credential |\n| --- | --- | --- | --- |\n| current | likvid-bank-demo | https://federation.demo.meshcloud.io | Browser login |\n"
	full, err := render(table, 300)
	require.NoError(t, err)

	fitting, err := renderFitting(table, 300)

	require.NoError(t, err)
	assert.Equal(t, strings.Count(full, "\n"), strings.Count(fitting, "\n"), "the URL does not wrap")
	assert.Contains(t, ansi.Strip(fitting), "Browser login", "no cell is cut off")
	assert.Less(t, widestLine(fitting), maxWidth, "the table does not stretch across the terminal")

	fitting, err = renderFitting(strings.Repeat("word ", 100), 300)
	require.NoError(t, err)
	assert.Greater(t, widestLine(fitting), maxWidth, "a long line takes what the terminal has")
}

func widestLine(rendered string) int {
	widest := 0
	for line := range strings.Lines(rendered) {
		widest = max(widest, ansi.StringWidth(strings.TrimSuffix(line, "\n")))
	}
	return widest
}

func TestRenderInlineStylesOneLineWithoutMarginOrPadding(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "dark")

	rendered := RenderInline("**ADMIN** (admin)")

	assert.Contains(t, rendered, "\x1b[", "the bold text is styled")
	assert.Equal(t, "ADMIN (admin)", ansi.Strip(rendered))
}
