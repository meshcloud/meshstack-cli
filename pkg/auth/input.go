// Package auth answers three questions for both meshStack front ends: how a caller
// authenticates, which workspace it acts in, and where its configuration comes from.
//
// It works in two phases with different lifetimes.
//
// Resolve runs once per process. It answers "who am I, against what, in which workspace"
// and produces an endpoint, a workspace, the methods available, the current method, and a
// store. Every precedence rule applies here and only here. The store is where the
// difference between a CI job and a laptop lives: a credential that arrived whole from the
// environment or from a Terraform provider block gets a memory store and touches no file,
// while a credential that came from a profile gets that profile's file and its lock.
//
// Session.Header runs before every HTTP request, so a long `terraform apply` renews
// mid-flight rather than failing halfway through. It must not touch the filesystem on the
// common path, so there are two caches: one in this process, and the store underneath it,
// consulted only when the in-process token is inside the grace window.
//
// Renewal never switches method. Falling back from a browser login to an API key would
// change the identity behind the command — an ephemeral API key carries only what a
// building block definition declared, while a personal login carries everything that user
// can reach. A command that quietly succeeds with different authority than the user
// intended is worse than one that fails and says why.
package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// Input is what a front end knows that pkg/auth cannot discover for itself. The meshStack
// CLI implements it over flags, stdin and a prompt; the Terraform provider implements it
// over its provider block.
type Input interface {
	// Explicit returns values the caller was given directly. An empty field means "not
	// configured", so pkg/auth may fall back to the environment and then to the profile.
	Explicit() Values

	// ApiKeySecret and ApiToken are called only at the moment a secret is actually needed,
	// so a command served from a cached token never prompts. The provider returns its
	// attribute; the CLI reads the environment, then stdin, then prompts.
	ApiKeySecret(ctx context.Context) (string, error)
	ApiToken(ctx context.Context) (string, error)

	// Warn reports something that did not stop the work. The CLI logs it through pkg/diags
	// at warning level; the provider appends a warning diagnostic. It exists because some
	// outcomes succeed *and* warn — picking a profile by endpoint is the case that forced
	// it — and an error return cannot express that.
	Warn(p diags.Problem)

	// Browser runs an interactive login, or is nil when this front end has no way to. The
	// CLI returns an implementation from pkg/oidc/browser; the Terraform provider returns
	// nil, and pkg/auth then fails a dead login method by naming `meshstack login`.
	Browser() Browser
}

// Browser is the one capability that only an interactive front end has. It is injected
// rather than called directly, so nothing the Terraform provider links can open a browser
// during a plan. A depguard rule keeps pkg/oidc/browser out of everything under pkg/, which
// makes that a compile-time guarantee rather than a promise.
type Browser interface {
	Login(ctx context.Context, cfg oidc.Client) (oidc.Token, error)
}

// Values are the configuration items a front end may supply directly. They sit at the top
// of the precedence order: a flag or a provider block attribute, then the environment, then
// the selected profile, then the built-in default.
type Values struct {
	Profile   string
	Endpoint  string
	Workspace workspace.Name
	ApiKey    string // the API key id; a secret is never carried here

	// Method is the method the caller demands, empty when it does not care. `--api-key`
	// sets it without setting ApiKey, which is how a bare --api-key reuses the id already
	// in the profile. A demanded method that differs from the profile's current one
	// switches it, which only `meshstack auth login` is allowed to do.
	Method method.Method
}
