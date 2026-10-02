package color

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/assert"
)

// TTY_FORCE makes colorprofile take the buffer for a terminal.
var terminal = []string{"TTY_FORCE=1", "TERM=xterm-256color"}

func TestColor(t *testing.T) {
	buildingBlockRun := []string{"HOME=/home/runner", "PATH=/usr/bin", "MESHSTACK_USER_MESSAGE=/tmp/user-message", "TF_IN_AUTOMATION=1"}
	for _, tc := range []struct {
		name        string
		environ     []string
		output, log colorprofile.Profile
	}{
		{"a pipe stays plain", nil, colorprofile.NoTTY, colorprofile.NoTTY},
		{"a building block run stays plain", buildingBlockRun, colorprofile.NoTTY, colorprofile.NoTTY},
		{"CI alone colors nothing", []string{"CI=true"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"GitHub Actions colors the log only", []string{"CI=true", "GITHUB_ACTIONS=true"}, colorprofile.NoTTY, colorprofile.TrueColor},
		{"GitLab CI colors the log only", []string{"CI=true", "GITLAB_CI=true"}, colorprofile.NoTTY, colorprofile.ANSI},
		{"Azure Pipelines colors the log only", []string{"TF_BUILD=True", "AGENT_NAME=Hosted Agent"}, colorprofile.NoTTY, colorprofile.ANSI},
		{"TF_BUILD alone is no Azure Pipelines", []string{"TF_BUILD=True"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"FORCE_COLOR=1 forces 16 colors", []string{"FORCE_COLOR=1"}, colorprofile.ANSI, colorprofile.ANSI},
		{"FORCE_COLOR=true forces 16 colors", []string{"FORCE_COLOR=true"}, colorprofile.ANSI, colorprofile.ANSI},
		{"FORCE_COLOR=2 forces 256 colors", []string{"FORCE_COLOR=2"}, colorprofile.ANSI256, colorprofile.ANSI256},
		{"FORCE_COLOR=3 forces true color", []string{"FORCE_COLOR=3"}, colorprofile.TrueColor, colorprofile.TrueColor},
		{"FORCE_COLOR raises a CI's log", []string{"GITLAB_CI=true", "FORCE_COLOR=3"}, colorprofile.TrueColor, colorprofile.TrueColor},
		{"an empty FORCE_COLOR forces nothing", []string{"FORCE_COLOR="}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"FORCE_COLOR=0 turns a CI's color off", []string{"GITHUB_ACTIONS=true", "FORCE_COLOR=0"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"FORCE_COLOR=false turns a CI's color off", []string{"GITHUB_ACTIONS=true", "FORCE_COLOR=false"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"CLICOLOR_FORCE=1 forces 16 colors", []string{"CLICOLOR_FORCE=1"}, colorprofile.ANSI, colorprofile.ANSI},
		{"CLICOLOR_FORCE=yes forces 16 colors", []string{"CLICOLOR_FORCE=yes"}, colorprofile.ANSI, colorprofile.ANSI},
		{"CLICOLOR_FORCE=0 forces nothing", []string{"CLICOLOR_FORCE=0"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"NO_COLOR outranks FORCE_COLOR", []string{"NO_COLOR=1", "FORCE_COLOR=3"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"NO_COLOR outranks CLICOLOR_FORCE", []string{"NO_COLOR=1", "CLICOLOR_FORCE=1"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"NO_COLOR outranks a CI", []string{"NO_COLOR=1", "GITHUB_ACTIONS=true"}, colorprofile.NoTTY, colorprofile.NoTTY},
		{"a terminal takes its colors", terminal, colorprofile.ANSI256, colorprofile.ANSI256},
		{"FORCE_COLOR=3 raises a terminal", append([]string{"FORCE_COLOR=3"}, terminal...), colorprofile.TrueColor, colorprofile.TrueColor},
		{"NO_COLOR keeps a terminal's emphasis", append([]string{"NO_COLOR=1"}, terminal...), colorprofile.ASCII, colorprofile.ASCII},
		{"any NO_COLOR counts", append([]string{"NO_COLOR=yes"}, terminal...), colorprofile.ASCII, colorprofile.ASCII},
		{"an empty NO_COLOR counts as unset", append([]string{"NO_COLOR="}, terminal...), colorprofile.ANSI256, colorprofile.ANSI256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.output, Output(&bytes.Buffer{}, tc.environ), "output")
			assert.Equal(t, tc.log, Log(&bytes.Buffer{}, tc.environ), "log")
		})
	}
}
