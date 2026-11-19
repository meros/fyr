// V0.agents-unit against systemd and the kernel, in the user manager (the
// module's polkit rule is for the system manager; the mechanics are the
// same): a thread's CLI starts in a delegated scope of its own with
// memory.oom.group=1, fyrd makes job/ inside it, and an oomd-style kill of
// the scope (cgroup.kill) ends the thread and leaves the test (fyrd) alone.
//
//	FYR_REAL_SCOPES=1 go test ./internal/cgroup -run TestRealAgentsUnit -v
package cgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealAgentsUnit(t *testing.T) {
	if os.Getenv("FYR_REAL_SCOPES") == "" {
		t.Skip("set FYR_REAL_SCOPES=1 on a systemd host with a user manager")
	}
	l := &logs{}
	tr := Setup(Options{Budget: Budget{Enable: true, AgentsSlice: "fyr-agents-realtest.slice"}, userBus: true, Logf: l.logf})
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatalf("no scope layout: %v", l.lines)
	}
	t.Logf("%v", l.lines)
	_, wrap := tr.Launch("t_real")
	defer tr.Release("t_real")
	cmd := exec.Command(wrap[0], append(append([]string{}, wrap[1:]...), "sh", "-c", "sleep 300 & wait")...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	sd := filepath.Join(tr.AgentsDir(), "fyr-agent-t_real.scope")
	var in []int
	for deadline := time.Now().Add(10 * time.Second); len(in) < 2; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the scope %s holds %v", sd, in)
		}
		in = pids(filepath.Join(sd, "cgroup.procs"))
	}
	if tr.threadDir("t_real") != sd {
		t.Fatalf("thread dir %s", tr.threadDir("t_real"))
	}
	b, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "cgroup"))
	if !strings.Contains(string(b), "/fyr-agents-realtest.slice/fyr-agent-t_real.scope") {
		t.Fatalf("the CLI runs in %s", b)
	}
	if g := read(t, filepath.Join(sd, "memory.oom.group")); g != "1" {
		t.Fatalf("memory.oom.group %s", g)
	}
	// fyrd's job cgroup inside the scope: the delegation works.
	job := filepath.Join(sd, jobDir)
	if err := os.Mkdir(job, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range in {
		if p != cmd.Process.Pid {
			if err := os.WriteFile(filepath.Join(job, "cgroup.procs"), []byte(strconv.Itoa(p)), 0o644); err != nil {
				t.Fatalf("move %d into job/: %v", p, err)
			}
		}
	}
	// oomd's kill.
	if err := os.WriteFile(filepath.Join(sd, "cgroup.kill"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the thread was not killed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the thread still runs")
	}
	self, _ := os.ReadFile("/proc/self/cgroup")
	if strings.Contains(string(self), "fyr-agents-realtest.slice") {
		t.Fatalf("the test runs in the slice: %s", self)
	}
}
