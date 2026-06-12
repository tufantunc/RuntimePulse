// Package version is the single source of truth for RuntimePulse's
// version. Release builds inject the git tag at link time:
//
//	-X github.com/tufantunc/RuntimePulse/internal/version.Version=1.0.0
//
// Source builds fall back to VCS metadata from the Go build info, so
// "which commit is this?" is always answerable.
package version

import "runtime/debug"

// Version is "dev" unless overwritten by a release build's ldflags.
var Version = "dev"

// IsDev reports whether this is a non-release build. Used to suppress
// the client/daemon mismatch warning for source builds.
func IsDev() bool { return Version == "dev" }

// String returns the human-facing version: the injected release
// version, or "dev (<short-rev>[+dirty])" for source builds, or plain
// "dev" when no VCS info exists (test binaries).
func String() string {
	if !IsDev() {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return Version
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		return "dev (" + rev + "+dirty)"
	}
	return "dev (" + rev + ")"
}
