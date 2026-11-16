package cgroup

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

	mu     sync.Mutex
	at     time.Time
	last   Load
	usage  map[string]uint64 // cgroup name ("" is agents/) → usage_usec at `at`
	cached bool
}

// Get returns the load, read now or at most MinGap ago; the slots are
// always read now.
func (s *Sampler) Get() Load {
	l := s.get()
	if s.Slots != nil && !s.Slots.Off() {
		sn := s.Slots.Snapshot()
		l.Slots = &sn
	}
	return l
}

// get reads the machine and the cgroups at most every MinGap.
func (s *Sampler) get() Load {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
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
			t := ThreadLoad{ID: e.Name(), Memory: readBytes(filepath.Join(d, "memory.current"))}
			if u, ok := readKey(filepath.Join(d, "cpu.stat"), "usage_usec"); ok {
				t.CPU = cpu(e.Name(), u)
			}
			l.Threads = append(l.Threads, t)
		}
		sort.Slice(l.Threads, func(i, j int) bool { return l.Threads[i].ID < l.Threads[j].ID })
	}
	s.at, s.usage, s.last, s.cached = now, usage, l, true
	return l
}
