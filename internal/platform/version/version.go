// Package version holds build metadata stamped in at link time.
package version

import "runtime/debug"

// Set via -ldflags "-X github.com/selimslab/gobase/internal/platform/version.Version=...".
var (
	// Version is the semantic version or tag of this build.
	Version = "dev"
	// Commit is the git revision of this build.
	Commit = ""
	// BuildTime is the RFC 3339 timestamp of this build.
	BuildTime = ""
)

// Info reports the build metadata, falling back to the values the Go
// toolchain embeds when the linker flags were not supplied.
func Info() (v, commit, buildTime string) {
	v, commit, buildTime = Version, Commit, BuildTime
	if commit != "" && buildTime != "" {
		return v, commit, buildTime
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v, commit, buildTime
	}

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
			}
		case "vcs.time":
			if buildTime == "" {
				buildTime = s.Value
			}
		}
	}

	return v, commit, buildTime
}
