// Package scripts holds tests of the install scripts.
package scripts

import (
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
	if runtime.GOOS != "linux" {
		t.Skip("install-linux.sh is for Linux")
	}
	if os.Getuid() == 0 {
		t.Skip("the script refuses root")
	}
	bin, home := t.TempDir(), t.TempDir()
	stub := "#!/bin/sh\nexit 1\n"
	if pk != "" {
		stub = "#!/bin/sh\necho pkaction version " + pk + "\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "pkaction"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "install-linux.sh", "--print")
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--print: %v\n%s", err, out)
	}
	return string(out), home
}

// V0.linux-without-nix: --print changes nothing and asks no sudo.
func TestInstallLinuxPrintWritesNothing(t *testing.T) {
	out, home := printInstall(t, "124")
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
