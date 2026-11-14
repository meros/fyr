package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// SPEC 0.5 V0.45: where a profile's CLI keeps its sessions.
func TestConfigDirPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	h, _ := os.UserHomeDir()
	cases := []struct {
		p    Profile
		want string
	}{
		{Profile{ConfigDir: "/x", Env: []string{"CLAUDE_CONFIG_DIR=/y"}}, "/x"},
		{Profile{ConfigDir: "~/.claude-b"}, filepath.Join(h, ".claude-b")},
		{Profile{Env: []string{"A=1", "CLAUDE_CONFIG_DIR=/y"}}, "/y"},
		{Profile{}, filepath.Join(h, ".claude")},
	}
	for _, c := range cases {
		if got := c.p.ConfigDirPath(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.p, got, c.want)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/daemon")
	if got := (Profile{}).ConfigDirPath(); got != "/daemon" {
		t.Errorf("daemon env: %q", got)
	}
}

func TestSessionFileFindsCutDirs(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "projects", "-a-b-c"), 0o700)
	os.MkdirAll(filepath.Join(d, "projects", "-cut-123"), 0o700)
	os.WriteFile(filepath.Join(d, "projects", "-a-b-c", "s1.jsonl"), nil, 0o600)
	os.WriteFile(filepath.Join(d, "projects", "-cut-123", "s2.jsonl"), nil, 0o600)
	if got := sessionFile(d, "/a/b.c", "s1"); got != filepath.Join(d, "projects", "-a-b-c", "s1.jsonl") {
		t.Errorf("exact: %q", got)
	}
	if got := sessionFile(d, "/somewhere/else", "s2"); got != filepath.Join(d, "projects", "-cut-123", "s2.jsonl") {
		t.Errorf("glob: %q", got)
	}
	if got := sessionFile(d, "/a", "../x"); got != "" {
		t.Errorf("bad id: %q", got)
	}
}
