package profile

import (
	"bytes"
	"cmp"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

// execute runs meshstack profile with args, answering from input, which is no terminal.
func execute(t *testing.T, input string, args ...string) (output string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "meshstack", SilenceUsage: true, SilenceErrors: true}
	for _, flag := range []*internal.Flag[string]{&internal.ProfileFlag, &internal.EndpointFlag, &internal.WorkspaceFlag} {
		flag.Value = ""
		flag.Register(root.PersistentFlags())
	}
	root.AddCommand(New())
	var out bytes.Buffer
	root.SetIn(strings.NewReader(input))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"profile"}, args...))
	err = root.ExecuteContext(t.Context())
	return out.String(), err
}

func TestProfileWithoutATerminalShowsTheHelp(t *testing.T) {
	twoProfiles(t)

	output, err := execute(t, "")

	require.NoError(t, err)
	assert.Contains(t, output, "Available Commands:")
}

func TestAddAsksLineByLineWithTheFlagsAsDefaults(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		answers   string
		wantErr   string
		wantAsked string
		want      *profile.Profile
	}{
		{
			name:      "every field is asked for",
			answers:   "https://c.example.io\n\nstaging\n",
			wantAsked: "Endpoint: Default workspace: Name [c-example]: ",
			want:      &profile.Profile{Name: "staging", Endpoint: endpointC},
		},
		{
			name:      "the endpoint suggests the name",
			answers:   "https://api.c.example.io\n\n\n",
			wantAsked: "Endpoint: Default workspace: Name [c-example]: ",
			want:      &profile.Profile{Name: "c-example", Endpoint: xurl.MustParsef("https://api.c.example.io")},
		},
		{
			name:      "a local endpoint suggests dev-local",
			answers:   "http://localhost:8080\n\n\n",
			wantAsked: "Endpoint: Default workspace: Name [dev-local]: ",
			want:      &profile.Profile{Name: "dev-local", Endpoint: xurl.MustParsef("http://localhost:8080")},
		},
		{
			name:      "the flags give the defaults",
			args:      []string{"--profile", "staging", "--endpoint", "https://c.example.io", "--workspace", "ws"},
			answers:   "\n\n\n",
			wantAsked: "Endpoint [https://c.example.io]: Default workspace [ws]: Name [staging]: ",
			want:      &profile.Profile{Name: "staging", Endpoint: endpointC, DefaultWorkspace: "ws"},
		},
		{
			name:    "an answer that does not fit is asked again",
			answers: "\nhttp://c.example.io\nhttps://c.example.io\n\nprod\nbad name\nstaging\n",
			wantAsked: "Endpoint: a profile needs an endpoint\n" +
				"Endpoint: URLs must start with 'https://' unless the host is localhost or another loopback address\n" +
				"Endpoint: Default workspace: Name [c-example]: a profile named 'prod' exists already\n" +
				"Name [c-example]: a profile name must match ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$\n" +
				"Name [c-example]: ",
			want: &profile.Profile{Name: "staging", Endpoint: endpointC},
		},
		{
			name:      "an input that ends takes the defaults",
			args:      []string{"--profile", "staging", "--endpoint", "https://c.example.io", "--workspace", "ws"},
			wantAsked: "Endpoint [https://c.example.io]: Default workspace [ws]: Name [staging]: ",
			want:      &profile.Profile{Name: "staging", Endpoint: endpointC, DefaultWorkspace: "ws"},
		},
		{
			name:      "an input that ends takes the suggested name",
			args:      []string{"--endpoint", "https://c.example.io"},
			wantAsked: "Endpoint [https://c.example.io]: Default workspace: Name [c-example]: ",
			want:      &profile.Profile{Name: "c-example", Endpoint: endpointC},
		},
		{
			name:      "an input that ends where a question has no valid default adds nothing",
			wantErr:   "nothing was entered for the endpoint: the prompt reached the end of its input; a profile needs an endpoint",
			wantAsked: "Endpoint: ",
		},
		{
			name:      "an input that ends at a name that is taken adds nothing",
			args:      []string{"--profile", "prod", "--endpoint", "https://c.example.io"},
			wantErr:   "nothing was entered for the name: the prompt reached the end of its input; a profile named 'prod' exists already",
			wantAsked: "Endpoint [https://c.example.io]: Default workspace: Name [prod]: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			twoProfiles(t)

			output, err := execute(t, tt.answers, append([]string{"add"}, tt.args...)...)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAsked, output)
			reloaded, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
			require.NoError(t, err)
			if tt.want == nil {
				assert.Equal(t, []profile.Name{"dev", "prod"}, names(reloaded))
				return
			}
			require.Contains(t, reloaded.Profiles, tt.want.Name)
			added := reloaded.Profiles[tt.want.Name]
			assert.Equal(t, tt.want.Endpoint, added.Endpoint)
			assert.Equal(t, tt.want.DefaultWorkspace, added.DefaultWorkspace)
		})
	}
}

func TestEditAsksLineByLineWhereAnEmptyAnswerKeepsTheValue(t *testing.T) {
	const askedForProd = "| Profile | prod |\n| --- | --- |\n| Endpoint | https://b.example.io |\n| Default workspace | ops |\n" +
		"| Credential | none, run `meshstack login -p prod` |\n\n" +
		"Endpoint [https://b.example.io]: Default workspace [ops]: Name [prod]: "
	tests := []struct {
		name          string
		args          []string
		answers       string
		wantAsked     string
		wantWorkspace string
	}{
		{
			name:          "--profile names the profile, and an empty answer keeps a value",
			args:          []string{"--profile", "prod"},
			answers:       "\nplatform\n\n",
			wantAsked:     askedForProd,
			wantWorkspace: "platform",
		},
		{
			name:          "--workspace gives the default answer",
			args:          []string{"--profile", "prod", "--workspace", "platform"},
			answers:       "\n\n\n",
			wantAsked:     strings.Replace(askedForProd, "Default workspace [ops]", "Default workspace [platform]", 1),
			wantWorkspace: "platform",
		},
		{
			name:          "without --profile it is selected",
			answers:       "2\n\n\n\n",
			wantAsked:     " *[1] dev  (https://a.example.io)\n  [2] prod (https://b.example.io)\nSelect a profile [1-2, default=1]: " + askedForProd,
			wantWorkspace: "ops",
		},
		{
			name:          "--endpoint selects the only profile for it",
			args:          []string{"--endpoint", "https://b.example.io"},
			answers:       "\n\n\n",
			wantAsked:     askedForProd,
			wantWorkspace: "ops",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			twoProfiles(t)

			output, err := execute(t, tt.answers, append([]string{"edit"}, tt.args...)...)

			require.NoError(t, err)
			assert.Equal(t, tt.wantAsked, output)
			reloaded, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
			require.NoError(t, err)
			assert.Equal(t, endpointB, reloaded.Profiles["prod"].Endpoint)
			assert.Equal(t, tt.wantWorkspace, string(reloaded.Profiles["prod"].DefaultWorkspace))
		})
	}
}

func TestDeleteAsksUnlessYesGoesWithAFlagThatNamesTheProfile(t *testing.T) {
	const confirmDev = "Delete profile 'dev' and its stored credentials? [y/N]: "
	tests := []struct {
		name        string
		env         string
		endpointEnv string
		args        []string
		answers     string
		wantErr     string
		wantAsked   string
		wantNames   []profile.Name
	}{
		{
			name:      "--profile and a yes delete",
			args:      []string{"--profile", "dev"},
			answers:   "y\n",
			wantAsked: confirmDev,
			wantNames: []profile.Name{"prod"},
		},
		{
			name:      "any other answer keeps it",
			args:      []string{"--profile", "dev"},
			answers:   "\n",
			wantAsked: confirmDev,
			wantNames: []profile.Name{"dev", "prod"},
		},
		{
			name:      "--yes deletes without asking",
			args:      []string{"--profile", "dev", "--yes"},
			wantNames: []profile.Name{"prod"},
		},
		{
			name:      "--yes deletes nothing without --profile or --endpoint",
			args:      []string{"--yes"},
			wantErr:   "--yes deletes only the profile --profile or --endpoint names",
			wantNames: []profile.Name{"dev", "prod"},
		},
		{
			name:      "--yes deletes the only profile for --endpoint without asking",
			args:      []string{"--endpoint", "https://a.example.io", "--yes"},
			wantNames: []profile.Name{"prod"},
		},
		{
			name:        "MESHSTACK_ENDPOINT selects the only profile for it, but does not name it",
			endpointEnv: "https://a.example.io",
			answers:     "y\n",
			wantAsked:   confirmDev,
			wantNames:   []profile.Name{"prod"},
		},
		{
			name:        "--profile wins over the endpoint",
			endpointEnv: "https://a.example.io",
			args:        []string{"--profile", "prod", "--yes"},
			wantNames:   []profile.Name{"dev"},
		},
		{
			name:      "an endpoint no profile is for is an error",
			args:      []string{"--endpoint", "https://c.example.io", "--yes"},
			wantErr:   "no profile is for endpoint 'https://c.example.io'",
			wantNames: []profile.Name{"dev", "prod"},
		},
		{
			name:      "MESHSTACK_PROFILE does not name the profile to delete",
			env:       "dev",
			answers:   "2\ny\n",
			wantAsked: " *[1] dev  (https://a.example.io)\n  [2] prod (https://b.example.io)\nSelect a profile [1-2, default=1]: " + strings.ReplaceAll(confirmDev, "dev", "prod"),
			wantNames: []profile.Name{"dev"},
		},
		{
			name:      "a profile that does not exist is an error",
			args:      []string{"--profile", "staging", "--yes"},
			wantErr:   "there is no profile 'staging'",
			wantNames: []profile.Name{"dev", "prod"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			twoProfiles(t)
			t.Setenv("MESHSTACK_PROFILE", tt.env)
			t.Setenv("MESHSTACK_ENDPOINT", tt.endpointEnv)

			output, err := execute(t, tt.answers, append([]string{"delete"}, tt.args...)...)

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAsked, output)
			reloaded, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
			require.NoError(t, err)
			assert.Equal(t, tt.wantNames, names(reloaded))
		})
	}
}

func TestCommandsWithoutADefaultWorkspaceToStoreRefuseWorkspace(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"show"}, {"delete", "--yes", "-p", "dev"}, {}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			twoProfiles(t)

			_, err := execute(t, "", append(args, "-w", "ops")...)

			require.ErrorContains(t, err, `invalid argument "ops" for "-w, --workspace" flag: has no effect here`)
		})
	}
}

func TestTheCommandsThatChangeTheProfilesFailWhileALoginHoldsThem(t *testing.T) {
	for _, args := range [][]string{
		{"add", "--profile", "staging", "--endpoint", "https://c.example.io"},
		{"edit", "--profile", "dev"},
		{"delete", "--profile", "dev", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			twoProfiles(t)
			release := testlogin.HoldProfiles(t)
			defer release()

			synctest.Test(t, func(t *testing.T) {
				_, err := execute(t, "", args...)

				require.ErrorIs(t, err, profile.ErrInUse)
				require.EqualError(t, err, "another meshstack command is changing the profiles, "+
					"such as a login or another meshstack profile; let it finish and try again")
			})
			requireStoredNames(t, "dev", "prod")
		})
	}
}

func TestTheCommandsThatOnlyReadTheProfilesRunWhileALoginHoldsThem(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"show"}, {}} {
		t.Run(cmp.Or(strings.Join(args, " "), "the help"), func(t *testing.T) {
			twoProfiles(t)
			release := testlogin.HoldProfiles(t)
			defer release()

			_, err := execute(t, "", args...)

			require.NoError(t, err)
		})
	}
}
