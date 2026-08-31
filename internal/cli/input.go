// Package cli implements the meshStack CLI's half of auth.Input: the values its flags
// carry, the secrets it reads from the environment, from stdin or from a terminal prompt,
// and the browser login.
//
// It is internal because Go's own rule is the guarantee that matters here. This package
// prompts, and a Terraform provider run must never block on a terminal that is not there —
// so rather than trusting a convention, the import is impossible. That is also what makes
// it the one place outside cmd/ that may reach pkg/oidc/browser; .golangci.yml carries the
// rule and the reasoning.
package cli

import (
	"context"
	"io"
	"os"

	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/browser"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// UserAgent identifies this CLI to the meshStack API. cmd/meshstack replaces it with the
// build version at startup, because the commands that build a client cannot import package
// main to read it there.
var UserAgent = "meshstack-cli"

// Input implements auth.Input over the meshStack CLI's flags, stdin and a terminal prompt.
// The fields are bound to flags by the commands that own them: the root command binds
// Profile, Endpoint and Workspace, and `auth login` binds ApiKey and Method. One Input is
// shared by the whole command tree, because the persistent flags feeding it are.
//
// No secret is a field here. A secret is never a flag value, so it arrives through
// ApiKeySecret and ApiToken at the moment it is needed and is never held.
type Input struct {
	Profile   string
	Endpoint  string
	Workspace string
	ApiKey    string
	Method    credential.Method

	// in and out are where a prompt reads and writes. They are fields only so that a test
	// can drive one; nothing outside this package sets them. in is an *os.File because
	// deciding whether to prompt at all means asking whether it is a terminal.
	in  *os.File
	out io.Writer
}

func New() *Input {
	// Prompts go to stderr so that a command's real output stays pipeable.
	return &Input{in: os.Stdin, out: os.Stderr}
}

func (i *Input) Explicit() auth.Values {
	return auth.Values{
		Profile:   i.Profile,
		Endpoint:  i.Endpoint,
		Workspace: workspace.Name(i.Workspace),
		ApiKey:    i.ApiKey,
		Method:    i.Method,
	}
}

func (i *Input) ApiKeySecret(ctx context.Context) (string, error) {
	return i.readSecret(ctx, auth.SecretFromEnv, auth.SecretPrompt, auth.MissingSecretError)
}

func (i *Input) ApiToken(ctx context.Context) (string, error) {
	return i.readSecret(ctx, auth.TokenFromEnv, auth.TokenPrompt, auth.MissingTokenError)
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
