// Decision 0032 slice 2 against the kernel. It needs a delegated cgroup, so it
// runs only when asked:
//
//	go test -c -o /tmp/cg.test ./internal/cgroup
//	systemd-run --user --scope -p Delegate=yes env FYR_REAL_CGROUP=1 /tmp/cg.test -test.run TestRealTurns -test.v
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

func TestRealTurns(t *testing.T) {
	if os.Getenv("FYR_REAL_CGROUP") == "" {
		t.Skip("FYR_REAL_CGROUP is not set")
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(strings.TrimSpace(string(raw)), "0::"))
	g := &JobGroups{Tree: &Tree{root: root}}
	td := g.threadDir("t_a")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// A runner with three children that each count as long as they run.
	script := `for n in 1 2 3; do (while :; do echo x >> "$0/$n"; sleep 0.05; done) & done; wait`
	cmd := exec.Command("sh", "-c", script, dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var kids []int
	t.Cleanup(func() {
		g.ThawUndeclared("t_a")
		for _, p := range kids {
			exec.Command("kill", "-9", strconv.Itoa(p)).Run()
		}
		cmd.Process.Kill()
		cmd.Wait()
		time.Sleep(200 * time.Millisecond)
		g.EndUndeclared("t_a")
		os.Remove(td)
		os.Remove(filepath.Dir(td))
	})
	for i := 0; i < 100 && len(kids) < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		kids = nil
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/task/" + strconv.Itoa(cmd.Process.Pid) + "/children")
		for _, f := range strings.Fields(string(b)) {
			n, _ := strconv.Atoi(f)
			kids = append(kids, n)
		}
	}
	if len(kids) != 3 {
		t.Fatalf("children %v", kids)
	}
	if err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range kids {
		if err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte(strconv.Itoa(p)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	units := []Unit{{PID: kids[0]}, {PID: kids[1]}, {PID: kids[2]}}
	lines := func(n int) int {
		b, _ := os.ReadFile(filepath.Join(dir, strconv.Itoa(n)))
		return strings.Count(string(b), "\n")
	}
	// Which counter belongs to which child is not known: the turn must leave
	// exactly one of them growing, and a new turn a different one.
	growing := func() []int {
		time.Sleep(300 * time.Millisecond) // lets a freeze settle
		before := [4]int{0, lines(1), lines(2), lines(3)}
		time.Sleep(700 * time.Millisecond)
		var out []int
		for n := 1; n <= 3; n++ {
			if lines(n) > before[n] {
				out = append(out, n)
			}
		}
		return out
	}
	if !g.RotateUndeclared("t_a", units, kids[0]) {
		t.Fatal("no rotation")
	}
	first := growing()
	if len(first) != 1 {
		t.Fatalf("running in the first turn: %v", first)
	}
	if !g.RotateUndeclared("t_a", units, kids[1]) {
		t.Fatal("no second turn")
	}
	second := growing()
	if len(second) != 1 || second[0] == first[0] {
		t.Fatalf("turns %v then %v", first, second)
	}
	if g.UndeclaredEmpty("t_a") {
		t.Fatal("reads as empty while the units live")
	}
	g.ThawUndeclared("t_a")
	if all := growing(); len(all) != 3 {
		t.Fatalf("running after the thaw: %v", all)
	}
}
