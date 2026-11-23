package cgroup

// V0.job-limits on a real cgroup v2 tree: the test process must own a
// delegated cgroup with cpu and memory, e.g.
//
//	systemd-run --user --scope -p Delegate=yes env FYR_REAL_CGROUP_SCOPE=1 go test -run TestRealJobLimits ./internal/cgroup
//
// It moves itself to a leaf (as Setup moves fyrd to server/), makes agents/,
// a thread whose CLI sits in cli/, and checks that the job fyrd moves to job/
// gets its own memory.high and cpu.max, and that the thread's cgroup itself
// can hold no process any more.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

func TestRealJobLimits(t *testing.T) {
	if os.Getenv("FYR_REAL_CGROUP_SCOPE") == "" {
		t.Skip("FYR_REAL_CGROUP_SCOPE is not set")
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(strings.TrimSpace(string(raw)), "0::"))
	self := filepath.Join(root, "self")
	agents := filepath.Join(root, "agents")
	for _, d := range []string{self, agents} {
		if err := os.Mkdir(d, 0o755); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
	}
	for _, p := range pids(filepath.Join(root, "cgroup.procs")) {
		os.WriteFile(filepath.Join(self, "cgroup.procs"), []byte(strconv.Itoa(p)), 0o644)
	}
	for _, d := range []string{root, agents} {
		if err := put(d, "cgroup.subtree_control", controllers); err != nil {
			t.Fatalf("controllers in %s: %v", d, err)
		}
	}
	put(agents, "memory.high", strconv.Itoa(4<<30))
	l := &logs{}
	tr := &Tree{root: root, logf: l.logf, refs: map[string]int{}, focus: map[string]focusBeat{}, raised: map[string]bool{}, weights: map[string]string{}}
	cli := tr.Thread("t_real")
	if cli == "" {
		t.Fatalf("no thread dir: %v", l.lines)
	}
	td := filepath.Dir(cli)
	cmd := exec.Command("sh", "-c", "exec sleep 30", "fyr-real-job-limits")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		time.Sleep(100 * time.Millisecond)
		os.Remove(filepath.Join(td, jobDir))
		tr.Release("t_real")
		os.Remove(agents)
	})
	if err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err == nil {
		t.Fatal("the thread's own cgroup took a process with controllers on for its children")
	}
	if err := os.WriteFile(filepath.Join(cli, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &JobGroups{Tree: tr, Limit: guard.JobLimit, Size: func(string, bool) resource.JobSize {
		return resource.JobSize{Class: "real", Peak: 100 << 20, Demand: 1.5, Known: true}
	}}
	g.Sync("t_real", "sleep 30")
	job := filepath.Join(td, jobDir)
	if got := strings.TrimSpace(string(must(os.ReadFile(filepath.Join(job, "cgroup.procs"))))); got != strconv.Itoa(cmd.Process.Pid) {
		t.Fatalf("job procs %q; log %v", got, l.lines)
	}
	if got := strings.TrimSpace(string(must(os.ReadFile(filepath.Join(job, "memory.high"))))); got != strconv.Itoa(125<<20) {
		t.Fatalf("memory.high %q; log %v", got, l.lines)
	}
	if got := strings.TrimSpace(string(must(os.ReadFile(filepath.Join(job, "cpu.max"))))); got != "200000 100000" {
		t.Fatalf("cpu.max %q", got)
	}
	t.Log(l.lines)
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
