package internal

import (
	"runtime/debug"
)

const devVersion = "dev"

// Version is set with -X by its package path in .goreleaser.yml, the Dockerfile and flake.nix, so
// moving or renaming it needs the same change there. The linker ignores an -X that does not resolve.
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
