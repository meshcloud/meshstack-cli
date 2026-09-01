// Package cli is the meshStack CLI's half of resolving a session: its settings source, the
// secrets it reads from stdin or a prompt, and the browser login.
//
// It is internal because Go's own rule is the guarantee that matters here. This package
// prompts, and a Terraform provider run must never block on a terminal that is not there, so
// the import is impossible rather than merely discouraged. That is also what makes it the one
// place outside cmd/ that may reach pkg/oidc/browser; .golangci.yml carries that rule.
package cli

import (
	"io"
	"os"

	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/browser"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

// UserAgent identifies this CLI to the meshStack API. cmd/meshstack replaces it with the
// build version at startup, because the commands that build a client cannot import package
// main to read it there.
var UserAgent = "meshstack-cli"

// Input is the top of every ranked list. One serves the whole command tree, because the
// persistent flags feeding it do.
//
// ApiSecret and ApiToken hold what --api-secret-stdin and --api-token-stdin read, which is a
// line of stdin rather than an argv entry. The read happens before the resolution, because a
// Source has neither a context nor an error return to report one that blocked.
type Input struct {
	Profile   string
	Endpoint  string
	Workspace string
	ApiKey    string
	ApiSecret string
	ApiToken  string
	NoInput   bool

	// MayPrompt is tty.NoInput and a terminal check, resolved once by cmd/meshstack. It is
	// separate from NoInput, which is the flag alone, because a command has to know whether
	// it may ask at a moment when the resolution has just failed and there is no Session.
	MayPrompt bool

	// in and out are fields only so that a test can drive one. in is an *os.File because
	// deciding whether to prompt at all means asking whether it is a terminal.
	in  *os.File
	out io.Writer
}

var _ setting.Source = (*Input)(nil)

func New() *Input {
	// Prompts go to stderr so that a command's real output stays pipeable.
	return &Input{in: os.Stdin, out: os.Stderr}
}

func (i *Input) Lookup(key string) (string, bool) {
	_, value := i.flag(key)
	return value, value != ""
}

func (i *Input) Describe(key string) string {
	name, _ := i.flag(key)
	return name
}

// flag answers both halves of setting.Source from one switch, so an origin naming a flag
// cannot drift from the value that flag carried.
func (i *Input) flag(key string) (name, value string) {
	switch key {
	case meshstack.Endpoint.EnvKey:
		return "--endpoint", i.Endpoint
	case meshstack.Workspace.EnvKey:
		return "--workspace", i.Workspace
	case profile.Name.EnvKey:
		return "--profile", i.Profile
	case credential.ApiKeyId.EnvKey:
		return "--api-key", i.ApiKey
	case credential.ApiSecret.EnvKey:
		return "--api-secret-stdin", i.ApiSecret
	case credential.ApiToken.EnvKey:
		return "--api-token-stdin", i.ApiToken
	case tty.NoInput.EnvKey:
		// A boolean flag left off answers nothing rather than "false", so that it does not
		// silence MESHSTACK_NO_INPUT below it.
		if !i.NoInput {
			return "--no-input", ""
		}
		return "--no-input", "true"
	}
	return key, ""
}

func (i *Input) Browser() auth.Browser {
	return browser.Login
}

func (i *Input) stdin() *os.File {
	if i.in == nil {
		return os.Stdin
	}
	return i.in
}

func (i *Input) stderr() io.Writer {
	if i.out == nil {
		return os.Stderr
	}
	return i.out
}
