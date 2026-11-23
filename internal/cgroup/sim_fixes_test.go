// V0.sim-fixes (2): a job that hits its memory.high is raised to what it uses
// × 1.25 at each hit, up to the agents' budget, and a job stalling on its own
// limit has it lifted while it stalls.
package cgroup

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

// simFixJob is a job of a class learned at 1.5 GB (5 runs) moved into job/,
// with the agents' budget at 13 GB.
func simFixJob(t *testing.T) (*JobGroups, string, *logs, *[]string) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	write(t, filepath.Join(o.FS, unit, "agents", "memory.current"), "4G")
	proc := t.TempDir()
	fakeProc(t, proc, 10, 1, "claude")
	fakeProc(t, proc, 20, 10, "pytest")
	write(t, filepath.Join(td, CLIDir, "cgroup.procs"), "10\n20\n")
	var raised []string
	g := &JobGroups{Tree: tr, Proc: proc, Cores: 8,
		Limit: guard.JobLimit, Size: func(string, bool) resource.JobSize {
			return resource.JobSize{Class: "pytest", Peak: 1500 << 20, Demand: 2, Known: true, Runs: 5}
		},
		Raise: func(class string) { raised = append(raised, class) }}
	g.Sync("t_a", "pytest")
	return g, filepath.Join(td, jobDir), l, &raised
}

func highOf(t *testing.T, job string) uint64 {
	t.Helper()
	v := read(t, filepath.Join(job, "memory.high"))
	if v == "max" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The two-growers and background rows of V0.load-sim: a class learned at
// 1.5 GB whose job grows to 7 GB got ×1.2 on its class once, kept a third of
// its memory and crawled.
func TestSimFixesAJobThatHitsItsLimitIsRaisedToItsUseEachTime(t *testing.T) {
	g, job, l, raised := simFixJob(t)
	first := highOf(t, job)
	if first != uint64(1.25*float64(1500<<20))/uint64(os.Getpagesize())*uint64(os.Getpagesize()) {
		t.Fatalf("first limit %d", first)
	}
	high, events := first, 0
	for i := range 20 {
		events += 3
		write(t, filepath.Join(job, "memory.current"), strconv.FormatUint(high, 10))
		write(t, filepath.Join(job, "memory.events"), "low 0\nhigh "+strconv.Itoa(events)+"\nmax 0\noom 0\n")
		g.resize()
		next := highOf(t, job)
		if next == 0 {
			if float64(high)*1.25 < 13<<30 {
				t.Fatalf("hit %d: lifted at %d, under the budget", i, high)
			}
			break
		}
		if want := float64(high) * 1.25; float64(next) < want-float64(os.Getpagesize()) {
			t.Fatalf("hit %d: memory.high %d after a hit at %d, want its use × 1.25 (%.0f): %v", i, next, high, want, l.lines)
		}
		high = next
	}
	if highOf(t, job) != 0 {
		t.Fatalf("never lifted past the budget: %d", highOf(t, job))
	}
	if len(*raised) != 1 {
		t.Fatalf("the class raised %d times, want once per job", len(*raised))
	}
}

// A job whose own memory stall is critical throttles itself into the stall
// V0.job-limits meant to avoid: its limit lifts while it stalls, and comes
// back no lower than its use × 1.25.
func TestSimFixesAJobStallingOnItsOwnLimitIsLifted(t *testing.T) {
	g, job, l, _ := simFixJob(t)
	write(t, filepath.Join(job, "memory.current"), "2000M")
	write(t, filepath.Join(job, "memory.pressure"), "some avg10=60.00 avg60=40.00 avg300=10.00 total=1\nfull avg10=35.00 avg60=20.00 avg300=5.00 total=1\n")
	g.resize()
	if got := read(t, filepath.Join(job, "memory.high")); got != "max" {
		t.Fatalf("memory.high %q while the job stalls at 35 %%: %v", got, l.lines)
	}
	if !l.has(`guard: job limit thread=t_a cgroup=job class="pytest" high=max cpu=3cores why=own-stall`) {
		t.Fatalf("log %v", l.lines)
	}
	write(t, filepath.Join(job, "memory.current"), "3000M")
	g.resize()
	write(t, filepath.Join(job, "memory.pressure"), "some avg10=1.00 avg60=1.00 avg300=1.00 total=2\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=2\n")
	g.resize()
	if got := highOf(t, job); float64(got) < 1.25*3000*(1<<20)-float64(os.Getpagesize()) {
		t.Fatalf("memory.high %d after the stall, below its use × 1.25", got)
	}
}
