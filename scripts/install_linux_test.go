// Package scripts holds tests of the install scripts.
package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// printInstall runs install-linux.sh --print with a fake pkaction that
// reports polkit version pk ("" for none) and a fresh HOME.
func printInstall(t *testing.T, pk string) (string, string) {
	t.Helper()
	return printInstallPath(t, pk, os.Getenv("PATH"))
}

// printInstallPath is printInstall with path after the fake pkaction.
func printInstallPath(t *testing.T, pk, path string) (string, string) {
	t.Helper()
	return printInstallArgs(t, pk, path, t.TempDir())
}

// printInstallArgs runs install-linux.sh --print with args in home, on a
// systemd 257.
func printInstallArgs(t *testing.T, pk, path, home string, args ...string) (string, string) {
	t.Helper()
	return printInstallSystemd(t, pk, "257", path, home, args...)
}

// printInstallSystemd is printInstallArgs with a fake systemctl that reports
// systemd version sd.
func printInstallSystemd(t *testing.T, pk, sd, path, home string, args ...string) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("install-linux.sh is for Linux")
	}
	if os.Getuid() == 0 {
		t.Skip("the script refuses root")
	}
	bin := t.TempDir()
	stub := "#!/bin/sh\nexit 1\n"
	if pk != "" {
		stub = "#!/bin/sh\necho pkaction version " + pk + "\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "pkaction"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	sdStub := "#!/bin/sh\necho 'systemd " + sd + " (" + sd + ".1-1ubuntu1)'\necho '+PAM +AUDIT'\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(sdStub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{"install-linux.sh", "--print"}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--print: %v\n%s", err, out)
	}
	return string(out), home
}

// currentUser is the name the script reads with id -un.
func currentUser(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("id", "-un").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// V0.linux-without-nix: --print changes nothing and asks no sudo.
func TestInstallLinuxPrintWritesNothing(t *testing.T) {
	out, home := printInstall(t, "124")
	// V0.fresh-ubuntu-run: the fyr command goes on PATH as a link.
	if want := "as " + currentUser(t) + ": link " + home + "/.local/bin/fyr to " + home + "/.local/share/fyr/launcher/fyr (the fyr command)"; !strings.Contains(out, want) {
		t.Errorf("missing %q in\n%s", want, out)
	}
	if es, _ := os.ReadDir(home); len(es) != 0 {
		t.Fatalf("--print wrote into HOME: %v", es)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "sudo ") && !strings.Contains(line, "tailscale") {
			t.Fatalf("--print shows a sudo call: %q", line)
		}
	}
}

// A polkit with JavaScript rules gets the module's scope layout: the slice,
// the rule, the PSI helper and the cgroups keys naming the slice.
func TestInstallLinuxScopeLayout(t *testing.T) {
	out, _ := printInstall(t, "124")
	for _, want := range []string{
		"layout: scope",
		"ManagedOOMPreference=omit",
		"ExecStartPre=+/usr/local/lib/fyr/agents-pressure-chown",
		"---- fyr-agents.slice",
		"MemoryHigh=40%",
		`/^fyr-(agent|deploy)-[A-Za-z0-9_.-]+\.scope$/`,
		`unit == "fyr.service" && (verb == "start" || verb == "stop" || verb == "restart")`, // decision 0038: Pause and Resume
		`"agentsSlice": "fyr-agents.slice"`,
		"root: install -D -m 0644",
		"root: systemctl restart fyr.service",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Delegate=") {
		t.Errorf("the scope layout's unit is not delegated:\n%s", out)
	}
}

// Ubuntu 22.04's polkit 0.105 reads no JavaScript rules, and a host may have
// no polkit: the delegated V0.80 unit, with no slice or rule.
func TestInstallLinuxUnitLayout(t *testing.T) {
	for _, pk := range []string{"0.105", ""} {
		out, _ := printInstall(t, pk)
		for _, want := range []string{"layout: unit", "Delegate=cpu memory", "root: rm -f /etc/systemd/system/fyr-agents.slice"} {
			if !strings.Contains(out, want) {
				t.Errorf("polkit %q: missing %q in\n%s", pk, want, out)
			}
		}
		for _, not := range []string{"---- fyr-agents.slice", "agentsSlice", "ExecStartPre"} {
			if strings.Contains(out, not) {
				t.Errorf("polkit %q: unexpected %q in\n%s", pk, not, out)
			}
		}
	}
}

// V0.fresh-ubuntu-run: systemd before 257 (Debian 12's 252, Ubuntu 24.04's
// 255) asks polkit about a transient scope with no unit and no verb, so the
// rule cannot grant the agents' scopes even with polkit 124: the unit layout.
func TestInstallLinuxOldSystemdUnitLayout(t *testing.T) {
	for _, sd := range []string{"252", "255", ""} {
		out, _ := printInstallSystemd(t, "124", sd, os.Getenv("PATH"), t.TempDir())
		for _, want := range []string{"layout: unit (systemd", "Delegate=cpu memory"} {
			if !strings.Contains(out, want) {
				t.Errorf("systemd %q: missing %q in\n%s", sd, want, out)
			}
		}
		for _, not := range []string{"---- fyr-agents.slice", "agentsSlice", "ExecStartPre"} {
			if strings.Contains(out, not) {
				t.Errorf("systemd %q: unexpected %q in\n%s", sd, not, out)
			}
		}
	}
	out, _ := printInstallSystemd(t, "124", "257", os.Getenv("PATH"), t.TempDir())
	if !strings.Contains(out, "layout: scope") {
		t.Errorf("systemd 257: not the scope layout:\n%s", out)
	}
}

// V0.release-without-gh: the install reads the release with git and curl
// from the public URLs; with no gh on PATH --print still passes and asks for
// no gh login.
func TestInstallLinuxNeedsNoGh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("install-linux.sh is for Linux")
	}
	// Every tool on PATH but gh.
	noGh := t.TempDir()
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		es, _ := os.ReadDir(d)
		for _, e := range es {
			if e.Name() == "gh" {
				continue
			}
			os.Symlink(filepath.Join(d, e.Name()), filepath.Join(noGh, e.Name())) // the first one wins
		}
	}
	out, _ := printInstallPath(t, "124", noGh)
	for _, want := range []string{
		"by git ls-remote https://github.com/meros/fyr.git and curl https://github.com/meros/fyr/releases/download/<tag>/ (no gh login)",
		"the install needs none",
		"GH_TOKEN=... (mode 0600) for a private fork; a public one needs none.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, not := range []string{"gh auth", "gh api", "gh release", "missing gh", "not logged in"} {
		if strings.Contains(out, not) {
			t.Errorf("unexpected %q in\n%s", not, out)
		}
	}
	src, err := os.ReadFile("install-linux.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, not := range []string{"gh api", "gh release", "gh auth"} {
		if strings.Contains(string(src), not) {
			t.Errorf("install-linux.sh still runs %q", not)
		}
	}
}

// V0.versioned-releases: a new install follows stable; an existing config
// keeps its channel (none is main, so a host installed before channels does
// not change); --channel sets it; --version installs a version and pins to
// it unless --channel says otherwise.
func TestInstallLinuxChannel(t *testing.T) {
	path := os.Getenv("PATH")
	withConfig := func(body string) string {
		home := t.TempDir()
		os.MkdirAll(filepath.Join(home, ".config", "fyr"), 0o755)
		os.WriteFile(filepath.Join(home, ".config", "fyr", "config.json"), []byte(body), 0o644)
		return home
	}
	for _, c := range []struct {
		name string
		home string
		args []string
		want []string
		not  []string
	}{
		{"new install", t.TempDir(), nil,
			[]string{"channel: stable (updateChannel stable", "download the newest stable version (vX.Y.Z, not a prerelease), or while none is published main's newest build (and follow main)", `set "updateChannel" in`},
			[]string{"deployable"}},
		{"existing config without a channel", withConfig(`{"cgroups": {}}`), nil,
			[]string{"channel: main (kept from", "the release build-<sha12> of tag deployable"},
			[]string{`set "updateChannel"`}},
		{"existing config on stable", withConfig(`{"updateChannel": "stable"}`), nil,
			[]string{"channel: stable (kept from", "the newest stable version"},
			[]string{`set "updateChannel"`, "while none is published"}},
		{"--channel stable", t.TempDir(), []string{"--channel", "stable"},
			[]string{"channel: stable (updateChannel stable", "the newest stable version"},
			[]string{"while none is published"}},
		{"--channel main", t.TempDir(), []string{"--channel", "main"},
			[]string{"channel: main (updateChannel main", "tag deployable", `set "updateChannel" in`},
			nil},
		{"--version pins", withConfig(`{}`), []string{"--version", "v1.2.3"},
			[]string{"channel: v1.2.3 (updateChannel v1.2.3", "download version v1.2.3 in meros/fyr", `to "v1.2.3"`},
			nil},
		{"--version with --channel stable", t.TempDir(), []string{"--version=v1.0.0-rc.1", "--channel=stable"},
			[]string{"channel: stable (updateChannel stable", "download version v1.0.0-rc.1", `to "stable"`},
			nil},
	} {
		out, _ := printInstallArgs(t, "124", path, c.home, c.args...)
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q in\n%s", c.name, w, out)
			}
		}
		for _, n := range c.not {
			if strings.Contains(out, n) {
				t.Errorf("%s: unexpected %q in\n%s", c.name, n, out)
			}
		}
	}
}

// V0.versioned-releases: a channel or version that is neither is refused
// before anything runs.
func TestInstallLinuxBadChannel(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getuid() == 0 {
		t.Skip("install-linux.sh runs on Linux as a user")
	}
	for _, args := range [][]string{{"--channel", "beta"}, {"--version", "1.2.3"}, {"--version", "v1.2"}, {"--channel"}} {
		cmd := exec.Command("bash", append([]string{"install-linux.sh", "--print"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 64 {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
}
