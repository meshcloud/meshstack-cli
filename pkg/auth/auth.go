// Package auth answers three questions for both meshStack front ends: how a caller
// authenticates, which workspace it acts in, and where its configuration comes from.
//
// It reports to a front end in exactly two ways: an error return, or an slog record. There
// is no third channel, because both front ends already route slog somewhere it is read — the
// meshStack CLI through its charmbracelet/log handler, the Terraform provider through its
// tflog bridge. The cost is that a warning reaches a Terraform practitioner only under
// TF_LOG and never in plan output, which is the trade this package takes. Use the Context
// form, because the provider's bridge takes terraform's logger out of the context and drops
// a record that arrives without one.
//
// It never prompts. A prompt is not a source, so the three failures a person could answer
// come back as ErrNoEndpoint, ErrNoApiSecret and ErrNoApiToken, and `meshstack auth login` —
// the one command allowed to ask — resolves again with the answer in its own source.
//
// Session.BearerToken then runs before every HTTP request, so a long `terraform apply`
// renews mid-flight rather than failing halfway through.
package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/pkg/oidc"
)

// TODO user agent should be defined from cmd package when building client.
const userAgent = "meshstack-cli"

// Browser is a parameter rather than a direct call into pkg/oidc/browser because
// .golangci.yml denies that package to everything under pkg/, so the Terraform provider
// cannot link a browser flow at all.
type Browser func(ctx context.Context, client oidc.Client) (oidc.Token, error)
