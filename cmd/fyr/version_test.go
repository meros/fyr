package main

import (
	"os"
	"path/filepath"
	"testing"
)

// V0.versioned-releases: `fyr --version` names the launcher's version (or
// build) and asks the current fyrd for its own.
func TestVersionLines(t *testing.T) {
	b, v := buildID, version
	t.Cleanup(func() { buildID, version = b, v })
	buildID, version = "0123456789ab", "v1.2.3"
	data := t.TempDir()
	if got := versionLines(data); got != "fyr v1.2.3 (build 0123456789ab)\nno current build in "+data+"\n" {
		t.Errorf("no build: %q", got)
	}
	os.MkdirAll(filepath.Join(data, "builds", "x"), 0o755)
	os.WriteFile(filepath.Join(data, "builds", "x", "fyrd"), []byte("#!/bin/sh\necho 'fyrd v1.2.3 (build 0123456789ab)'\n"), 0o755)
	os.Symlink(filepath.Join("builds", "x"), filepath.Join(data, "current"))
	version = ""
	if got := versionLines(data); got != "fyr 0123456789ab (main)\ncurrent: fyrd v1.2.3 (build 0123456789ab)\n" {
		t.Errorf("with a build: %q", got)
	}
}
