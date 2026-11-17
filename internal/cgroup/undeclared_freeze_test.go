// V0.164 section 7 and 12.5 (F3, F11): the job roots of an undeclared episode and all their descendants move into
// agents/<thread>/undeclared/ and freeze; the CLI and what it keeps running stay where they are.
package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestF3TheRootsAndTheirDescendantsAreFrozenInTheirOwnCgroup(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := tr.Thread("t_a")
	proc := t.TempDir()
	fakeProc2(t, proc, 10, 1, 100, 10, "claude")
	fakeProc2(t, proc, 11, 10, 110, 10, "node", "mcp.js")
	fakeProc2(t, proc, 20, 10, 9000, 10, "/bin/zsh", "-c", "yarn pre-push:check")
	fakeProc2(t, proc, 21, 20, 9001, 10, "node", "a.js")
	fakeProc2(t, proc, 22, 21, 9002, 10, "node", "b.js")
	write(t, filepath.Join(td, "cgroup.procs"), "10\n11\n20\n21\n22\n")
	g := &JobGroups{Tree: tr, Proc: proc}
	if !g.FreezeUndeclared("t_a", []Root{{PID: 20}}) {
		t.Fatal("not frozen")
	}
	dir := filepath.Join(td, "undeclared")
	if read(t, filepath.Join(dir, "cgroup.freeze")) != "1" || !g.Frozen("t_a") || !g.HasUndeclared("t_a") {
		t.Fatal("freeze")
	}
	// The plain directory keeps the last pid written; the walk is roots, then descendants, in pid order.
	if got := read(t, filepath.Join(dir, "cgroup.procs")); got != "22" {
		t.Fatalf("the last process moved: %q", got)
	}
	// S5's job/ is a different directory: a declared and an undeclared job never mix.
	if _, err := os.Stat(filepath.Join(td, "job")); !os.IsNotExist(err) {
		t.Fatal("job/ made")
	}
	g.ThawUndeclared("t_a")
	if read(t, filepath.Join(dir, "cgroup.freeze")) != "0" || g.Frozen("t_a") {
		t.Fatal("thaw")
	}
	write(t, filepath.Join(dir, "cgroup.procs"), "")
	g.EndUndeclared("t_a")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the empty cgroup stays")
	}
}

func TestF11ARootThatExitsDuringTheMoveIsSkipped(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := tr.Thread("t_a")
	proc := t.TempDir()
	fakeProc2(t, proc, 20, 10, 9000, 10, "/bin/zsh", "-c", "x")
	fakeProc2(t, proc, 21, 20, 9001, 10, "node")
	write(t, filepath.Join(td, "cgroup.procs"), "20\n21\n99\n")
	g := &JobGroups{Tree: tr, Proc: proc}
	if !g.FreezeUndeclared("t_a", []Root{{PID: 99}, {PID: 20}}) {
		t.Fatal("an exited root failed the whole move")
	}
	if !strings.Contains(read(t, filepath.Join(td, "undeclared", "cgroup.procs")), "21") {
		t.Fatal("the rest was not moved")
	}
	// No roots at all: nothing to freeze.
	if g.FreezeUndeclared("t_b", nil) {
		t.Fatal("froze nothing")
	}
}

func TestBashCommandsAreKeptPerThreadNewestLast(t *testing.T) {
	g := &JobGroups{}
	for i := 0; i < 15; i++ {
		g.Bash("t_a", "cmd number "+string(rune('a'+i)))
	}
	got := g.Bashes("t_a")
	if len(got) != 10 || got[9] != "cmd number o" || got[0] != "cmd number f" {
		t.Fatalf("%v", got)
	}
}
