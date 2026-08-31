package auth

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
)

// newStatus builds `meshstack auth status`, which makes no network call unless --verify is
// given: everything except whether the credential was revoked is already on disk, and
// staying local is what keeps it fast enough for a shell prompt or a script.
//
// It deliberately does not require a workspace. It is one of the two commands a user runs
// in order to pick one.
func newStatus(in *cli.Input) *cobra.Command {
	var verify bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the resolved endpoint, workspace, methods and token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			session, err := auth.Resolve(ctx, in)
			if err != nil {
				return err
			}
			status, err := session.Status()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printStatus(out, status)
			if !verify {
				return nil
			}

			// One authenticated read that every method can make. Revocation is the one
			// thing the stored state cannot know, because a refresh token carries no
			// expiry, so proving it costs a round trip and gets a flag.
			api, err := session.Client(ctx, cli.UserAgent)
			if err != nil {
				return auth.HintErr(err, session)
			}
			if _, err := api.Workspace.List(ctx); err != nil {
				return auth.HintErr(err, session)
			}
			_, _ = fmt.Fprintf(out, "%-9s the credential still works\n", "Verified")
			return nil
		},
	}

	cmd.Flags().BoolVar(&verify, "verify", false, "also make one authenticated call to prove the credential still works")

	return cmd
}

func printStatus(out io.Writer, status auth.Status) {
	if status.Profile != "" {
		row(out, "Profile", status.Profile, status.ConfigPath)
	} else {
		row(out, "Profile", "none", status.Sources["credential"])
	}
	row(out, "Endpoint", status.Endpoint, "")
	if strings.TrimSpace(status.Workspace) != "" {
		row(out, "Workspace", status.Workspace, status.Sources["workspace"])
	} else {
		row(out, "Workspace", "none", "")
	}

	printMethods(out, status)
	row(out, "Current", status.Current.Description(), "")
	if token := status.Token; token != nil {
		where := "scope " + string(token.Scope)
		switch {
		case token.ExpiresAt.IsZero():
			row(out, "Token", where, "no expiry; the server decides")
		default:
			row(out, "Token", where, "expires "+humanDuration(token.ExpiresIn))
		}
	} else {
		row(out, "Token", "none cached", "")
	}
}

func printMethods(out io.Writer, status auth.Status) {
	label := "Methods"
	if login := status.Login; login != nil {
		row(out, label, "login  "+login.Issuer, "logged in "+humanDuration(-login.Age))
		label = ""
	}
	if apiKey := status.ApiKey; apiKey != nil {
		row(out, label, "apiKey "+apiKey.ClientId, "secret from "+apiKey.SecretFrom)
		label = ""
	}
	// A pasted token is not a method on disk — nothing can renew it — so it is named here
	// only when it is what this session authenticates with.
	if status.Current == credential.MethodManual {
		row(out, label, "manual an API token", "nothing can refresh it")
		label = ""
	}
	if label != "" {
		row(out, label, "none stored", "`meshstack login` stores one")
	}
}
