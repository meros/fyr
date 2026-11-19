package cgroup

import (
	"os"
	"path/filepath"
	"testing"
)

// Decision 0036 (D): a service's pid in the thread's job or undeclared cgroup
// (or a part under it) is held; one in the thread's cli/ is not.
func TestJobGroupsHoldsAServiceInTheJobOrUndeclaredWork(t *testing.T) {
	root := t.TempDir()
	tr := &Tree{root: root}
	g := &JobGroups{Tree: tr}
	th := "t_1"
	td := g.threadDir(th)
	write := func(dir, procs string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(procs), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(td, "cli"), "10\n")
	write(filepath.Join(td, jobDir), "20\n")
	write(filepath.Join(td, undeclaredDir, "u30"), "30\n31\n")
	for pid, want := range map[int]bool{10: false, 20: true, 31: true, 99: false, 0: false} {
		if got := g.Holds(th, pid); got != want {
			t.Errorf("Holds(%d) = %v, want %v", pid, got, want)
		}
	}
}
