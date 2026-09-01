package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// bareApiKey is what --api-key means without a value: keep the id already in the profile.
//
// pflag needs a non-empty NoOptDefVal before it will accept a flag without a value, and it
// renders that value in the usage line, so the sentinel is the placeholder the
// documentation already uses and the help reads `--api-key[=<id>]`. An id is a UUID, so
// nothing a user could legitimately pass collides with it.
const bareApiKey = "<id>"

// apiKeyId exists only for its Type, which pflag renders into the usage line. A named type
// would print as `--api-key id[=<id>]` and a string as `--api-key[="<id>"]`; the empty one
// prints `--api-key[=<id>]`, which is how the flag is documented.
type apiKeyId string

func (v *apiKeyId) String() string     { return string(*v) }
func (v *apiKeyId) Set(s string) error { *v = apiKeyId(s); return nil }
func (v *apiKeyId) Type() string       { return "" }

// NewLogin returns a fresh command on every call, because cmd/meshstack registers login
// under two parents. Flag targets have to stay local to this function: a package-level var
// would be shared by both instances.
func NewLogin(in *cli.Input) *cobra.Command {
	var (
		apiKey      apiKeyId
		apiToken    bool
		devLocal    bool
		force       bool
		secretStdin bool
		tokenStdin  bool
		method      credential.Method
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to meshStack",
		Long: `Log in to meshStack and store the credential in a profile.

Without a flag this opens a browser, and re-running it costs nothing: the stored login is
probed first and reported rather than replaced. --api-key and --api-token select the other
two methods, and each implies --force, because changing method is explicit by nature.

--dev-local configures a profile for a local dev stack out of what that stack publishes at
/mesh/info, so running against one needs no credentials of your own. It defaults to the
endpoint http://localhost:8080 and to the profile dev-local.

No secret and no token is ever a flag value. Both arrive through MESHSTACK_API_SECRET or
MESHSTACK_API_TOKEN, through stdin, or through a prompt that does not echo.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			// --api-key takes an optional value, so pflag resolves it before it looks at
			// the next argument and `--api-key <id>` leaves the id behind as a positional
			// argument. Saying so beats "unknown command".
			if cmd.Flags().Changed("api-key") {
				return diags.Errorf("an API key id needs an equals sign",
					"write `--api-key=%s`. --api-key takes an optional value, so %q was read as a positional argument rather than as the id.",
					args[0], args[0])
			}
			return diags.Errorf("this command takes no arguments",
				"`meshstack auth login` does not take %q. Everything it needs comes from flags and the environment.", args[0])
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case devLocal:
				return runDevLocalLogin(cmd, in)
			case cmd.Flags().Changed("api-key"):
				if apiKey == "" {
					return diags.Errorf("the API key id is empty",
						"`--api-key=` was given without an id. Leave the value off entirely to reuse the id already in the profile.")
				}
				method = credential.MethodApiKey
				if apiKey != bareApiKey {
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
			return runLogin(cmd, in, method, force)
		},
	}

	flags := cmd.Flags()
	flags.Var(&apiKey, "api-key", "switch to the apiKey method, with the stored id, MESHSTACK_API_KEY, or a new one")
	flags.Lookup("api-key").NoOptDefVal = bareApiKey
	flags.BoolVar(&apiToken, "api-token", false, "store an API token that nothing can refresh")
	flags.BoolVar(&devLocal, "dev-local", false, "configure a profile from a local dev stack's own published credentials")
	flags.BoolVar(&force, "force", false, "log in again even if the stored login still works")
	flags.BoolVar(&secretStdin, "api-secret-stdin", false, "read the API key secret from the first line of stdin")
	flags.BoolVar(&tokenStdin, "api-token-stdin", false, "read the API token from the first line of stdin")
	cmd.MarkFlagsMutuallyExclusive("api-key", "api-token", "dev-local")
	cmd.MarkFlagsMutuallyExclusive("api-secret-stdin", "api-token-stdin")

	return cmd
}

// runDevLocalLogin parses nothing and decides nothing: the two defaults --dev-local brings and
// the bootstrap itself are pkg/auth's, so that the Terraform provider's acceptance tests can
// reach the same behaviour without a CLI process.
func runDevLocalLogin(cmd *cobra.Command, in *cli.Input) error {
	ctx := cmd.Context()

	session, err := auth.ResolveForDevLocalLogin(ctx, in)
	if err != nil {
		return err
	}
	if err := profile.Ensure(session.Profile, &session.Endpoint); err != nil {
		return err
	}
	result, err := session.LoginDevLocal(ctx)
	if err != nil {
		return err
	}
	printLogin(cmd.OutOrStdout(), result)
	return nil
}

func runLogin(cmd *cobra.Command, in *cli.Input, method credential.Method, force bool) error {
	ctx := cmd.Context()

	session, err := loginSession(ctx, in, method)
	if err != nil {
		// This is the one command allowed to ask, and pkg/auth names the three failures a
		// person could answer. Everything else is reported as it came.
		if !askAbout(cmd, in, err) {
			return err
		}
		if session, err = loginSession(ctx, in, method); err != nil {
			return err
		}
	}

	if err := profile.Ensure(session.Profile, &session.Endpoint); err != nil {
		return err
	}

	options := auth.LoginOptions{Force: force, Browser: in.Browser()}
	if in.MayPrompt {
		options.ChooseWorkspace = func(_ context.Context, candidates []string) (string, error) {
			return chooseWorkspace(cmd, candidates)
		}
	}

	result, err := session.Login(ctx, options)
	if err != nil {
		return err
	}
	printLogin(cmd.OutOrStdout(), result)
	return nil
}

// loginSession resolves with the profile's own store, which is what makes `meshstack login
// --api-key=k` write the secret to disk while an ordinary command with the same environment
// does not.
func loginSession(ctx context.Context, in *cli.Input, method credential.Method) (*auth.Session, error) {
	selection, err := profile.Select(ctx, in, setting.Environ())
	if err != nil {
		return nil, err
	}
	store, err := profile.NewFileStore(selection.Name)
	if err != nil {
		return nil, err
	}
	return auth.ResolveSession(ctx, auth.ResolveSessionOptions{
		Settings: in, DemandMethod: method, Store: store,
	})
}

// askAbout puts the failure to the person at the keyboard and reports whether they supplied
// something worth resolving again with. It no longer guesses which failure happened: the
// three sentinels say so.
func askAbout(cmd *cobra.Command, in *cli.Input, failure error) bool {
	if !in.MayPrompt {
		return false
	}
	ctx := cmd.Context()
	switch {
	case errors.Is(failure, auth.ErrNoEndpoint):
		endpoint, asked := askForEndpoint(ctx, cmd, in)
		in.Endpoint = endpoint
		return asked
	case errors.Is(failure, auth.ErrNoApiSecret):
		secret, err := in.PromptSecret(ctx, "meshStack API key secret")
		in.ApiSecret = secret
		return err == nil && secret != ""
	case errors.Is(failure, auth.ErrNoApiToken):
		token, err := in.PromptSecret(ctx, "meshStack API token")
		in.ApiToken = token
		return err == nil && token != ""
	}
	return false
}

// askForEndpoint offers the endpoints already configured, because a second profile against a
// meshStack the machine already knows is the common case. It selects the profile the way
// everything else does rather than keeping its own copy of that rule.
func askForEndpoint(ctx context.Context, cmd *cobra.Command, in *cli.Input) (string, bool) {
	known, err := profile.List()
	if err != nil {
		return "", false
	}
	selection, err := profile.Select(ctx, in, setting.Environ())
	if err != nil {
		return "", false
	}

	out := cmd.ErrOrStderr()
	_, _ = fmt.Fprintf(out, "Profile %q has no endpoint. Which one?\n", selection.Name)
	endpoints := knownEndpoints(known)
	for i, endpoint := range endpoints {
		_, _ = fmt.Fprintf(out, "  %d) %-40s (profile %s)\n", i+1, endpoint.url, strings.Join(endpoint.profiles, ", "))
	}
	_, _ = fmt.Fprintf(out, "  %d) another endpoint\n", len(endpoints)+1)

	choice, err := ask(cmd, "> ")
	if err != nil {
		return "", false
	}
	if picked, err := strconv.Atoi(choice); err == nil && picked >= 1 && picked <= len(endpoints) {
		return endpoints[picked-1].url, true
	}
	typed, err := ask(cmd, "Endpoint: ")
	if err != nil || typed == "" {
		return "", false
	}
	return typed, true
}

// knownEndpoint is one line of that prompt.
type knownEndpoint struct {
	url      string
	profiles []string
}

func knownEndpoints(known []profile.Summary) []knownEndpoint {
	var list []knownEndpoint
	for _, p := range known {
		if p.Endpoint == "" {
			continue
		}
		if i := slices.IndexFunc(list, func(e knownEndpoint) bool { return e.url == p.Endpoint }); i >= 0 {
			list[i].profiles = append(list[i].profiles, p.Name)
			continue
		}
		list = append(list, knownEndpoint{url: p.Endpoint, profiles: []string{p.Name}})
	}
	return list
}

// chooseWorkspace leaves the profile's default alone on an empty answer, for a user who
// wants to decide later with `meshstack profile set workspace`.
func chooseWorkspace(cmd *cobra.Command, candidates []string) (string, error) {
	out := cmd.ErrOrStderr()
	if len(candidates) == 0 {
		_, _ = fmt.Fprintln(out, "This login can see no workspaces yet, so the profile keeps no default one.")
		return "", nil
	}
	_, _ = fmt.Fprintln(out, "Which workspace should this profile use?")
	for i, candidate := range candidates {
		_, _ = fmt.Fprintf(out, "  %d) %s\n", i+1, candidate)
	}
	answer, err := ask(cmd, "> ")
	if err != nil {
		return "", err
	}
	// An answer that is not a number comes back as 0, which is out of range like any other
	// wrong number, and gets the same reply.
	picked, _ := strconv.Atoi(answer)
	if picked < 1 || picked > len(candidates) {
		_, _ = fmt.Fprintln(out, "No workspace was chosen, so the profile keeps no default one.")
		return "", nil
	}
	return candidates[picked-1], nil
}

// ask writes to stderr, so that a command's real output stays pipeable while it is asking.
func ask(cmd *cobra.Command, prompt string) (string, error) {
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), prompt)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func printLogin(out io.Writer, result auth.LoginResult) {
	who := ""
	if result.Username != "" {
		who = " as " + result.Username
	}
	switch {
	case result.AlreadyLoggedIn:
		_, _ = fmt.Fprintf(out, "Already logged in to %s%s.\n", result.Endpoint, who)
	case result.SwitchedFrom != "":
		_, _ = fmt.Fprintf(out, "Switched from the %s and logged in to %s%s.\n",
			result.SwitchedFrom.Description(), result.Endpoint, who)
	default:
		_, _ = fmt.Fprintf(out, "Logged in to %s%s.\n", result.Endpoint, who)
	}

	if result.Profile != "" {
		row(out, "Profile", result.Profile, "")
	}
	row(out, "Method", result.Method.Description(), "")
	if strings.TrimSpace(result.Workspace) != "" {
		row(out, "Workspace", result.Workspace, "")
	}
	if result.Method != credential.MethodManual {
		return
	}

	// An API token is the one credential with a deadline nothing can extend, so the deadline
	// is part of what happened rather than something to look up later.
	if result.ExpiryKnown {
		row(out, "Expires", result.ExpiresAt.Local().Format(time.RFC3339), humanDuration(time.Until(result.ExpiresAt)))
	} else {
		row(out, "Expires", "unknown", "the token is not a JWT, so it carries no expiry")
	}
	_, _ = fmt.Fprintln(out, "Nothing can refresh an API token. Store a fresh one with `meshstack auth login --api-token` when this one runs out.")
}
