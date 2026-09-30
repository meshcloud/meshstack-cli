package auth

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
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
			wantAsked: "  [1] first  (https://a.example.io)\n *[2] second (https://b.example.io)\n  [3] third  (https://a.example.io)\n" +
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

func TestWorkspaceSelection(t *testing.T) {
	workspaces := meshstack.Workspaces{Items: []client.MeshWorkspace{
		{Metadata: client.MeshWorkspaceMetadata{Name: "first"}, Spec: client.MeshWorkspaceSpec{DisplayName: "First"}},
		{Metadata: client.MeshWorkspaceMetadata{Name: "second"}, Spec: client.MeshWorkspaceSpec{DisplayName: "Second"}},
	}}
	const asked = "  [1] First  (first)\n  [2] Second (second)\nSelect a workspace [1-2]: "

	// An input that stays open with nothing on it, as the stdin of some scripts does.
	silent := func(t *testing.T) io.Reader {
		t.Helper()
		reader, writer := io.Pipe()
		t.Cleanup(func() { _ = writer.Close() })
		return reader
	}

	tests := []struct {
		name     string
		optional bool
		answers  func(t *testing.T) io.Reader
		want     string
		wantErr  string
	}{
		{name: "an answer selects", answers: answer("2"), want: "second"},
		{name: "an optional selection answered selects as well", optional: true, answers: answer("2"), want: "second"},
		{name: "an input that ends is an error", answers: answer(""), wantErr: "nothing was entered for the workspace selection"},
		{name: "an optional selection takes an input that ends as none", optional: true, answers: answer("")},
		{name: "an input with no answer in time is an error", answers: silent, wantErr: context.DeadlineExceeded.Error()},
		{name: "an optional selection fails as well with no answer in time", optional: true, answers: silent, wantErr: context.DeadlineExceeded.Error()},
	}
	for _, tt := range tests {
		// The fake clock of synctest runs out the answer time of the prompt at once.
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output bytes.Buffer

				source := newWorkspaceSelectionSource(prompt.New(tt.answers(t), &output), tt.optional)
				ctx := meshstack.SetWorkspacesInContext(t.Context(), func() (meshstack.Workspaces, error) {
					return workspaces, nil
				})

				selected, err := source.Lookup(ctx, setting.Workspace.EnvKey())

				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, tt.want, selected)
				assert.Equal(t, asked, output.String())
			})
		})
	}
}

func answer(line string) func(*testing.T) io.Reader {
	return func(*testing.T) io.Reader {
		if line == "" {
			return strings.NewReader("")
		}
		return strings.NewReader(line + "\n")
	}
}
