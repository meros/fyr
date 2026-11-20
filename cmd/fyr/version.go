package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/meros/fyr/internal/proc"
)

// V0.versioned-releases: CI sets these with -ldflags "-X main.version=… -X
// main.buildID=…"; version is empty on main.
var (
	version = ""
	buildID = ""
)

// launcherBuild is this launcher's build id: the ldflag, else the VCS
// revision, else "dev".
func launcherBuild() string {
	if buildID != "" {
		return buildID
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value[:min(12, len(s.Value))]
			}
		}
	}
	return "dev"
}

// versionLines is `fyr --version`: the launcher's version (or build on
// main), then the line of the fyrd it runs (the current build updates
// itself; the launcher does not).
func versionLines(data string) string {
	l := "fyr " + launcherBuild() + " (main)"
	if version != "" {
		l = "fyr " + version + " (build " + launcherBuild() + ")"
	}
	exe := filepath.Join(data, "current", "fyrd")
	if _, err := os.Stat(exe); err != nil {
		return l + "\nno current build in " + data + "\n"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := proc.Command(ctx, exe, "--version").Output()
	if err != nil {
		return l + "\ncurrent: " + exe + " does not say its version (an older build)\n"
	}
	return l + "\ncurrent: " + strings.TrimSpace(string(out)) + "\n"
}
