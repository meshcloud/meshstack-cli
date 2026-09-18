package internal

import (
	"runtime/debug"
)

const devVersion = "dev"

// Version identifies the CLI to the meshStack API through the
// client's UserAgent. A release overrides it with
// -ldflags "-X github.com/meshcloud/meshstack-cli/cmd/internal.Version=<tag>".
var Version = devVersion

func init() {
	if Version == devVersion {
		Version = versionFromGoBuild()
	}
}

func versionFromGoBuild() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return devVersion
	}
	return info.Main.Version
}
