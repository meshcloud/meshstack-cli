package profile

import "github.com/meshcloud/meshstack-cli/pkg/setting"

var Name = setting.Value[string]{
	EnvKey: "MESHSTACK_PROFILE",
	Short:  "The profile whose credentials and defaults this run uses. Also read from MESHSTACK_PROFILE.",
	Long: "The profile whose credentials and defaults this run uses, also read from `MESHSTACK_PROFILE`.\n\n" +
		"A profile is a named bundle of endpoint, credential and default workspace, written by " +
		"`meshstack auth login` into the meshStack CLI's configuration directory. It supplies each of those " +
		"only where nothing above it did, so it is never an override.\n\n" +
		"With no name given, the profile is the one whose endpoint matches the endpoint in use, else the one " +
		"`meshstack profile set` last selected, else `default`.",
	Parse: setting.Text,
}

var ConfigDir = setting.Value[string]{
	EnvKey: envConfigDir,
	Short:  "The directory holding config.json and one credentials file per profile. Also read from MESHSTACK_CONFIG_DIR.",
	Long: "The directory holding the meshStack CLI's configuration, also read from `MESHSTACK_CONFIG_DIR`.\n\n" +
		"`config.json` describes every profile, and `credentials/<profile>.json` holds that profile's " +
		"credentials and its cached tokens. The credentials directory is a convention rather than a " +
		"setting of its own, so it moves with this one and cannot be pointed elsewhere.",
	Parse: setting.Text,
}
