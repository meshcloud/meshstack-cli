package auth

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
)

// newStatus makes no network call unless --verify is given: staying local is what keeps it
// fast enough for a shell prompt. It deliberately does not require a workspace either, being
// one of the two commands a user runs in order to pick one.
func newStatus(in *cli.Input) *cobra.Command {
	var verify bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the resolved endpoint, workspace, methods and token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			session, err := auth.ResolveSession(ctx, auth.ResolveSessionOptions{Settings: in})
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

			// Revocation is the one thing the stored state cannot know, because a refresh
			// token carries no expiry, so proving it costs a round trip and gets a flag.
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
	row(out, "Profile", status.Profile, status.ConfigPath)
	row(out, "Endpoint", status.Endpoint, originOf(status, meshstack.Endpoint.EnvKey))
	if status.Workspace == "" {
		row(out, "Workspace", "none", "")
	} else {
		row(out, "Workspace", status.Workspace, originOf(status, meshstack.Workspace.EnvKey))
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
		from := apiKey.SecretFrom
		if from == "" {
			from = originOf(status, credential.ApiSecret.EnvKey)
		}
		if from == "" {
			// A profile that holds an API key beside the login it is authenticating with:
			// nothing resolved the secret, so nothing recorded where it came from.
			from = "the credentials file"
		}
		row(out, label, "apiKey "+apiKey.ClientId, "secret from "+from)
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

// originOf finds one setting's origin. `meshstack profile view` prints the whole list; this
// command wants two of them beside the values they explain.
func originOf(status auth.Status, key string) string {
	for _, origin := range status.Origins {
		if origin.Key == key {
			return origin.Source
		}
	}
	return ""
}
