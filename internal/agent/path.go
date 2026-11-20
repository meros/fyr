package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PathInput is what the agents' PATH is made from (decision 0007,
// V0.linux-without-nix).
type PathInput struct {
	// Config: the config's agentPath, put first; `~/` is the home folder.
	Config []string
	// Unit: the PATH fyrd started with (a NixOS unit's packages, systemd's
	// default on another Linux, launchd's on a Mac).
	Unit string
	// Login: the PATH of the user's login shell (LoginPath), "" when it
	// could not be read.
	Login      string
	Home, User string
	// Exists reports whether a directory is there; the profile folders
	// below are added only when it says yes.
	Exists func(string) bool
}

// AgentPath is the PATH the agents get, so they find the same tools as a
// login shell: the config's agentPath, the unit's PATH, the login shell's,
// then the user's profile folders (NixOS's among them) that exist and are
// not there yet. Each folder is kept once, where it first appears.
func AgentPath(in PathInput) string {
	have := map[string]bool{}
	parts := []string{}
	add := func(p string) {
		if p != "" && !have[p] {
			have[p] = true
			parts = append(parts, p)
		}
	}
	for _, p := range in.Config {
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			p = filepath.Join(in.Home, rest)
		}
		add(p)
	}
	for _, path := range []string{in.Unit, in.Login} {
		for _, p := range strings.Split(path, ":") {
			add(p)
		}
	}
	for _, p := range []string{
		"/run/wrappers/bin",
		filepath.Join(in.Home, ".local", "bin"),
		filepath.Join(in.Home, ".nix-profile", "bin"),
		filepath.Join("/etc/profiles/per-user", in.User, "bin"),
		"/run/current-system/sw/bin",
	} {
		if !have[p] && in.Exists(p) {
			add(p)
		}
	}
	return strings.Join(parts, ":")
}

// loginMark precedes the PATH in the login shell's output, so a profile that
// prints something does not end up in it.
const loginMark = "__FYR_LOGIN_PATH__="

// LoginPath is the PATH a login shell of the user has: shell -l -c, with no
// input, within timeout. A profile that waits or fails gives an error.
func LoginPath(shell string, timeout time.Duration) (string, error) {
	if shell == "" {
		return "", errors.New("no login shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", `printf '\n`+loginMark+`%s\n' "$PATH"`)
	// Stdin stays nil: /dev/null, so a profile that reads input gets EOF.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", errors.New(shell + " -l did not finish in " + timeout.String())
	}
	lines := bytes.Split(out, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if p, ok := bytes.CutPrefix(lines[i], []byte(loginMark)); ok {
			return string(p), nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", errors.New(shell + " -l printed no PATH")
}

// LoginShell is the user's login shell: $SHELL (systemd sets it for a
// unit's User=), else the user's entry in /etc/passwd, else "".
func LoginShell(user string) string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	return passwdShell(string(b), user)
}

// passwdShell is the shell field of user's line in an /etc/passwd.
func passwdShell(passwd, user string) string {
	for _, line := range strings.Split(passwd, "\n") {
		f := strings.Split(line, ":")
		if len(f) == 7 && f[0] == user {
			return strings.TrimSpace(f[6])
		}
	}
	return ""
}

// DirExists reports whether a path is a directory.
func DirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
