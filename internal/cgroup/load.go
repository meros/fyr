package cgroup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/lockwatch"
	"github.com/meros/fyr/internal/slots"
)

// Load is what GET /api/load answers for one host (SPEC 0.5 V0.80).
type Load struct {
	Host  string     `json:"host"`
	At    time.Time  `json:"at"`
	Cores int        `json:"cores"`
	Load  [3]float64 `json:"load"` // 1, 5 and 15 minutes
	// CPUPressure: /proc/pressure/cpu "some avg10", in percent; nil
	// without PSI.
	CPUPressure *float64 `json:"cpuPressure,omitempty"`
	Cgroups     bool     `json:"cgroups"`
	// CgroupLayout is where the agents run (V0.agents-unit): "unit" is
	// fyr.service/agents/<thread>, "scope" a scope per thread in the
	// agents' slice; "" without cgroups.
	CgroupLayout Layout       `json:"cgroupLayout,omitempty"`
	Agents       *Use         `json:"agents,omitempty"`
	Threads      []ThreadLoad `json:"threads"`
	// Heavy is the host's heavy jobs (V0.86; V0.163 in percent of the
	// host), read fresh on each Get; nil without a pool. Slots is the same,
	// under its old name, for older fronts for one release.
	Heavy *slots.Snapshot `json:"heavy,omitempty"`
	Slots *slots.Snapshot `json:"slots,omitempty"`

	// Decision 0029 S1 (#185), absent without a provider:
	// Provider reads the signals ("cgroups", "proc" or "sysctl"); Capacity is what
	// agents may use; Slowdown is s (1.0 idle), nil without a fresh probe;
	// Headroom is the share of Capacity.Memory left; MemoryPressure is PSI
	// memory full avg10 and MemoryStall its level (ok, elevated,
	// critical); AgentsPressure is agents/cpu.pressure some avg10;
	// AgentsMemoryStall is the level of agents/memory.pressure full avg10
	// (V0.load-tuning), absent without the tree.
	Provider          string    `json:"provider,omitempty"`
	Capacity          *Capacity `json:"capacity,omitempty"`
	Slowdown          *float64  `json:"slowdown,omitempty"`
	Headroom          *float64  `json:"headroom,omitempty"`
	MemoryPressure    *float64  `json:"memoryPressure,omitempty"`
	MemoryStall       Stall     `json:"memoryStall,omitempty"`
	AgentsPressure    *float64  `json:"agentsCpuPressure,omitempty"`
	AgentsMemoryStall Stall     `json:"agentsMemoryStall,omitempty"`
	// AgentsSwapFull: agents/memory.swap.current is at SwapFullShare of a
	// set memory.swap.max (V0.guard-swap).
	AgentsSwapFull bool `json:"agentsSwapFull,omitempty"`
	// V0.oomd-guard: the unit's own memory.pressure full avg10 (what oomd
	// compares) and oomd's pressure limit on the unit, both in percent;
	// absent without the tree or without an oomd rule.
	UnitMemoryStall *float64 `json:"unitMemoryStall,omitempty"`
	OomdLimit       float64  `json:"oomdLimit,omitempty"`
	// Jobs: the measured slot holds per class, the newest first.
	Jobs []JobClass `json:"jobs,omitempty"`
	// V0.164: the suggested heavy jobs, the ones the owner added, and the
	// thresholds of "undeclared".
	HeavyAdded         []heavy.Added   `json:"heavyAdded,omitempty"`
	UndeclaredSettings *UndeclaredView `json:"undeclaredSettings,omitempty"`
	// V0.rerun-classes: the classes the owner marked rerunnable.
	Rerun *RerunView `json:"rerun,omitempty"`
	// V0.176: the leftover processes past their grace, and the temporary
	// folders listed for the owner (settled threads' worktrees).
	Leftovers []Leftover `json:"leftovers,omitempty"`
	Temps     []Temp     `json:"temps,omitempty"`
	// ServicePatterns: the host's service patterns (V0.services-not-jobs).
	ServicePatterns []heavy.ServicePattern `json:"servicePatterns,omitempty"`
}

// Use is agents/ as a whole.
type Use struct {
	CPU       *float64 `json:"cpu,omitempty"` // cores, nil on the first read
	CPUMax    float64  `json:"cpuMax"`        // cores, 0 without a cap
	Memory    uint64   `json:"memory"`
	MemoryMax uint64   `json:"memoryMax"` // 0 without a limit
}

// ThreadLoad is one cgroup under agents/: a thread's id, or "self-deploy".
type ThreadLoad struct {
	ID     string   `json:"id"`
	CPU    *float64 `json:"cpu,omitempty"` // cores, nil on the first read
	Memory uint64   `json:"memory"`
	// Decision 0029 S1: the cgroup's cpu.pressure some avg10, its
	// memory.peak, and the class of the job its slot runs now.
	CPUPressure *float64 `json:"cpuPressure,omitempty"`
	Peak        uint64   `json:"peak,omitempty"`
	// Working is the working set, memory.current less inactive_file (V0.164):
	// what the undeclared-work detector reads; Memory stays the raw reading.
	Working uint64 `json:"-"`
	// Used is the process tree's CPU time so far, on a host without cgroups.
	Used time.Duration `json:"-"`
	Job  string        `json:"job,omitempty"`
	// Paused: the memory guard froze this thread's job (decision 0029 S5).
	Paused bool `json:"paused,omitempty"`
	// Services: its running services (V0.services-not-jobs).
	Services []Service `json:"services,omitempty"`
}

// MinGap is how often the host reads at most; a client asks every 5 s.
const MinGap = 2 * time.Second

// Sampler reads the load when asked, at most every MinGap; a CPU figure is
// the cgroup's usage_usec between this read and the one before.
type Sampler struct {
	Tree  *Tree
	Proc  string // default /proc
	Cores int    // default runtime.NumCPU()
	Now   func() time.Time
	Slots *slots.Pool // V0.86; optional
	// Decision 0029 S1: the slowdown probe and the job records; optional.
	Probe *Slowdown
	Jobs  *Jobs
	// Groups keeps the holders' jobs in their own cgroups (S5); optional.
	Groups *JobGroups
	// V0.164: the detector of undeclared heavy work, its settings (nil is the
	// defaults) and, on a host without the cgroup tree, the reader of the
	// process tree under each thread's CLI (CLIs says each thread's pid; each
	// Tick asks it, so a read of the load calls nothing out).
	Detect       *Detector
	DetectConfig func() UndeclaredConfig
	Settings     *UndeclaredSettings        // GET /api/load shows it
	Rerun        *RerunClasses              // V0.rerun-classes; GET /api/load shows it
	Book         *heavy.Book                // the suggestions and added patterns GET /api/load shows
	Project      func(thread string) string // a thread's project name, asked outside every lock
	names        atomic.Pointer[map[string]string]
	Trees        *TreeReader
	CLIs         func() map[string]int
	clis         atomic.Pointer[map[string]int]
	// V0.176: the leftover tracker and the temp sweep; optional.
	Leftovers *Leftovers
	Temps     *Temps
	// V0.services-not-jobs: the threads' services and the host's patterns; optional.
	Services    *Services
	ServiceBook *heavy.Services

	mu     lockwatch.Mutex
	pub    atomic.Pointer[Load] // the last read, for readers that must not wait
	at     time.Time
	last   Load
	usage  map[string]uint64 // cgroup name ("" is agents/) → usage_usec at `at`
	cached bool
	// refaults: the agents' workingset_refault_file at refaultAt (V0.admission-tidy)
	refaults  uint64
	refaultAt time.Time
}

// Get returns the load, read now or at most MinGap ago; the slots and the
// live jobs are always the newest the pool and the tracker published.
// V0.147: Get never waits on a lock another path can hold across I/O or a
// call out: while a read of the machine runs it answers the last one, and
// the pool and the tracker are read from their published views.
func (s *Sampler) Get() Load {
	l := s.base()
	if s.Slots != nil && !s.Slots.Off() {
		sn := s.Slots.View()
		l.Heavy, l.Slots = &sn, &sn
	}
	if s.Book != nil && l.Provider != "" {
		l.HeavyAdded = s.Book.Added()
	}
	if s.Settings != nil && l.Provider != "" {
		v := s.Settings.View()
		l.UndeclaredSettings = &v
	}
	if s.Rerun != nil && l.Provider != "" {
		v := s.Rerun.View()
		l.Rerun = &v
	}
	if s.Leftovers != nil {
		l.Leftovers = s.Leftovers.View()
	}
	if s.ServiceBook != nil && l.Provider != "" {
		l.ServicePatterns = s.ServiceBook.List()
	}
	if s.Services != nil {
		th := make([]ThreadLoad, len(l.Threads))
		for i, t := range l.Threads {
			t.Services = s.Services.Of(t.ID)
			th[i] = t
		}
		l.Threads = th
	}
	if s.Temps != nil {
		l.Temps = s.Temps.View()
	}
	if s.Jobs != nil {
		v := s.Jobs.View()
		l.Jobs = v.Classes
		// Decision 0029 S3, V0.163: each class's need on this host now, as
		// percentages of it (CPU and memory); Cost is the old one number.
		if l.Capacity != nil && s.Slots != nil && !s.Slots.Off() {
			host := slots.Signals{Provider: l.Provider, CPU: l.Capacity.CPU, Memory: l.Capacity.Memory}
			for i, c := range l.Jobs {
				if lr, ok := s.Jobs.Learned(c.Class); ok {
					need := slots.NeedOf(lr.Demand, lr.Peak, lr.Runs, host, s.Slots.N())
					cpu, mem := need.Shares(host)
					l.Jobs[i].CPUPct, l.Jobs[i].MemPct = slots.Pct(cpu), slots.Pct(mem)
					l.Jobs[i].Cost = float64(int(max(cpu, mem)*float64(s.Slots.N())*100+0.5)) / 100
					l.Jobs[i].Learned = lr.Runs
				}
			}
		}
		th := make([]ThreadLoad, len(l.Threads))
		for i, t := range l.Threads {
			t.Job = v.Live[t.ID]
			t.Paused = s.Groups.Frozen(t.ID)
			th[i] = t
		}
		l.Threads = th
	}
	return l
}

// base is the machine's load: read now if no read runs, else the last.
func (s *Sampler) base() Load {
	if p := s.pub.Load(); p != nil {
		if !s.mu.TryLock() {
			return *p
		}
		s.mu.Unlock()
	}
	return s.get()
}

func (s *Sampler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Tick reads the load and follows the slot holds (decision 0029 S1); the
// host calls it every ProbeEvery, whether or not a client asks.
func (s *Sampler) Tick() {
	if s.Trees != nil && s.CLIs != nil {
		c := s.CLIs()
		s.clis.Store(&c)
	}
	l := s.get()
	if s.Services != nil { // V0.services-not-jobs: before the detector, which leaves them out
		ids := make([]string, 0, len(l.Threads))
		for _, t := range l.Threads {
			ids = append(ids, t.ID)
		}
		s.Services.Tick(s.Tree, ids)
	}
	s.detect(l)
	s.Tree.SplitThreads()                      // V0.job-limits: each thread's CLI in its cli/ leaf
	s.Tree.ShadowThreadBudget(s.threadHolds()) // decision 0032 slice 3, V0.share-shadow: measured, not enforced
	if s.Jobs == nil || s.Slots == nil || s.Slots.Off() {
		return
	}
	// Decision 0029 S3: the project names ClassOf reads under the pool's
	// lock, asked here outside every lock.
	snap := s.Slots.Snapshot()
	// Decision 0029 S5: the holders' jobs in their own cgroups.
	if s.Groups != nil {
		held := make([]string, 0, len(snap.Held))
		for _, h := range snap.Held {
			if !h.Priority {
				held = append(held, h.Owner)
			}
		}
		s.Groups.Tick(held)
	}
	owners := make([]string, 0, len(snap.Held)+len(snap.Waiting))
	for _, h := range append(snap.Held, snap.Waiting...) {
		owners = append(owners, h.Owner)
	}
	s.Jobs.RefreshProjects(owners)
	rates := map[string]*float64{}
	var trees map[string]reading
	at := s.now()
	if s.Tree == nil {
		trees, at = map[string]reading{}, l.At // the readings are of that read
	}
	for _, t := range l.Threads {
		rates[t.ID] = t.CPU
		if trees != nil {
			trees[t.ID] = reading{cpu: uint64(t.Used.Microseconds()), hasCPU: true, mem: t.Working}
		}
	}
	s.Jobs.tick(s.Tree, trees, s.Slots.Snapshot(), rates, l.Slowdown, at)
}

// readTrees fills the threads and the agents' sum from the process trees.
func (s *Sampler) readTrees(l *Load, now time.Time) {
	clis := s.clis.Load()
	if s.Trees == nil || clis == nil {
		return
	}
	a := &Use{}
	sum, some := 0.0, false
	for _, rd := range s.Trees.Read(now, *clis) {
		t := ThreadLoad{ID: rd.Thread, Memory: rd.Work, Working: rd.Work, Used: rd.Used}
		if rd.CPU != nil {
			c := round2(*rd.CPU)
			t.CPU = &c
			sum, some = sum+c, true
		}
		a.Memory += rd.Work
		l.Threads = append(l.Threads, t)
	}
	if some {
		c := round2(sum)
		a.CPU = &c
	}
	l.Agents = a
}

// TreeCPUTime is thread's process tree's CPU time at the last read; false
// without one (V0.process-tree-hosts: the quiet wait of a host without cgroups).
func (s *Sampler) TreeCPUTime(thread string) (time.Duration, bool) {
	p := s.pub.Load()
	if p == nil {
		return 0, false
	}
	for _, t := range p.Threads {
		if t.ID == thread {
			return t.Used, true
		}
	}
	return 0, false
}

// threadHolds are the threads holding a slot now.
func (s *Sampler) threadHolds() map[string]bool {
	if s.Slots == nil || s.Slots.Off() {
		return nil
	}
	out := map[string]bool{}
	for _, h := range s.Slots.Snapshot().Held {
		out[h.Owner] = true
	}
	return out
}

// Signals are the measures admission reads (decision 0029 S2): the last
// published read (V0.147: admission runs under the pool's lock, so it does
// no I/O; the sampler's tick reads every ProbeEvery). The first call reads.
func (s *Sampler) Signals() slots.Signals {
	var l Load
	if p := s.pub.Load(); p != nil {
		l = *p
	} else {
		l = s.get()
	}
	sig := slots.Signals{Provider: l.Provider, Slowdown: l.Slowdown, Headroom: l.Headroom, Stall: slots.Stall(l.MemoryStall), AgentsStall: slots.Stall(l.AgentsMemoryStall), SwapFull: l.AgentsSwapFull,
		UnitStall: l.UnitMemoryStall, OomdLimit: l.OomdLimit}
	if l.Capacity != nil {
		sig.CPU, sig.Memory = l.Capacity.CPU, l.Capacity.Memory
	}
	if l.Agents != nil {
		sig.AgentsMem = l.Agents.Memory
	}
	return sig
}

// ThreadMemory is thread's memory at the last read (its cgroup, or its
// process tree); 0 when unknown. V0.guard-wake: the guard keeps what a job
// held when it froze it.
func (s *Sampler) ThreadMemory(thread string) uint64 {
	p := s.pub.Load()
	if p == nil {
		return 0
	}
	for _, t := range p.Threads {
		if t.ID == thread {
			return t.Memory
		}
	}
	return 0
}

// Baseline takes thread's job baseline at the gate's grant (V0.admission-tidy):
// its cgroup read now and its memory.peak reset now, or on a host without the
// tree its last reading. The tracker's next tick makes the hold from it.
func (s *Sampler) Baseline(thread string) {
	if s.Jobs == nil {
		return
	}
	if s.Tree != nil {
		dir := s.Tree.threadDir(unsafe.ReplaceAllString(thread, "_"))
		s.Jobs.Mark(thread, readCgroup(dir), openPeak(dir), s.now())
		return
	}
	p := s.pub.Load()
	if p == nil {
		return
	}
	for _, t := range p.Threads {
		if t.ID == thread {
			s.Jobs.Mark(thread, reading{cpu: uint64(t.Used.Microseconds()), hasCPU: true, mem: t.Working}, nil, p.At)
		}
	}
}

// Run ticks until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	t := time.NewTicker(ProbeEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick()
		}
	}
}

// get reads the machine and the cgroups at most every MinGap.
func (s *Sampler) get() Load {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.cached && now.Sub(s.at) < MinGap {
		return s.last
	}
	proc := s.Proc
	if proc == "" {
		proc = "/proc"
	}
	l := Load{At: now.UTC(), Cores: s.Cores, Threads: []ThreadLoad{}}
	if l.Cores == 0 {
		l.Cores = runtime.NumCPU()
	}
	if b, err := os.ReadFile(filepath.Join(proc, "loadavg")); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			l.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	} else if la, ok := hostLoadavg(); ok {
		l.Load = la
	}
	if b, err := os.ReadFile(filepath.Join(proc, "pressure", "cpu")); err == nil {
		for _, w := range strings.Fields(strings.SplitN(string(b), "\n", 2)[0]) {
			if v, ok := strings.CutPrefix(w, "avg10="); ok {
				if p, err := strconv.ParseFloat(v, 64); err == nil {
					l.CPUPressure = &p
				}
			}
		}
	}
	usage := map[string]uint64{}
	gap := now.Sub(s.at)
	cpu := func(name string, u uint64) *float64 {
		usage[name] = u
		prev, ok := s.usage[name]
		if !ok || !s.cached || gap <= 0 || gap > time.Minute || u < prev {
			return nil
		}
		c := float64(u-prev) / float64(gap.Microseconds())
		c = float64(int(c*100+0.5)) / 100
		return &c
	}
	if s.Tree != nil {
		l.Cgroups = true
		l.CgroupLayout = s.Tree.layout
		agents := s.Tree.agentsDir()
		a := &Use{Memory: readBytes(filepath.Join(agents, "memory.current")), MemoryMax: readBytes(filepath.Join(agents, "memory.max"))}
		if u, ok := readKey(filepath.Join(agents, "cpu.stat"), "usage_usec"); ok {
			a.CPU = cpu("", u)
		}
		if b, err := os.ReadFile(filepath.Join(agents, "cpu.max")); err == nil {
			f := strings.Fields(string(b))
			if len(f) == 2 && f[0] != "max" {
				q, _ := strconv.ParseFloat(f[0], 64)
				p, _ := strconv.ParseFloat(f[1], 64)
				if p > 0 {
					a.CPUMax = q / p
				}
			}
		}
		l.Agents = a
		seen := map[string]int{}
		for _, c := range s.Tree.threadCgroups() {
			d := c.Dir
			t := ThreadLoad{ID: c.Name, Memory: readBytes(filepath.Join(d, "memory.current")), Peak: readBytes(filepath.Join(d, "memory.peak"))}
			t.Working = t.Memory
			if in, ok := readKey(filepath.Join(d, "memory.stat"), "inactive_file"); ok && in <= t.Memory {
				t.Working -= in
			}
			if u, ok := readKey(filepath.Join(d, "cpu.stat"), "usage_usec"); ok {
				t.CPU = cpu(filepath.Base(d), u)
			}
			if p, ok := readPSI(filepath.Join(d, "cpu.pressure")); ok {
				v := p.some
				t.CPUPressure = &v
			}
			// A thread with two cgroups (a handover, deploy commands, one
			// from before the switch to scopes) is one row: their sum.
			if i, ok := seen[c.Name]; ok {
				l.Threads[i] = addThreadLoad(l.Threads[i], t)
				continue
			}
			seen[c.Name] = len(l.Threads)
			l.Threads = append(l.Threads, t)
		}
		sort.Slice(l.Threads, func(i, j int) bool { return l.Threads[i].ID < l.Threads[j].ID })
	} else {
		s.readTrees(&l, now)
	}
	var agentsMem uint64
	if l.Agents != nil {
		agentsMem = l.Agents.Memory
	}
	s.signals(&l, proc, agentsMem)
	s.at, s.usage, s.last, s.cached = now, usage, l, true
	pub := l
	s.pub.Store(&pub)
	return l
}
