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
