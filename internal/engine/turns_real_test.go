// Decision 0032 slice 2 under real memory pressure: the guard's engine and the
// real cgroup code, a runner with four heavy children, and a budget they do
// not fit in. It needs a delegated service, so it runs only when asked:
//
//	go test -c -o /tmp/engine.test ./internal/engine
//	systemd-run --user --wait --pipe -p Delegate=yes --setenv=FYR_REAL_CGROUP=1 \
//	  --setenv=FYR_PROOF_PYTHON=$(command -v python3) /tmp/engine.test -test.run TestRealGuardTakesTurns -test.v -test.timeout 20m
package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/slots"
)

const proofHog = `import os, sys
mb, touches = int(sys.argv[1]), int(sys.argv[2])
n, page = mb * 1024 * 1024, 4096
buf = bytearray(n)
for off in range(0, n, page):
    buf[off:off + 32] = os.urandom(32)
pages = n // page
x = os.getpid() | 1
for _ in range(touches):
    x = (x * 6364136223846793005 + 1442695040888963407) & 0xFFFFFFFFFFFFFFFF
    buf[((x >> 33) % pages) * page + 40] = x & 255
`

// proofRunner starts the runner in the thread's cgroup; its children are born there.
func proofRunner(t *testing.T, td string) (*exec.Cmd, chan error) {
	t.Helper()
	hog := filepath.Join(t.TempDir(), "hog.py")
	if err := os.WriteFile(hog, []byte(proofHog), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `read go; for i in 1 2 3 4; do "$0" -I "$1" 300 40000000 & done; wait`, os.Getenv("FYR_PROOF_PYTHON"), hog)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	if err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(in, "go")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd, done
}

func killThreadWork(td string) {
	filepath.WalkDir(td, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == "cgroup.kill" {
			os.WriteFile(p, []byte("1"), 0o644)
		}
		return nil
	})
}

// runningUnits counts the unit cgroups that hold processes and are not frozen.
func runningUnits(td string) int {
	n := 0
	dirs, _ := filepath.Glob(filepath.Join(td, "undeclared", "u*"))
	for _, d := range dirs {
		b, _ := os.ReadFile(filepath.Join(d, "cgroup.events"))
		s := string(b)
		if strings.Contains(s, "populated 1") && strings.Contains(s, "frozen 0") {
			n++
		}
	}
	return n
}

func TestRealGuardTakesTurns(t *testing.T) {
	if os.Getenv("FYR_REAL_CGROUP") == "" {
		t.Skip("FYR_REAL_CGROUP is not set")
	}
	const limit = 4 * time.Minute
	tr := cgroup.Setup(cgroup.Options{Budget: cgroup.Budget{Enable: true, AgentsMemoryHigh: "450M"}, Logf: t.Logf})
	if tr == nil {
		t.Fatal("no cgroup tree")
	}
	g := &cgroup.JobGroups{Tree: tr}

	// Without the guard.
	off := tr.Thread("t_off")
	start := time.Now()
	_, done := proofRunner(t, off)
	unguarded := limit
	select {
	case <-done:
		unguarded = time.Since(start)
	case <-time.After(limit):
		t.Logf("without the guard: not done in %s", limit)
	}
	killThreadWork(off)
	t.Logf("without the guard: %s, read back %s", unguarded.Round(100*time.Millisecond), slots.GB(float64(g.MajorFaultBytes("t_off"))))
	time.Sleep(2 * time.Second)

	// With it: the engine's own tick, every 10 s as in fyrd.
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	td := tr.Thread(a)
	*now = time.Now()
	start = time.Now()
	cmd, done := proofRunner(t, td)
	roots := []cgroup.Root{{PID: cmd.Process.Pid}}
	e.UndeclaredFreeze = func(o string) bool { return g.FreezeUndeclared(o, roots) }
	e.UndeclaredThaw = g.ThawUndeclared
	e.UndeclaredEmpty = g.UndeclaredEmpty
	e.UndeclaredFaults = g.MajorFaultBytes
	e.UndeclaredUnits = func(o string) []slots.WorkUnit {
		var out []slots.WorkUnit
		for _, x := range g.Units(o, roots) {
			out = append(out, slots.WorkUnit{PID: x.PID, RSS: x.RSS, Command: "hog"})
		}
		return out
	}
	turns := map[int]bool{}
	e.UndeclaredRotate = func(o string, us []slots.WorkUnit, active int) bool {
		cu := make([]cgroup.Unit, 0, len(us))
		for _, x := range us {
			cu = append(cu, cgroup.Unit{PID: x.PID, RSS: x.RSS})
		}
		turns[active] = true
		return g.RotateUndeclared(o, cu, active)
	}
	guardDeps(t, e)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	time.Sleep(3 * time.Second) // the children have memory to split by
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	*now = time.Now()
	e.Tick()
	guarded, maxRunning := time.Duration(0), 0
	timeout := time.After(limit)
loop:
	for {
		select {
		case <-done:
			guarded = time.Since(start)
			break loop
		case <-timeout:
			t.Fatalf("with the guard: not done in %s", limit)
		case <-tick.C:
			*now = time.Now()
			e.Tick()
			time.Sleep(300 * time.Millisecond) // a freeze takes effect
			if n := runningUnits(td); n > maxRunning {
				maxRunning = n
			}
		}
	}
	faults := g.MajorFaultBytes(a)
	killThreadWork(td)
	g.ThawUndeclared(a)
	t.Logf("with the guard: %s, read back %s, parts that had a turn %d, most parts running at once %d", guarded.Round(100*time.Millisecond), slots.GB(float64(faults)), len(turns), maxRunning)
	if len(turns) < 4 {
		t.Errorf("only %d parts had a turn", len(turns))
	}
	if maxRunning > 1 {
		t.Errorf("%d parts ran at once", maxRunning)
	}
	if guarded >= unguarded*7/10 {
		t.Errorf("the guard did not help: %s against %s", guarded, unguarded)
	}
}
