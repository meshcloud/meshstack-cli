package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
	"github.com/spf13/cobra"
)

func NewLogin(ctx context.Context) *cobra.Command {
	const apiKeyIdDefault = "<id>"
	var (
		apiKey   string
		apiToken bool
		force    bool
		stdin    bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to meshStack",
		Long:  `Log in to meshStack and store the credential in a profile.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if cmd.Flags().Changed("api-key") {
				return fmt.Errorf("an API key id needs an equals sign: write `--api-key=%s`. --api-key takes an optional value, so %q was read as a positional argument rather than as the id", args[0], args[0])
			}
			return fmt.Errorf("this command takes no arguments: `meshstack auth login` does not take %q. Everything it needs comes from flags and the environment", args[0])
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var sources setting.ExplicitSources
			switch {
			case cmd.Flags().Changed("api-key"):
				if apiKey == "" {
					return errors.New("the API key id is empty: `--api-key=` was given without an id. Leave the value off entirely to reuse the id already in the profile")
				} else if apiKey != apiKeyIdDefault {

				}
				method = credential.MethodApiKey
				if apiKey != apiKeyIdDefault {
					in.ApiKey = string(apiKey)
				}
				force = true
			case apiToken:
				method = credential.MethodManual
				force = true
			default:
				// Every form names the method it wants, and bare means the browser login.
				// Naming it is what makes `meshstack login` switch a profile back from its
				// API key rather than logging in again with whatever is current.
				method = credential.MethodLogin
			}
			// Read before the resolution, because a setting.Source has neither a context nor
			// an error return, so a read that blocked would have nowhere to report itself.
			if secretStdin {
				secret, err := in.ReadLine()
				if err != nil {
					return err
				}
				in.ApiSecret = secret
			}
			if tokenStdin {
				token, err := in.ReadLine()
				if err != nil {
					return err
				}
				in.ApiToken = token
			}
			auth.ResolveSession(ctx, auth.ResolveSessionOptions{})
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&apiKey, "api-key", apiKeyIdDefault, "switch to the apiKey method, with the stored id, MESHSTACK_API_KEY, or a new one")
	flags.BoolVar(&apiToken, "api-token", false, "store an API token that nothing can refresh")
	flags.BoolVar(&force, "force", false, "log in again even if the stored login still works")
	flags.BoolVar(&stdin, "stdin", false, "read the API key secret or API token from the first line of stdin")
	cmd.MarkFlagsMutuallyExclusive("api-key", "api-token")
	cmd.MarkFlagsRequiredTogether("api-secret-stdin", "api-token-stdin")

	return cmd
}
