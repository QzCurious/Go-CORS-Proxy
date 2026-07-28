package version

import "runtime/debug"

const Name = "seamless-cors"

// Version is replaced with the release tag at build time.
var Version = "dev"

// Current returns the release version embedded by GoReleaser or recorded by
// module-aware go install.
func Current() string {
	if Version != "dev" {
		return Version
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok || buildInfo.Main.Version == "" || buildInfo.Main.Version == "(devel)" {
		return Version
	}
	return buildInfo.Main.Version
}
