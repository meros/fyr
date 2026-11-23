// V0.freeze-charge: the guard's move gives undeclared work of an unknown class
// no memory.high, a job of an unknown class is never limited below the prior,
// and a freeze reclaims the frozen work's memory where the kernel charged it.
package cgroup

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

// 19 Nov 20:48: memory short, the guard moved t_5428's work into undeclared/
// and the unknown class got the agents' free memory, 73 MB, as its memory.high.
func TestFreezeChargeUndeclaredWorkOfAnUnknownClassGetsNoLimit(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Join(o.FS, unit, "agents")
	write(t, filepath.Join(agents, "memory.current"), "12930M") // 70 MB of 13 GB left
	proc := t.TempDir()
	fakeProc(t, proc, 10, 1, "claude")
	fakeProc(t, proc, 20, 10, "node", "e2e")
	write(t, filepath.Join(td, CLIDir, "cgroup.procs"), "10\n20\n")
	ud := filepath.Join(td, undeclaredDir)
	write(t, filepath.Join(ud, "memory.high"), "max")
	g := &JobGroups{Tree: tr, Proc: proc, N: 3, Limit: guard.JobLimit, Size: func(string, bool) resource.JobSize { return resource.JobSize{} }}
	if !g.FreezeUndeclared("t_a", []Root{{PID: 20}}) {
		t.Fatal("not frozen")
	}
	if got := read(t, filepath.Join(ud, "memory.high")); got != "max" {
		t.Fatalf("memory.high %q at the guard's move, want max: %v", got, l.lines)
	}
	// A learned class keeps its learned limit.
	g2 := &JobGroups{Tree: tr, Proc: proc, Limit: guard.JobLimit, Size: func(string, bool) resource.JobSize {
		return resource.JobSize{Class: "node e2e", Peak: 800 << 20, Known: true, Runs: 5}
	}}
	g2.sizeJob("t_a", ud, true)
	if got := read(t, filepath.Join(ud, "memory.high")); got != strconv.Itoa(1000<<20) {
		t.Fatalf("learned class: memory.high %q", got)
	}
}

// A job of an unknown class moved into job/ while memory is short is not held
// to the little that is free: never below the prior, 1/N of the agents.
func TestFreezeChargeAnUnknownJobIsNeverLimitedBelowThePrior(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Join(o.FS, unit, "agents")
	write(t, filepath.Join(agents, "memory.current"), "12930M")
	proc := t.TempDir()
	fakeProc(t, proc, 10, 1, "claude")
	fakeProc(t, proc, 20, 10, "make", "build")
	write(t, filepath.Join(td, CLIDir, "cgroup.procs"), "10\n20\n")
	g := &JobGroups{Tree: tr, Proc: proc, N: 4, Limit: guard.JobLimit, Size: func(string, bool) resource.JobSize { return resource.JobSize{Class: "fyr: make build"} }}
	g.Sync("t_a", "make build")
	page := uint64(os.Getpagesize())
	want := strconv.FormatUint((13<<30)/4/page*page, 10)
	if got := read(t, filepath.Join(td, jobDir, "memory.high")); got != want {
		t.Fatalf("memory.high %q, want the prior %s: %v", got, want, l.lines)
	}
}

// 19 Nov 20:48-20:54: the work frozen in undeclared/ held 11.6 GB, all charged
// to cli/ where it started; the reclaim asked undeclared/, which held nothing.
func TestFreezeChargeAFreezeReclaimsWhereTheWorkIsCharged(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Dir(td)
	write(t, filepath.Join(agents, "memory.swap.max"), "10240M") // room: 9216M
	write(t, filepath.Join(agents, "memory.swap.current"), "0")
	proc := t.TempDir()
	page := os.Getpagesize()
	fakeProc2(t, proc, 10, 1, 0, (200<<20)/page, "claude")       // the CLI: 200 MB its own
	fakeProc2(t, proc, 20, 10, 0, (2<<30)/page, "node", "e2e")   // frozen work: 3 GB
	fakeProc2(t, proc, 21, 20, 0, (1<<30)/page, "node", "child") //
	cli := filepath.Join(td, CLIDir)
	write(t, filepath.Join(cli, "cgroup.procs"), "10\n")
	write(t, filepath.Join(cli, "memory.stat"), "anon 3758096384\n") // 3.5 GB: the CLI's and the work's
	write(t, filepath.Join(cli, "memory.reclaim"), "")
	ud := filepath.Join(td, undeclaredDir)
	write(t, filepath.Join(ud, "cgroup.procs"), "20\n21\n")
	write(t, filepath.Join(ud, "cgroup.freeze"), "1")
	write(t, filepath.Join(ud, "memory.stat"), "anon 0\n")
	write(t, filepath.Join(ud, "memory.reclaim"), "")
	g := &JobGroups{Tree: tr, Proc: proc}

	g.reclaimFrozen("t_a")
	if got := read(t, filepath.Join(cli, "memory.reclaim")); got != "3221225472 swappiness=200" {
		t.Fatalf("cli/: reclaim %q, want the frozen work's 3 GiB, anon only", got)
	}
	if got := read(t, filepath.Join(ud, "memory.reclaim")); got != "" {
		t.Fatalf("undeclared/ holds nothing, reclaim %q", got)
	}

	// cli/ holds less than the work's resident size (shared pages, or charged
	// elsewhere): no more than what the CLI's own processes do not hold.
	write(t, filepath.Join(cli, "memory.stat"), "anon 1258291200\n") // 1.2 GB
	write(t, filepath.Join(cli, "memory.reclaim"), "")
	g.reclaimFrozen("t_a")
	if got := read(t, filepath.Join(cli, "memory.reclaim")); got != "1048576000 swappiness=200" {
		t.Fatalf("cli/: reclaim %q, want 1.2 GB less the CLI's 200 MB", got)
	}

	// Nothing frozen: cli/ is left alone.
	write(t, filepath.Join(ud, "cgroup.freeze"), "0")
	write(t, filepath.Join(cli, "memory.reclaim"), "")
	g.reclaimFrozen("t_a")
	if got := read(t, filepath.Join(cli, "memory.reclaim")); got != "" {
		t.Fatalf("nothing frozen, cli/ reclaimed %q", got)
	}
}
