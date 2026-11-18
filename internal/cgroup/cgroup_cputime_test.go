package cgroup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// V0.195: a thread's CPU time is its cgroup's usage_usec; no cgroup, no reading.
func TestTreeCPUTime(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "agents", "t_1")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "cpu.stat"), []byte("usage_usec 2500000\nuser_usec 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := &Tree{root: root}
	if got, ok := tr.CPUTime("t_1"); !ok || got != 2500*time.Millisecond {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := tr.CPUTime("t_2"); ok {
		t.Fatal("a reading without a cgroup")
	}
	if _, ok := (*Tree)(nil).CPUTime("t_1"); ok {
		t.Fatal("a reading without a tree")
	}
}
