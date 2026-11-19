// V0.191 against the kernel. It needs a delegated cgroup, so it runs only when asked:
//
//	go test -c -o /tmp/cg.test ./internal/cgroup
//	systemd-run --user --scope -p Delegate=yes env FYR_REAL_CGROUP=1 /tmp/cg.test -test.run TestRealRefreeze -test.v
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

func TestRealRefreeze(t *testing.T) {
	if os.Getenv("FYR_REAL_CGROUP") == "" {
		t.Skip("FYR_REAL_CGROUP is not set")
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(strings.TrimSpace(string(raw)), "0::"))
	g := &JobGroups{Tree: &Tree{root: root, logf: t.Logf}} // V0.job-limits: split logs a refusal
	td := g.threadDir("t_a")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	count := filepath.Join(t.TempDir(), "n")
	cmd := exec.Command("sh", "-c", `while :; do echo x >> "$0"; sleep 0.05; done`, count)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		g.ThawUndeclared("t_a")
		cmd.Process.Kill()
		cmd.Wait()
		time.Sleep(200 * time.Millisecond)
		g.EndUndeclared("t_a")
		os.Remove(td)
		os.Remove(filepath.Dir(td))
	})
	if err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	grows := func() bool {
		time.Sleep(300 * time.Millisecond)
		a, _ := os.ReadFile(count)
		time.Sleep(500 * time.Millisecond)
		b, _ := os.ReadFile(count)
		return len(b) > len(a)
	}
	roots := []Root{{PID: cmd.Process.Pid}}
	for round := 1; round <= 2; round++ {
		if !g.FreezeUndeclared("t_a", roots) {
			t.Fatalf("freeze %d failed", round)
		}
		if grows() {
			t.Fatalf("freeze %d holds nothing", round)
		}
		g.ThawUndeclared("t_a")
		if !grows() {
			t.Fatalf("thaw %d", round)
		}
	}
}
