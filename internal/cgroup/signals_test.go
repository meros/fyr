// Decision 0029 slice S1 (#185): the host's capacity and the portable signals (slowdown, memory headroom as a share,
// the memory stall level), read by the provider the host supports, and a record per slot hold. The cgroupfs and /proc
// here are plain directories.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

func memPSI(full float64) string {
	return fmt.Sprintf("some avg10=%.2f avg60=0.00 avg300=0.00 total=1\nfull avg10=%.2f avg60=0.00 avg300=0.00 total=1\n", full, full)
}

func TestS1CapacityIsReadFromTheHost(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	agents := filepath.Join(o.FS, unit, "agents")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:    8000000 kB\n")
	// cpu.max 4 cores, but the cpuset allows 2: the smaller wins; memory.high is the memory.
	write(t, filepath.Join(agents, "cpuset.cpus.effective"), "0-1\n")
	c := capacity(tr, proc, 8, 0.8)
	if c.CPU != 2 || c.Memory != 13<<30 {
		t.Fatalf("with cgroups %+v", c)
	}
	// Without a tree: the CPU count and 80 % of the RAM.
	c = capacity(nil, proc, 8, 0.8)
	if c.CPU != 8 || c.Memory != uint64(0.8*32000000*1024) {
		t.Fatalf("without cgroups %+v", c)
	}
}

func TestS1SignalsByProvider(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	agents := filepath.Join(o.FS, unit, "agents")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 1.00 1.00 1/10 1\n")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	write(t, filepath.Join(proc, "pressure", "memory"), memPSI(6))
	write(t, filepath.Join(agents, "memory.current"), fmt.Sprint(uint64(10)<<30))
	write(t, filepath.Join(agents, "cpu.pressure"), "some avg10=55.00 avg60=0.00 avg300=0.00 total=1\nfull avg10=1.00 avg60=0.00 avg300=0.00 total=1\n")
	s := &Sampler{Tree: tr, Proc: proc, Cores: 4}
	l := s.Get()
	if l.Provider != "cgroups" || l.Capacity == nil || l.Capacity.Memory != 13<<30 {
		t.Fatalf("provider and capacity %+v %+v", l.Provider, l.Capacity)
	}
	// 13 GB may be used, 10 GB are: 3/13 left (MemAvailable is larger).
	if l.Headroom == nil || fmt.Sprintf("%.3f", *l.Headroom) != fmt.Sprintf("%.3f", 3.0/13) {
		t.Fatalf("headroom %v", l.Headroom)
	}
	if l.MemoryStall != "elevated" || l.MemoryPressure == nil || *l.MemoryPressure != 6 {
		t.Fatalf("stall %q %v", l.MemoryStall, l.MemoryPressure)
	}
	if l.AgentsPressure == nil || *l.AgentsPressure != 55 {
		t.Fatalf("agents pressure %v", l.AgentsPressure)
	}

	// Linux without cgroups: /proc only; the headroom is MemAvailable over 80 % of the RAM.
	write(t, filepath.Join(proc, "pressure", "memory"), memPSI(12))
	l = (&Sampler{Proc: proc, Cores: 4}).Get()
	if l.Provider != "proc" || l.MemoryStall != "critical" || l.Headroom == nil || fmt.Sprintf("%.3f", *l.Headroom) != fmt.Sprintf("%.3f", 20.0/(0.8*32)) {
		t.Fatalf("proc provider %+v %q %v", l.Provider, l.MemoryStall, l.Headroom)
	}
	// No /proc (macOS, decision 0029 deferred): no signals at all, as before.
	l = (&Sampler{Proc: t.TempDir(), Cores: 4}).Get()
	if l.Provider != "" || l.Capacity != nil || l.Headroom != nil || l.MemoryStall != "" || l.Slowdown != nil {
		t.Fatalf("no provider %+v", l)
	}
}

func TestS1SlowdownIsTheMedianOfTheLastThreeAndFollowsTheHostsFit(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	var sd Slowdown
	for _, v := range []float64{1.0, 9.0, 1.2, 1.4} {
		sd.Add(now, v, nil)
	}
	if v := sd.Value(now, nil); v == nil || *v != 1.4 { // median of 9.0, 1.2, 1.4
		t.Fatalf("median %v", v)
	}
	if v := sd.Value(now.Add(time.Minute), nil); v != nil {
		t.Fatalf("a stale probe still counts: %v", *v)
	}
	// This host learns that PSI 85 % means about 4×: a calm probe under that pressure reads 4.
	hi := 85.0
	for i := 0; i < 20; i++ {
		sd.Add(now, 4, &hi)
	}
	sd.Add(now, 1.1, nil)
	sd.Add(now, 1.1, nil)
	sd.Add(now, 1.1, nil)
	if v := sd.Value(now, &hi); v == nil || *v != 4 {
		t.Fatalf("fit %v", v)
	}
	lo := 15.0
	if v := sd.Value(now, &lo); v == nil || *v != 1.1 { // no fit for 10–20 %: the probe alone
		t.Fatalf("no fit %v", v)
	}
}

type kv map[string]string

func (m kv) GetKV(k string) (string, error) { return m[k], nil }
func (m kv) SetKV(k, v string) error        { m[k] = v; return nil }

func TestS1ASlotHoldLeavesAJobRecord(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := tr.Thread("t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 1.00 1.00 1/10 1\n")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	cg := func(usec, stall int, mem uint64) {
		write(t, filepath.Join(td, "cpu.stat"), fmt.Sprintf("usage_usec %d\n", usec))
		write(t, filepath.Join(td, "cpu.pressure"), fmt.Sprintf("some avg10=0.00 avg60=0.00 avg300=0.00 total=%d\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n", stall))
		write(t, filepath.Join(td, "memory.current"), fmt.Sprint(mem))
	}
	cg(1_000_000, 0, 1<<30)
	write(t, filepath.Join(td, "memory.peak"), fmt.Sprint(1<<30))
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	pool := slots.New(2)
	pool.Now = func() time.Time { return now }
	store := kv{}
	j := &Jobs{KV: store, Project: func(id string) string { return map[string]string{"t_a": "fyr"}[id] }, Now: func() time.Time { return now }}
	s := &Sampler{Tree: tr, Proc: proc, Cores: 4, Slots: pool, Jobs: j, Now: func() time.Time { return now }}

	pool.Acquire("t_a", slots.Test)
	s.Tick()                  // the grant: the baseline
	j.Saw("t_a", "make test") // the gate hook's class of "cd /x && make test 2>&1 | tail -20"
	// 60 s: 120 core-seconds (2 cores), stalled half the time, the peak 3 GB.
	now = now.Add(30 * time.Second)
	cg(61_000_000, 15_000_000, 2<<30)
	s.Tick()
	if l := s.Get(); len(l.Threads) != 1 || l.Threads[0].Job != "make test" {
		t.Fatalf("the live job %+v", l.Threads)
	}
	now = now.Add(30 * time.Second)
	cg(121_000_000, 30_000_000, 2<<30)
	write(t, filepath.Join(td, "memory.peak"), fmt.Sprint(3<<30))
	s.Tick()
	pool.Release("t_a")
	now = now.Add(5 * time.Second)
	s.Tick() // the end

	recs := j.Records()
	if len(recs) != 1 {
		t.Fatalf("records %+v", recs)
	}
	r := recs[0]
	if r.Class != "fyr: make test" || r.Thread != "t_a" || r.Kind != slots.Test || r.Wall != 60 {
		t.Fatalf("record %+v", r)
	}
	if r.CoreSeconds != 120 || r.Cores != 2 || r.Demand != 4 || r.Peak != 2<<30 {
		t.Fatalf("measures %+v", r)
	}
	if store["slot-jobs"] == "" {
		t.Fatal("not kept in the store")
	}
	// A new tracker reads the kept records; the load lists the class.
	j2 := &Jobs{KV: store}
	l := (&Sampler{Proc: proc, Cores: 4, Jobs: j2}).Get()
	if len(l.Jobs) != 1 || l.Jobs[0].Class != "fyr: make test" || l.Jobs[0].Runs != 1 || l.Jobs[0].Demand != 4 {
		t.Fatalf("classes %+v", l.Jobs)
	}
	_ = os.Remove
}

func TestS1AHoldWithoutAMatchedCommandIsNamedByItsKind(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := tr.Thread("t_b")
	write(t, filepath.Join(td, "cpu.stat"), "usage_usec 0\n")
	write(t, filepath.Join(td, "memory.current"), "0\n")
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	pool := slots.New(2)
	pool.Now = func() time.Time { return now }
	j := &Jobs{KV: kv{}, Now: func() time.Time { return now }}
	s := &Sampler{Tree: tr, Proc: t.TempDir(), Cores: 4, Slots: pool, Jobs: j, Now: func() time.Time { return now }}
	pool.Acquire("t_b", slots.E2E)
	s.Tick()
	now = now.Add(10 * time.Second)
	s.Tick()
	pool.Release("t_b")
	now = now.Add(5 * time.Second)
	s.Tick()
	if r := j.Records(); len(r) != 1 || r[0].Class != "e2e" {
		t.Fatalf("%+v", r)
	}
}

// #174: a hold is active at its grant, at each heavy command and at each reading above its baseline; an idle hold
// keeps its last active time.
func TestH174HoldActivity(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := tr.Thread("t_a")
	usec := 0
	stat := func() { write(t, filepath.Join(td, "cpu.stat"), fmt.Sprintf("usage_usec %d\n", usec)) }
	stat()
	write(t, filepath.Join(td, "memory.current"), "0\n")
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	now := t0
	pool := slots.New(2)
	pool.Now = func() time.Time { return now }
	j := &Jobs{KV: kv{}, Now: func() time.Time { return now }}
	s := &Sampler{Tree: tr, Proc: t.TempDir(), Cores: 4, Slots: pool, Jobs: j, Now: func() time.Time { return now }}
	if _, ok := j.Active("t_a"); ok {
		t.Fatal("active without a hold")
	}
	pool.Acquire("t_a", slots.Test)
	s.Tick()
	if at, ok := j.Active("t_a"); !ok || !at.Equal(t0) {
		t.Fatalf("at the grant %v %v", at, ok)
	}
	now = t0.Add(10 * time.Second) // 1 core for 10 s: busy
	usec += 10_000_000
	stat()
	s.Tick()
	now = t0.Add(5 * time.Minute) // idle since
	s.Tick()
	if at, _ := j.Active("t_a"); !at.Equal(t0.Add(10 * time.Second)) {
		t.Fatalf("after a busy then an idle reading %v", at)
	}
	j.Saw("t_a", "make test")
	if at, _ := j.Active("t_a"); !at.Equal(now) {
		t.Fatalf("after a heavy command %v", at)
	}
}

// Hotfix (V0.146 (2)): page cache is not used memory. agents/memory.current fills to memory.high with cache the
// kernel drops first (inactive_file); the headroom counts the working set (current − inactive_file), as Kubernetes
// does. Live on 17 Nov: 9215 of 9216 MiB "used", 5456 MiB of it inactive_file, and every heavy job held.
func TestHeadroomCountsTheWorkingSetNotTheCache(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	agents := filepath.Join(o.FS, unit, "agents")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 1.00 1.00 1/10 1\n")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   14000000 kB\n")
	write(t, filepath.Join(agents, "memory.high"), fmt.Sprint(uint64(9216)<<20))
	write(t, filepath.Join(agents, "memory.current"), fmt.Sprint(uint64(9215)<<20))
	write(t, filepath.Join(agents, "memory.stat"), fmt.Sprintf("anon %d\nfile %d\ninactive_file %d\nactive_file %d\n", uint64(940)<<20, uint64(6879)<<20, uint64(5456)<<20, uint64(1422)<<20))
	l := (&Sampler{Tree: tr, Proc: proc, Cores: 4}).Get()
	want := float64(9216-(9215-5456)) / 9216
	if l.Headroom == nil || fmt.Sprintf("%.3f", *l.Headroom) != fmt.Sprintf("%.3f", want) {
		t.Fatalf("headroom %v, want %.3f", l.Headroom, want)
	}
}

// Decision 0029 S3 (#187): each finished record teaches its class: an EMA of demand (half weight when starved), a
// decaying max of the peak, the last ten wall times; kept in the store; Reset forgets a class. A waiting thread's
// class is the heavy command it named (slot_acquire's command or the refused one).
func TestS3JobsLearnPerClass(t *testing.T) {
	store := kv{}
	j := &Jobs{KV: store, Project: func(string) string { return "fyr" }}
	rec := func(class string, demand, cores float64, peak uint64, wall float64) {
		j.mu.Lock()
		j.load()
		j.learn(JobRecord{Class: class, Demand: demand, Cores: cores, Peak: peak, Wall: wall})
		raw := j.tableRaw()
		j.mu.Unlock()
		j.storeTable(raw) // as finish does: stored after the unlock
	}
	rec("fyr: make test", 2, 2, 4<<30, 100)
	rec("fyr: make test", 3, 3, 2<<30, 120)
	l, ok := j.Learned("fyr: make test")
	four := float64(4 << 30)
	if !ok || l.Runs != 2 || l.Demand != 2.3 || l.Peak != uint64(0.9*four) {
		t.Fatalf("after two runs %+v", l)
	}
	rec("fyr: make test", 4.3, 1, 1<<30, 300) // starved (4.3×): half weight, 0.85·2.3 + 0.15·4.3
	if l, _ := j.Learned("fyr: make test"); l.Demand != 2.6 || l.WallP90() != 300 {
		t.Fatalf("a starved run %+v p90 %v", l, l.WallP90())
	}
	j2 := &Jobs{KV: store}
	if l, ok := j2.Learned("fyr: make test"); !ok || l.Runs != 3 {
		t.Fatalf("not kept %+v", l)
	}
	if !j2.Reset("fyr: make test") {
		t.Fatal("reset")
	}
	if _, ok := (&Jobs{KV: store}).Learned("fyr: make test"); ok {
		t.Fatal("reset not kept")
	}
	// A waiting thread named its job: its class, else its kind.
	j.RefreshProjects([]string{"t_w", "t_x"})
	j.Saw("t_w", "yarn screenshot:diff")
	if c := j.ClassOf("t_w", "e2e"); c != "fyr: yarn screenshot:diff" {
		t.Fatalf("class of a waiter %q", c)
	}
	if c := j.ClassOf("t_x", "test"); c != "fyr: test" {
		t.Fatalf("class by kind %q", c)
	}
}

// Decision 0029 S3 with #206's lock order: ClassOf runs under the pool's lock (Admit, Backfill, Cost), so it never
// calls Project (the engine); it reads the names Tick cached outside every lock.
func TestS3ClassOfReadsCachedProjectNames(t *testing.T) {
	calls := 0
	j := &Jobs{KV: kv{}, Project: func(id string) string { calls++; return "fyr" }}
	if c := j.ClassOf("t_a", "test"); c != "test" || calls != 0 {
		t.Fatalf("before a refresh: %q, %d calls", c, calls)
	}
	j.RefreshProjects([]string{"t_a"})
	j.Saw("t_a", "make test")
	if c := j.ClassOf("t_a", "test"); c != "fyr: make test" || calls != 1 {
		t.Fatalf("after: %q, %d calls", c, calls)
	}
}
