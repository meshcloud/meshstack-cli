package auth

import (
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

// SecretFromEnv and TokenFromEnv apply the environment layer to a value that never travels
// as a flag, so that a front end can rank the environment above stdin and a prompt without
// importing the variable's name. No MESHSTACK_* name is exported anywhere in this module;
// every message that has to mention one is produced here.
func SecretFromEnv() (string, bool) {
	secret := env(envApiSecret)
	return secret, secret != ""
}

func TokenFromEnv() (string, bool) {
	token := env(envApiToken)
	return token, token != ""
}

// MissingSecretError and MissingTokenError name every way the value could have been
// supplied. A command that cannot prompt returns one of them rather than hanging on a
// terminal that is not there.
func MissingSecretError() error {
	return diags.Errorf("no API key secret",
		"set %s, or pipe the secret to stdin. A secret is never a flag value, because a flag value lands in shell history, in ps output and in CI logs. Prompting is off because stdin is not a terminal or %s is set.",
		envApiSecret, tty.NoInputHint())
}

func MissingTokenError() error {
	return diags.Errorf("no API token",
		"set %s, or pipe the token to stdin. Prompting is off because stdin is not a terminal or %s is set.",
		envApiToken, tty.NoInputHint())
}

// SecretPrompt and TokenPrompt are the words a front end puts in front of the cursor, kept
// here so that both front ends and every error message agree on what is being asked for.
const (
	SecretPrompt = "meshStack API key secret"
	TokenPrompt  = "meshStack API token"
)
