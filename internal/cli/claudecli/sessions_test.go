package claudecli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionFileFindsCutDirs(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "projects", "-a-b-c"), 0o700)
	os.MkdirAll(filepath.Join(d, "projects", "-cut-123"), 0o700)
	os.WriteFile(filepath.Join(d, "projects", "-a-b-c", "s1.jsonl"), nil, 0o600)
	os.WriteFile(filepath.Join(d, "projects", "-cut-123", "s2.jsonl"), nil, 0o600)
	if got := SessionFile(d, "/a/b.c", "s1"); got != filepath.Join(d, "projects", "-a-b-c", "s1.jsonl") {
		t.Errorf("exact: %q", got)
	}
	if got := SessionFile(d, "/somewhere/else", "s2"); got != filepath.Join(d, "projects", "-cut-123", "s2.jsonl") {
		t.Errorf("glob: %q", got)
	}
	if got := SessionFile(d, "/a", "../x"); got != "" {
		t.Errorf("bad id: %q", got)
	}
}
