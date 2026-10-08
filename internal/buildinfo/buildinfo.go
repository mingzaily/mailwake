// Package buildinfo reports the version of the running binary.
package buildinfo

import "runtime/debug"

// Version is set at release build time:
//
//	-ldflags "-X github.com/mingzaily/mailwake/internal/buildinfo.Version=v0.1.0"
var Version string

// Current returns the release version and VCS revision. Without a release version it
// falls back to the Go module version, and to "devel" for local builds.
func Current() (version, revision string) {
	version = Version
	if info, ok := debug.ReadBuildInfo(); ok {
		if version == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
		}
	}
	if version == "" {
		version = "devel"
	}
	return version, revision
}
