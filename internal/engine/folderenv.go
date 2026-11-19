package engine

// V0.run-folder-env (#355): a Run card runs in its folder's environment, as
// the agent's Bash there does. When the folder or a folder above it has an
// .envrc that direnv allows, the script runs through `direnv exec <dir>`, so
// what the workspace's .envrc and shell.nix give (gcloud on PATH,
// CLOUDSDK_CONFIG) is there. Without direnv, an .envrc, or the owner's allow,
// the script runs as before. A root run keeps its own PATH (rootArgv).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// direnvPaths are where direnv lives when fyrd's PATH does not have it (a
// service's PATH is short): NixOS's per-user and system profiles, Nix's
// profile, Homebrew.
func direnvPaths(home, user string) []string {
	return []string{
		filepath.Join("/etc/profiles/per-user", user, "bin/direnv"),
		filepath.Join(home, ".nix-profile/bin/direnv"),
		"/run/current-system/sw/bin/direnv",
		"/nix/var/nix/profiles/default/bin/direnv",
		"/opt/homebrew/bin/direnv",
		"/usr/local/bin/direnv",
	}
}

// findDirenv is the direnv to run, or "".
func findDirenv() string {
	if p, err := exec.LookPath("direnv"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	for _, p := range direnvPaths(home, user) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// hasEnvrc reports an .envrc in dir or a folder above it.
func hasEnvrc(dir string) bool {
	for d := filepath.Clean(dir); ; {
		if _, err := os.Stat(filepath.Join(d, ".envrc")); err == nil {
			return true
		}
		up := filepath.Dir(d)
		if up == d {
			return false
		}
		d = up
	}
}

// direnvStatus is what `direnv status --json` says of the folder's .envrc.
type direnvStatus struct {
	State struct {
		FoundRC *struct {
			Allowed int    `json:"allowed"` // 0: allowed, 1: not allowed, 2: denied
			Path    string `json:"path"`
		} `json:"foundRC"`
	} `json:"state"`
}

// allowedRC reports that status names an .envrc the owner allowed.
func allowedRC(status []byte) bool {
	var s direnvStatus
	if json.Unmarshal(status, &s) != nil {
		return false
	}
	return s.State.FoundRC != nil && s.State.FoundRC.Path != "" && s.State.FoundRC.Allowed == 0
}

// folderArgv wraps argv in `direnv exec dir` when dir has an allowed .envrc.
// status reads `direnv status --json` in dir.
func folderArgv(dir string, argv []string, direnv string, status func(direnv, dir string) ([]byte, error)) []string {
	if dir == "" || direnv == "" || !hasEnvrc(dir) {
		return argv
	}
	out, err := status(direnv, dir)
	if err != nil || !allowedRC(out) {
		return argv
	}
	return append([]string{direnv, "exec", dir}, argv...)
}

// FolderArgv is folderArgv with this host's direnv: what fyrd gives the
// engine as FolderEnv.
func FolderArgv(dir string, argv []string) []string {
	return folderArgv(dir, argv, findDirenv(), func(direnv, dir string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, direnv, "status", "--json")
		cmd.Dir = dir
		return cmd.Output()
	})
}
