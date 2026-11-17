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
	CPUPressure *float64     `json:"cpuPressure,omitempty"`
	Cgroups     bool         `json:"cgroups"`
	Agents      *Use         `json:"agents,omitempty"`
	Threads     []ThreadLoad `json:"threads"`
	// Slots: the host's slots for heavy jobs (V0.86), read fresh on each
	// Get; nil without a pool.
	Slots *slots.Snapshot `json:"slots,omitempty"`

	// Decision 0029 S1 (#185), absent without a provider (no /proc):
	// Provider reads the signals ("cgroups" or "proc"); Capacity is what
	// agents may use; Slowdown is s (1.0 idle), nil without a fresh probe;
	// Headroom is the share of Capacity.Memory left; MemoryPressure is PSI
	// memory full avg10 and MemoryStall its level (ok, elevated,
	// critical); AgentsPressure is agents/cpu.pressure some avg10.
	Provider       string    `json:"provider,omitempty"`
	Capacity       *Capacity `json:"capacity,omitempty"`
	Slowdown       *float64  `json:"slowdown,omitempty"`
	Headroom       *float64  `json:"headroom,omitempty"`
	MemoryPressure *float64  `json:"memoryPressure,omitempty"`
	MemoryStall    Stall     `json:"memoryStall,omitempty"`
	AgentsPressure *float64  `json:"agentsCpuPressure,omitempty"`
	// Jobs: the measured slot holds per class, the newest first.
	Jobs []JobClass `json:"jobs,omitempty"`
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
	Job         string   `json:"job,omitempty"`
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

	mu     lockwatch.Mutex
	pub    atomic.Pointer[Load] // the last read, for readers that must not wait
	at     time.Time
	last   Load
	usage  map[string]uint64 // cgroup name ("" is agents/) → usage_usec at `at`
	cached bool
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
		l.Slots = &sn
	}
	if s.Jobs != nil {
		v := s.Jobs.View()
		l.Jobs = v.Classes
		// Decision 0029 S3: each class's cost on this host now.
		if l.Capacity != nil && s.Slots != nil && !s.Slots.Off() {
			for i, c := range l.Jobs {
				if lr, ok := s.Jobs.Learned(c.Class); ok {
					l.Jobs[i].Cost = slots.CostOf(lr.Demand, lr.Peak, lr.Runs, l.Capacity.CPU, l.Capacity.Memory, s.Slots.Budget())
					l.Jobs[i].Learned = lr.Runs
				}
			}
		}
		th := make([]ThreadLoad, len(l.Threads))
		for i, t := range l.Threads {
			t.Job = v.Live[t.ID]
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
	l := s.get()
	if s.Jobs == nil || s.Slots == nil || s.Slots.Off() {
		return
	}
	// Decision 0029 S3: the project names ClassOf reads under the pool's
	// lock, asked here outside every lock.
	snap := s.Slots.Snapshot()
	owners := make([]string, 0, len(snap.Held)+len(snap.Waiting))
	for _, h := range append(snap.Held, snap.Waiting...) {
		owners = append(owners, h.Owner)
	}
	s.Jobs.RefreshProjects(owners)
	rates := map[string]*float64{}
	for _, t := range l.Threads {
		rates[t.ID] = t.CPU
	}
	s.Jobs.tick(s.Tree, s.Slots.Snapshot(), rates, l.Slowdown, s.now())
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
	sig := slots.Signals{Provider: l.Provider, Slowdown: l.Slowdown, Headroom: l.Headroom, Stall: slots.Stall(l.MemoryStall)}
	if l.Capacity != nil {
		sig.CPU, sig.Memory = l.Capacity.CPU, l.Capacity.Memory
	}
	return sig
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
		agents := filepath.Join(s.Tree.root, "agents")
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
		es, _ := os.ReadDir(agents)
		for _, e := range es {
			if !e.IsDir() || e.Name() == "probe" {
				continue
			}
			d := filepath.Join(agents, e.Name())
			t := ThreadLoad{ID: e.Name(), Memory: readBytes(filepath.Join(d, "memory.current")), Peak: readBytes(filepath.Join(d, "memory.peak"))}
			if u, ok := readKey(filepath.Join(d, "cpu.stat"), "usage_usec"); ok {
				t.CPU = cpu(e.Name(), u)
			}
			if p, ok := readPSI(filepath.Join(d, "cpu.pressure")); ok {
				v := p.some
				t.CPUPressure = &v
			}
			l.Threads = append(l.Threads, t)
		}
		sort.Slice(l.Threads, func(i, j int) bool { return l.Threads[i].ID < l.Threads[j].ID })
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
