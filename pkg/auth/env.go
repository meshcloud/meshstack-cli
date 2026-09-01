package auth

import (
	"os"
	"strings"
)

// The environment layer of the precedence order. The names are private, because no
// MESHSTACK_* name is exported anywhere in this module: every message that has to mention
// one is produced in the package that consults it, so neither front end assembles a
// sentence out of a constant it imported.
//
// There are deliberately no per-profile variants. AWS has no AWS_SECRET_ACCESS_KEY_myprofile
// either: the environment layer means "the credential for right now", and selecting a
// profile is what envProfile is for.
const (
	envEndpoint  = "MESHSTACK_ENDPOINT"
	envApiKey    = "MESHSTACK_API_KEY"
	envApiSecret = "MESHSTACK_API_SECRET"
	envApiToken  = "MESHSTACK_API_TOKEN"
	// TODO move that to profile package.
	envProfile = "MESHSTACK_PROFILE"
)

// DevLocalProfile is the name reserved for `meshstack login --dev-local`. That flag writes a
// profile whose every value came from a local dev stack's /mesh/info, so a re-run overwrites
// it without asking and a stack rebuilt from scratch leaves nothing stale behind. Keeping it
// off profile.DefaultName is what makes that safe: a developer's own `default` profile points
// at a real meshStack and must survive. --profile names another one for anybody who wants two.
const DevLocalProfile = "dev-local"

// TODO user agent should be defined from cmd package when building client.
// userAgent identifies this package on the two requests it makes without the API client: the
// API key exchange and the OIDC grants.
const userAgent = "meshstack-cli"

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }

// source names where a resolved value came from, for `meshstack profile view` and for the
// warning that says which profile was picked and why.
type source string

const (
	sourceExplicit source = "explicit"
	sourceEnv      source = "environment"
	sourceProfile  source = "profile"
	sourceDefault  source = "built-in default"
)

func (s source) describe(detail string) string {
	if detail == "" {
		return string(s)
	}
	return string(s) + " " + detail
}

// pick applies the top two layers of the precedence order to one value.
func pick(explicit, envKey string) (value string, from source, detail string) {
	if explicit != "" {
		return explicit, sourceExplicit, ""
	}
	if v := env(envKey); v != "" {
		return v, sourceEnv, envKey
	}
	return "", "", ""
}
