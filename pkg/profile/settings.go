package profile

import "github.com/meshcloud/meshstack-cli/pkg/setting"

var Name = setting.Value[string]{
	EnvKey: "MESHSTACK_PROFILE",
	Short:  "The profile whose credentials and defaults this run uses. Also read from MESHSTACK_PROFILE.",
	Parse:  setting.Text,
}

var ConfigFile = setting.Value[string]{
	EnvKey: envConfigFile,
	Short:  "The file describing every profile. Also read from MESHSTACK_CONFIG_FILE.",
	Parse:  setting.Text,
}

var CredentialsDir = setting.Value[string]{
	EnvKey: envCredentialsDir,
	Short:  "The directory holding one credentials file per profile. Also read from MESHSTACK_CREDENTIALS_DIR.",
	Parse:  setting.Text,
}
