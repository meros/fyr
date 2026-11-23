package runner

import (
	"os"
	"path/filepath"
	"testing"
)

// V0.runner-lookpath: a bare binary name is found on the PATH the process is
// given, not only on the runner's own (the launcher keeps the unit's PATH).
func TestCommandFindsTheBinaryOnTheGivenPATH(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fyr-agent-cli-for-test")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/nonexistent-for-test") // the runner's own PATH has no such CLI
	env := []string{"HOME=/tmp", "PATH=/usr/bin:" + dir}
	cmd := command("fyr-agent-cli-for-test", nil, env)
	if cmd.Path != bin || cmd.Err != nil {
		t.Fatalf("path %q err %v, want %q", cmd.Path, cmd.Err, bin)
	}
	cmd.Env = env
	if out, err := cmd.Output(); err != nil || string(out) != "hi\n" {
		t.Fatalf("run: %q %v", out, err)
	}
}

func TestCommandKeepsAnAbsolutePathAndReportsAMissingName(t *testing.T) {
	if c := command("/bin/sh", []string{"-c", "true"}, []string{"PATH=/nowhere"}); c.Path != "/bin/sh" {
		t.Fatalf("absolute path changed: %q", c.Path)
	}
	t.Setenv("PATH", "/nonexistent-for-test")
	c := command("fyr-no-such-cli", nil, []string{"PATH=/also-nonexistent"})
	if c.Err == nil {
		t.Fatalf("a missing CLI is not reported: %q", c.Path)
	}
}

func TestStartLocalFindsTheBinaryOnTheGivenPATH(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fyr-agent-cli-for-test"), []byte("#!/bin/sh\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/nonexistent-for-test")
	p, err := StartLocal("fyr-agent-cli-for-test", nil, dir, []string{"PATH=" + dir}, "")
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	p.Kill()
}
