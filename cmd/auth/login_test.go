package auth

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func TestProfileSelection(t *testing.T) {
	var (
		endpointA = xurl.MustParsef("https://a.example.io")
		endpointB = xurl.MustParsef("https://b.example.io")
		unknown   = xurl.MustParsef("https://unknown.example.io")
		first     = &profile.Profile{Name: "first", Endpoint: endpointA}
		second    = &profile.Profile{Name: "second", Endpoint: endpointB}
		third     = &profile.Profile{Name: "third", Endpoint: endpointA}
		all       = []*profile.Profile{first, second, third}
	)

	tests := []struct {
		name      string
		selection profile.Selection
		stdin     bool
		answers   string
		want      string
		wantErr   string
		wantAsked string
	}{
		{
			name:      "no stored profile leaves the name to the default",
			selection: profile.Selection{},
		},
		{
			name:      "a single profile is taken without asking",
			selection: profile.Selection{Profiles: []*profile.Profile{second}, Current: "second"},
			want:      "second",
		},
		{
			name:      "several profiles are asked for, and Enter takes the current one",
			selection: profile.Selection{Profiles: all, Current: "second"},
			answers:   "\n",
			want:      "second",
			wantAsked: "  [1] first (https://a.example.io)\n *[2] second (https://b.example.io)\n  [3] third (https://a.example.io)\n" +
				"Select a profile [1-3, default=2]: ",
		},
		{
			name:      "only the profiles for the endpoint are offered",
			selection: profile.Selection{Profiles: all, Current: "second", Endpoint: &endpointA},
			answers:   "not a number\n2\n",
			want:      "third",
			wantAsked: "  [1] first (https://a.example.io)\n  [2] third (https://a.example.io)\n" +
				"Select a profile [1-2]: Answer with a number between 1 and 2.\nSelect a profile [1-2]: ",
		},
		{
			name:      "an endpoint no profile is for suggests creating one",
			selection: profile.Selection{Profiles: []*profile.Profile{second}, Current: "second", Endpoint: &unknown},
			wantErr: "no profile is for endpoint 'https://unknown.example.io', only second (https://b.example.io); " +
				"create a new profile for it with `meshstack login --endpoint https://unknown.example.io --profile <new profile name>`",
		},
		{
			name:      "with --stdin the current profile is taken without asking",
			selection: profile.Selection{Profiles: all, Current: "third", Endpoint: &endpointA},
			stdin:     true,
			want:      "third",
		},
		{
			name:      "with --stdin a current profile for another endpoint is an error",
			selection: profile.Selection{Profiles: all, Current: "second", Endpoint: &endpointA},
			stdin:     true,
			wantErr:   "name one with --profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked bytes.Buffer

			source := newProfileSelectionSource(prompt.New(strings.NewReader(tt.answers), &asked), tt.stdin)
			ctx := profile.SetSelectionInContext(t.Context(), func() (profile.Selection, error) {
				return tt.selection, nil
			})

			selected, err := source.Lookup(ctx, setting.Profile.EnvKey())

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, selected)
			assert.Equal(t, tt.wantAsked, asked.String())
		})
	}
}
