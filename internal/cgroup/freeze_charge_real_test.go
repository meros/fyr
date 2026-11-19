// V0.freeze-charge against the kernel: work that grew in cli/ and was then
// moved into undeclared/ and frozen keeps its pages charged to cli/, and the
// reclaim after the freeze must still move them to swap while the agent's
// CLI, running in cli/, keeps its own pages. It needs a delegated cgroup with
// swap allowed, so it runs only when asked:
//
//	go test -c -o /tmp/cg.test ./internal/cgroup
//	systemd-run --user --wait --pipe -p Delegate=yes --setenv=FYR_REAL_CGROUP=1 /tmp/cg.test -test.run TestRealFreezeChargeReclaimsCLIsCharge -test.v
//
// It holds about 340 MB for a few seconds.
package cgroup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	freezeChargeWork = 300 << 20 // the work: written once, then idle
	freezeChargeCLI  = 40 << 20  // the CLI: written over and over
)

var freezeChargeHeld []byte

// TestRealFreezeChargeChild is the child the real test starts: "work" or "cli".
func TestRealFreezeChargeChild(t *testing.T) {
	role := os.Getenv("FYR_FREEZE_CHARGE_CHILD")
	if role == "" {
		t.Skip("only as the real test's child")
	}
	bufio.NewReader(os.Stdin).ReadString('\n') // moved into cli/: go
	n := freezeChargeWork
	if role == "cli" {
		n = freezeChargeCLI
	}
	freezeChargeHeld = make([]byte, n)
	page := os.Getpagesize()
	touch := func(v byte) {
		for i := 0; i < n; i += page {
			freezeChargeHeld[i] = v
		}
	}
	touch(1)
	fmt.Println("ready")
	for v := byte(2); ; v++ {
		if role == "cli" {
			touch(v)
			time.Sleep(20 * time.Millisecond)
		} else {
			time.Sleep(time.Hour)
		}
	}
}

func procStatus(pid int, key string) uint64 {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	for _, ln := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(ln, ":"); ok && k == key {
			n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			return n << 10
		}
	}
	return 0
}

func TestRealFreezeChargeReclaimsCLIsCharge(t *testing.T) {
	if os.Getenv("FYR_REAL_CGROUP") == "" {
		t.Skip("FYR_REAL_CGROUP is not set")
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(strings.TrimSpace(string(raw)), "0::"))
	self, agents := filepath.Join(root, "self"), filepath.Join(root, "agents")
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
	put(agents, "memory.swap.max", strconv.Itoa(1<<30)) // swapRoom: 900 MB
	tr := &Tree{root: root, logf: t.Logf, refs: map[string]int{}, focus: map[string]focusBeat{}, raised: map[string]bool{}, weights: map[string]string{}}
	cli := tr.Thread("t_real")
	if cli == "" {
		t.Fatal("no thread dir")
	}
	td := filepath.Dir(cli)
	start := func(role string) (*exec.Cmd, io.WriteCloser) {
		c := exec.Command(os.Args[0], "-test.run", "^TestRealFreezeChargeChild$")
		c.Env = append(os.Environ(), "FYR_FREEZE_CHARGE_CHILD="+role, "GOGC=off")
		in, _ := c.StdinPipe()
		out, _ := c.StdoutPipe()
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		if err := put(cli, "cgroup.procs", strconv.Itoa(c.Process.Pid)); err != nil {
			t.Fatal(err)
		}
		io.WriteString(in, "go\n")
		if l, _ := bufio.NewReader(out).ReadString('\n'); strings.TrimSpace(l) != "ready" {
			t.Fatalf("%s: %q", role, l)
		}
		return c, in
	}
	g := &JobGroups{Tree: tr, Size: func(string, bool) JobSize { return JobSize{} }}
	ccmd, _ := start("cli")
	wcmd, _ := start("work")
	t.Cleanup(func() {
		g.ThawUndeclared("t_real")
		for _, c := range []*exec.Cmd{ccmd, wcmd} {
			c.Process.Kill()
			c.Wait()
		}
		time.Sleep(200 * time.Millisecond)
		os.Remove(filepath.Join(td, undeclaredDir))
		tr.Release("t_real")
		os.Remove(agents)
	})
	time.Sleep(time.Second) // the CLI's pages are hot
	ud := filepath.Join(td, undeclaredDir)
	g.reclaimed = map[string]time.Time{"t_real": time.Now()} // the freeze's own reclaim waits: this one is measured
	if !g.FreezeUndeclared("t_real", []Root{{PID: wcmd.Process.Pid}}) {
		t.Fatal("not frozen")
	}
	mb := func(b uint64) string { return fmt.Sprintf("%.0f MB", float64(b)/(1<<20)) }
	tdBefore := readBytes(filepath.Join(td, "memory.current"))
	cliBefore := readBytes(filepath.Join(cli, "memory.current"))
	udBefore := readBytes(filepath.Join(ud, "memory.current"))
	workRSS := procStatus(wcmd.Process.Pid, "VmRSS")
	t.Logf("frozen: thread %s, cli/ %s, undeclared/ %s; the work's RSS %s, the CLI's %s",
		mb(tdBefore), mb(cliBefore), mb(udBefore), mb(workRSS), mb(procStatus(ccmd.Process.Pid, "VmRSS")))
	if udBefore > workRSS/4 {
		t.Fatalf("undeclared/ holds %s: the charge moved with the process, nothing to measure", mb(udBefore))
	}
	time.Sleep(time.Second)
	at := time.Now()
	moved := g.reclaimFrozen("t_real")
	took := time.Since(at)
	time.Sleep(time.Second) // the CLI runs on: what it lost it reads back
	tdAfter := readBytes(filepath.Join(td, "memory.current"))
	cliSwap := procStatus(ccmd.Process.Pid, "VmSwap")
	workSwap := procStatus(wcmd.Process.Pid, "VmSwap")
	t.Logf("reclaimed in %v: moved %s; thread %s → %s; the work in swap %s; the CLI in swap %s, resident %s",
		took, mb(moved), mb(tdBefore), mb(tdAfter), mb(workSwap), mb(cliSwap), mb(procStatus(ccmd.Process.Pid, "VmRSS")))
	if tdBefore < tdAfter || tdBefore-tdAfter < workRSS*7/10 {
		t.Fatalf("the thread gave up %s of the work's %s", mb(tdBefore-min(tdBefore, tdAfter)), mb(workRSS))
	}
	if workSwap < workRSS*7/10 {
		t.Fatalf("the work's swap %s of %s", mb(workSwap), mb(workRSS))
	}
	if cliSwap > freezeChargeCLI/10 {
		t.Fatalf("the CLI lost %s of its %s working set to swap", mb(cliSwap), mb(freezeChargeCLI))
	}
}
