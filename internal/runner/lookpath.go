package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// command is exec.Command with a bare binary name looked up on the PATH of
// env, the environment the process will run with (V0.runner-lookpath).
// exec.Command looks a bare name up on the calling process's own PATH: the
// runner lives in the launcher, which keeps the unit's minimal PATH, while
// fyrd hands the agent the login shell's PATH (V0.linux-without-nix). On a
// plain Linux install the CLI is in ~/.local/bin, on the agent's PATH and not
// on the launcher's, so no thread could start.
func command(bin string, args []string, env []string) *exec.Cmd {
	if bin != "" && !strings.ContainsRune(bin, filepath.Separator) {
		if p := lookIn(bin, pathOf(env)); p != "" {
			bin = p
		}
	}
	return exec.Command(bin, args...)
}

// pathOf is the PATH in env (the last one wins, as in exec), or this
// process's own PATH when env is nil (exec then inherits it).
func pathOf(env []string) string {
	if env == nil {
		return os.Getenv("PATH")
	}
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	return path
}

// lookIn is the first executable file named bin in the folders of path, or
// "" when there is none (exec.Command then reports it as before).
func lookIn(bin, path string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, bin)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}
