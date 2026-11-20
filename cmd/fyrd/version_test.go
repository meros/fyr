package main

import "testing"

// V0.versioned-releases: `fyrd --version` names the version and the build,
// or the build on main.
func TestVersionLine(t *testing.T) {
	b, v := buildID, version
	t.Cleanup(func() { buildID, version = b, v })
	buildID, version = "0123456789ab", ""
	if got := versionLine(); got != "fyrd 0123456789ab (main)" {
		t.Errorf("main: %q", got)
	}
	version = "v1.0.0-rc.1"
	if got := versionLine(); got != "fyrd v1.0.0-rc.1 (build 0123456789ab)" {
		t.Errorf("version: %q", got)
	}
}
