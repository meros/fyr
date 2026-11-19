// V0.admission-tidy: without a CPU stall reading a job's demand is what it got
// times the run's mean slowdown (decision 0029 section 0 (d)); the job's
// baseline is taken at the gate's grant, not at the next tick; the headroom
// subtracts only the page cache that is not being read back.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// V0.admission-tidy (2): one core got over 100 s on a host that ran new jobs
// 2× slow wanted about two; a factor over 3 learns at half weight.
func TestAdmissionTidyDemandWithoutAStallReadingUsesTheSlowdown(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	h := &hold{kind: slots.Test, since: t0, last: t0.Add(100 * time.Second), cpu: 100_000_000, sSum: 4, sN: 2}
	r, ok := (&Jobs{}).record("t_a", h)
	if !ok || r.Cores != 1 || r.Demand != 2 {
		t.Fatalf("%+v", r)
	}
	h = &hold{kind: slots.Test, since: t0, last: t0.Add(100 * time.Second), cpu: 100_000_000, sSum: 9, sN: 1}
	if r, _ := (&Jobs{}).record("t_a", h); r.Demand != demandMax {
		t.Fatalf("not capped at 4×: %+v", r)
	}
	h = &hold{kind: slots.Test, since: t0, last: t0.Add(100 * time.Second), cpu: 100_000_000}
	if r, _ := (&Jobs{}).record("t_a", h); r.Demand != 1 {
		t.Fatalf("without a slowdown: %+v", r)
	}
}

// V0.admission-tidy (3): the tracker made the hold at the first tick after the
// grant (up to 5 s late), so the job's first seconds went into its baseline. A
// mark at the gate is the baseline.
func TestAdmissionTidyTheBaselineIsTakenAtTheGrant(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	now := t0
	p := slots.New(2)
	p.Now = func() time.Time { return now }
	j := &Jobs{Now: func() time.Time { return now }}
	p.Acquire("t_a", slots.Test)
	j.Mark("t_a", reading{cpu: 0, hasCPU: true, mem: 1 << 30}, nil, t0)
	read := func(cpu uint64, mem uint64) map[string]reading {
		return map[string]reading{"t_a": {cpu: cpu, hasCPU: true, mem: mem}}
	}
	now = t0.Add(4 * time.Second) // 2 cores since the grant, 2 GB more
	j.tick(nil, read(8_000_000, 3<<30), p.Snapshot(), nil, nil, now)
	now = t0.Add(9 * time.Second)
	j.tick(nil, read(18_000_000, 3<<30), p.Snapshot(), nil, nil, now)
	p.Release("t_a")
	now = t0.Add(14 * time.Second)
	j.tick(nil, nil, p.Snapshot(), nil, nil, now)
	rs := j.Records()
	if len(rs) != 1 || rs[0].Cores != 2 || rs[0].Peak != 2<<30 {
		t.Fatalf("%+v", rs)
	}
	// A mark from an earlier grant is not this hold's baseline.
	j.Mark("t_a", reading{cpu: 0, hasCPU: true}, nil, t0)
	now = t0.Add(10 * time.Minute)
	p.Acquire("t_a", slots.Test)
	j.tick(nil, read(30_000_000, 1<<30), p.Snapshot(), nil, nil, now)
	now = now.Add(5 * time.Second)
	j.tick(nil, read(35_000_000, 1<<30), p.Snapshot(), nil, nil, now)
	p.Release("t_a")
	j.tick(nil, nil, p.Snapshot(), nil, nil, now.Add(5*time.Second))
	if rs := j.Records(); len(rs) != 2 || rs[1].Cores != 1 {
		t.Fatalf("a stale mark was used: %+v", rs)
	}
}

// V0.admission-tidy (9): on 19 Nov 16:02 the headroom read 48 % while the
// agents sat at memory.high with a critical full stall: the inactive_file it
// took off was being read back (refaulted) as fast as it was dropped. The
// headroom takes off only the page cache that is not read back within 10 s.
func TestAdmissionTidyRefaultedCacheIsNotHeadroom(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	agents := filepath.Join(o.FS, unit, "agents")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 1.00 1.00 1/10 1\n")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   14000000 kB\n")
	write(t, filepath.Join(agents, "memory.high"), fmt.Sprint(uint64(9216)<<20))
	write(t, filepath.Join(agents, "memory.current"), fmt.Sprint(uint64(9215)<<20))
	stat := func(refaults uint64) {
		write(t, filepath.Join(agents, "memory.stat"), fmt.Sprintf("anon %d\nfile %d\ninactive_file %d\nactive_file %d\nworkingset_refault_file %d\n", uint64(940)<<20, uint64(6879)<<20, uint64(5456)<<20, uint64(1422)<<20, refaults))
	}
	t0 := time.Date(2026, 11, 19, 16, 2, 0, 0, time.UTC)
	now := t0
	s := &Sampler{Tree: tr, Proc: proc, Cores: 4, Now: func() time.Time { return now }}
	stat(1000)
	l := s.Get()
	cache := float64(9216-(9215-5456)) / 9216
	if l.Headroom == nil || fmt.Sprintf("%.3f", *l.Headroom) != fmt.Sprintf("%.3f", cache) {
		t.Fatalf("the first read takes off the cache: %v, want %.3f", l.Headroom, cache)
	}
	// 2.4 GB/s read back: all of the inactive cache within 10 s.
	page := uint64(os.Getpagesize())
	now = t0.Add(5 * time.Second)
	stat(1000 + 5*(2400<<20)/page)
	l = s.Get()
	if l.Headroom == nil || *l.Headroom > 0.01 {
		t.Fatalf("refaulted cache counted as headroom: %v", *l.Headroom)
	}
	// A slow read-back (100 MB in 5 s) takes off the rest: 5456 − 200 MiB.
	now = t0.Add(10 * time.Second)
	stat(1000 + 5*(2400<<20)/page + (100<<20)/page)
	l = s.Get()
	want := float64(9216-(9215-(5456-200))) / 9216
	if l.Headroom == nil || fmt.Sprintf("%.2f", *l.Headroom) != fmt.Sprintf("%.2f", want) {
		t.Fatalf("headroom %v, want %.3f", *l.Headroom, want)
	}
}
