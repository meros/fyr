// V0.rerun-classes (decision 0036 (F)): the owner's rerunnable classes, kept per host; the kill of a job's cgroup; the
// learned peak raised after a kill.
package cgroup

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestRerunClassesAreKeptAndMatch(t *testing.T) {
	store := kv{}
	r := &RerunClasses{KV: store}
	r.Load()
	if len(r.List()) != 0 || r.Match("make test", "make test") != "" {
		t.Fatal("something is rerunnable by default")
	}
	for _, c := range []string{"make test", "  yarn   lint ", "make test"} {
		if err := r.Set(map[string]any{"add": c}); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.List(); len(got) != 2 || got[1] != "yarn lint" {
		t.Fatalf("list %q", got)
	}
	again := &RerunClasses{KV: store}
	again.Load()
	if len(again.List()) != 2 {
		t.Fatalf("not kept: %q", again.List())
	}
	for _, c := range []struct{ class, cmd, want string }{
		{"make test", "", "make test"},
		{"fyr: make test", "", "make test"}, // a job record's class carries its project
		{"", "cd web && make test 2>&1 | tail", "make test"},
		{"", "make testdata", ""},
		{"", "yarn lint --fix", "yarn lint"},
		{"go test ./...", "go test ./...", ""},
	} {
		if got := r.Match(c.class, c.cmd); got != c.want {
			t.Errorf("Match(%q, %q) = %q, want %q", c.class, c.cmd, got, c.want)
		}
	}
	if err := r.Set(map[string]any{"remove": "make test"}); err != nil || r.Match("make test", "") != "" {
		t.Fatalf("remove: %v %q", err, r.List())
	}
	if err := r.Set(map[string]any{"remove": "nope"}); err == nil {
		t.Fatal("removed an unknown class")
	}
	if err := r.Set(map[string]any{"add": " "}); err == nil {
		t.Fatal("added an empty class")
	}
}

func TestKillJobWritesCgroupKill(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	tr.Thread("t_a") // the CLI's cli/ leaf (V0.job-limits); the job sits beside it
	g := &JobGroups{Tree: tr}
	td := g.threadDir("t_a")
	if n := g.KillJob("t_a", false); n != 0 {
		t.Fatalf("killed %d without a job", n)
	}
	// Pids that do not exist: process_mrelease finds nothing to open, the kill is the file write.
	write(t, filepath.Join(td, "job", "cgroup.procs"), "4000001\n4000002\n")
	write(t, filepath.Join(td, "undeclared", "cgroup.procs"), "")
	write(t, filepath.Join(td, "undeclared", "u1", "cgroup.procs"), "4000003\n")
	if n := g.KillJob("t_a", false); n != 2 || read(t, filepath.Join(td, "job", "cgroup.kill")) != "1" {
		t.Fatalf("job: killed %d", n)
	}
	if n := g.KillJob("t_a", true); n != 1 || read(t, filepath.Join(td, "undeclared", "cgroup.kill")) != "1" {
		t.Fatalf("undeclared work and its parts: killed %d", n)
	}
}

func TestKillJobWithoutCgroupsKillsWhatWasStopped(t *testing.T) {
	var sent []int
	g := &JobGroups{Trees: &TreeReader{}, CLIs: func() map[string]int { return nil }, KV: &memKV{},
		Kill: func(pid int, sig syscall.Signal) error {
			if sig == syscall.SIGKILL {
				sent = append(sent, pid)
			}
			return nil
		}}
	g.stopped = map[string][]int{stopJob + "t_a": {4000001, 4000002}}
	if n := g.KillJob("t_a", false); n != 2 || len(sent) != 2 {
		t.Fatalf("killed %d, signals %v", n, sent)
	}
	if n := g.KillJob("t_a", false); n != 0 {
		t.Fatalf("killed %d the second time", n)
	}
}

func TestRaisePeakAfterAKill(t *testing.T) {
	j := &Jobs{KV: kv{}}
	if j.RaisePeak("fyr: make test", 1.2) {
		t.Fatal("raised an unknown class")
	}
	j.mu.Lock()
	j.load()
	j.table = map[string]resource.Learned{"fyr: make test": {Peak: 1000, Runs: 3}}
	j.mu.Unlock()
	if !j.RaisePeak("fyr: make test", 1.2) {
		t.Fatal("not raised")
	}
	if l, _ := j.Learned("fyr: make test"); l.Peak != 1200 {
		t.Fatalf("peak %d", l.Peak)
	}
}
