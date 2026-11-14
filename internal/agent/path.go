package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// UserPath appends the user's profile directories to a unit's PATH, so the
// agents Fyr starts find the same tools as a login shell (decision 0007).
// Directories already present or missing on disk are skipped.
func UserPath(path, home, user string, exists func(string) bool) string {
	have := map[string]bool{}
	parts := []string{}
	for _, p := range strings.Split(path, ":") {
		if p != "" && !have[p] {
			have[p] = true
			parts = append(parts, p)
		}
	}
	for _, p := range []string{
		"/run/wrappers/bin",
		filepath.Join(home, ".nix-profile", "bin"),
		filepath.Join("/etc/profiles/per-user", user, "bin"),
		"/run/current-system/sw/bin",
	} {
		if !have[p] && exists(p) {
			have[p] = true
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ":")
}

// DirExists reports whether a path is a directory.
func DirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
