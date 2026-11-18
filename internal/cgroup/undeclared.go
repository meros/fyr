package cgroup

// V0.164: heavy work that nobody asked for, seen from the thread's own
// cgroup. Every tick the sampler reads each thread's cores and working set
// (admitted or not); the detector keeps each thread's idle level, the median
// of its quiet readings of the last 10 min, and flags a thread that is not
// admitted and stays over a threshold above that level for a window. One flag
// is one episode, from the flag to the moment the use is back down. The
// detector decides nothing about the work: the
// pool counts the use, and the memory guard may freeze it.

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// UndeclaredConfig is the host's settings for it (shares of what the host
// gives its agents, so no number is absolute: decision 0029 section 0 (a)).
type UndeclaredConfig struct {
	// Off turns all of V0.164 off on this host.
	Off bool
	// CPU and Memory are the thresholds as shares of the agents' capacity.
	CPU, Memory float64
	// After is the window W: 30–120 s.
	After time.Duration
}

// DefaultUndeclared: a quarter of the agents' cores, 8 % of their memory, 45 s.
var DefaultUndeclared = UndeclaredConfig{CPU: 0.25, Memory: 0.08, After: 45 * time.Second}

// Norm fills what is unset with the defaults and bounds the window.
func (c UndeclaredConfig) Norm() UndeclaredConfig {
	if c.CPU <= 0 || c.CPU > 1 {
		c.CPU = DefaultUndeclared.CPU
	}
	if c.Memory <= 0 || c.Memory > 1 {
		c.Memory = DefaultUndeclared.Memory
	}
	if c.After <= 0 {
		c.After = DefaultUndeclared.After
	}
	c.After = min(max(c.After, 30*time.Second), 120*time.Second)
	return c
}

// idleSpan is how far back the idle level looks.
const idleSpan = 10 * time.Minute

// Reading is one thread's use at a tick: the cores since the last reading
// (nil on the first) and the working set in bytes (memory.current less
// inactive_file; on a host without cgroups, the process tree's RSS). Used is
// the process tree's CPU time so far (TreeReader only).
type Reading struct {
	Thread string
	CPU    *float64
	Work   uint64
	Used   time.Duration
}

// DetectInput is what one tick gives the detector.
type DetectInput struct {
	// Capacity is what the host gives its agents; nil (no provider) turns
	// detection off.
	Capacity *Capacity
	Config   UndeclaredConfig
	Reads    []Reading
	// Admitted: the threads that hold an admission now; Queued: those that
	// asked and wait.
	Admitted, Queued map[string]bool
}

// Level is a thread's idle level.
type Level struct {
	CPU float64
	Mem uint64
}

// Flag is a flagged thread: the use above its idle level.
type Flag struct {
	Thread    string
	Since     time.Time
	FirstOver time.Time // the window's start at the flag: the roots started after it less 15 s
	CPU       float64   // mean cores above the idle level over the window
	Mem       uint64    // working set now less the idle level
	// OverCPU and OverMem: the measure is at least a tenth of its threshold.
	OverCPU, OverMem bool
	// MemOver: the memory itself is over its threshold (the freeze picks these).
	MemOver bool
	Queued  bool
	Command string
	Window  time.Duration
	Idle    Level
}

// Episode is a flag's record, kept when it ends.
type Episode struct {
	Thread  string    `json:"thread"`
	Project string    `json:"project,omitempty"`
	Command string    `json:"command,omitempty"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	// PeakMem is the largest memory above the idle level; MeanCores the mean
	// cores above it over the episode.
	PeakMem   uint64  `json:"peakMem"`
	MeanCores float64 `json:"meanCores"`
	Paused    bool    `json:"paused"`
}

type hread struct {
	at     time.Time
	cpu    float64
	hasCPU bool
	work   uint64
}

type qread struct {
	at  time.Time
	cpu float64
	mem uint64
}

type episode struct {
	since, firstOver, calmSince time.Time
	paused                      bool
	peak                        uint64
	cpuSum                      float64
	cpuN                        int
	command                     string
	queued                      bool
	window                      time.Duration
	cpu                         float64
	mem                         uint64
	overCPU, overMem, memOver   bool
}

type dthread struct {
	seeded  bool
	idle    Level
	quiet   []qread
	hist    []hread
	ep      *episode
	touched bool
}

// Detector keeps the idle levels and the flags. Observe is called by one
// goroutine (the sampler's tick); the readers by others.
type Detector struct {
	// OnEnd hears an episode that ended, outside the detector's lock.
	OnEnd func(Episode)

	mu      sync.Mutex
	threads map[string]*dthread
	pub     atomic.Pointer[[]Flag]
	levels  atomic.Pointer[map[string]Level]
}

// Flags are the flagged threads, the oldest episode first; a published copy,
// so a reader never waits.
func (d *Detector) Flags() []Flag {
	if p := d.pub.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// Idle is a thread's idle level (zero for an unknown thread).
func (d *Detector) Idle(thread string) Level {
	if p := d.levels.Load(); p != nil {
		return (*p)[thread]
	}
	return Level{}
}

// SetCommand records the command behind thread's episode (the newest wins).
func (d *Detector) SetCommand(thread, command string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if th := d.threads[thread]; th != nil && th.ep != nil && command != "" && th.ep.command != command {
		th.ep.command = command
		d.publishLocked()
	}
}

// MarkPaused notes that the episode was frozen (for its record).
func (d *Detector) MarkPaused(thread string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if th := d.threads[thread]; th != nil && th.ep != nil {
		th.ep.paused = true
	}
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// Observe takes one tick's readings.
func (d *Detector) Observe(now time.Time, in DetectInput) {
	cfg := in.Config.Norm()
	var ended []Episode
	d.mu.Lock()
	if d.threads == nil {
		d.threads = map[string]*dthread{}
	}
	if in.Config.Off || in.Capacity == nil || in.Capacity.CPU <= 0 || in.Capacity.Memory == 0 {
		for name, th := range d.threads {
			if th.ep != nil {
				ended = append(ended, th.ep.record(name, now, th.idle))
			}
		}
		d.threads = map[string]*dthread{}
		d.publishLocked()
		d.mu.Unlock()
		d.emit(ended)
		return
	}
	cpuThr := cfg.CPU * in.Capacity.CPU
	memThr := cfg.Memory * float64(in.Capacity.Memory)
	for _, th := range d.threads {
		th.touched = false
	}
	for _, r := range in.Reads {
		th := d.threads[r.Thread]
		if th == nil {
			th = &dthread{}
			d.threads[r.Thread] = th
		}
		th.touched = true
		work := float64(r.Work)
		if !th.seeded {
			th.seeded = true
			th.idle = Level{Mem: r.Work}
			th.quiet = []qread{{now, 0, r.Work}}
			th.hist = []hread{{at: now, work: r.Work}}
			continue
		}
		if in.Admitted[r.Thread] {
			// An admitted job is not undeclared work and never the idle level.
			if th.ep != nil {
				ended = append(ended, th.ep.record(r.Thread, now, th.idle))
				th.ep = nil
			}
			th.hist = nil
			continue
		}
		h := hread{at: now, work: r.Work}
		if r.CPU != nil {
			h.cpu, h.hasCPU = *r.CPU, true
		}
		th.hist = append(th.hist, h)
		// Keep what the window and the idle level need.
		cut := now.Add(-(cfg.After + 2*ProbeEvery))
		for len(th.hist) > 0 && th.hist[0].at.Before(cut) {
			th.hist = th.hist[1:]
		}
		var recent []float64
		for _, x := range th.hist {
			if x.hasCPU && x.at.After(now.Add(-15*time.Second)) {
				recent = append(recent, x.cpu)
			}
		}
		cpu15 := mean(recent)
		quiet := work < float64(th.idle.Mem)+memThr/2 && cpu15 < cpuThr/2
		if quiet {
			th.quiet = append(th.quiet, qread{now, cpu15, r.Work})
			th.updateIdle(now)
		}
		cpuOver, memOver, cpuUse := th.window(now, cfg.After, cpuThr, memThr)
		if th.ep == nil {
			if cpuOver || memOver {
				th.ep = &episode{since: now, firstOver: now.Add(-cfg.After), window: cfg.After}
			}
		}
		if ep := th.ep; ep != nil {
			memUse := uint64(0)
			if work > float64(th.idle.Mem) {
				memUse = uint64(work) - th.idle.Mem
			}
			ep.cpu, ep.mem = cpuUse, memUse
			ep.overCPU, ep.overMem = cpuUse >= cpuThr/10, float64(memUse) >= memThr/10
			ep.memOver = float64(memUse) >= memThr
			ep.queued = in.Queued[r.Thread]
			ep.peak = max(ep.peak, memUse)
			if r.CPU != nil {
				ep.cpuSum += max(0, *r.CPU-th.idle.CPU)
				ep.cpuN++
			}
			calm := max(0, cpu15-th.idle.CPU) < cpuThr/2 && float64(memUse) < memThr/2
			switch {
			case !calm:
				ep.calmSince = time.Time{}
			case ep.calmSince.IsZero():
				ep.calmSince = now
			}
			if !ep.calmSince.IsZero() && now.Sub(ep.calmSince) >= cfg.After {
				ended = append(ended, ep.record(r.Thread, now, th.idle))
				th.ep = nil
			}
		}
		// Hysteresis aside, a thread that is calm and not flagged keeps nothing.
		_ = cpuOver
	}
	for name, th := range d.threads {
		if th.touched {
			continue
		}
		if th.ep != nil { // its CLI ended
			ended = append(ended, th.ep.record(name, now, th.idle))
		}
		delete(d.threads, name)
	}
	d.publishLocked()
	d.mu.Unlock()
	d.emit(ended)
}

func (d *Detector) emit(ended []Episode) {
	if d.OnEnd != nil {
		for _, e := range ended {
			d.OnEnd(e)
		}
	}
}

func (e *episode) record(thread string, now time.Time, idle Level) Episode {
	ep := Episode{Thread: thread, Command: e.command, Start: e.since, End: now, PeakMem: e.peak, Paused: e.paused}
	if e.cpuN > 0 {
		ep.MeanCores = e.cpuSum / float64(e.cpuN)
	}
	return ep
}

// updateIdle sets the idle level to the median of the quiet readings of the
// last idleSpan, cores and bytes apart (older readings are dropped).
func (th *dthread) updateIdle(now time.Time) {
	cut := now.Add(-idleSpan)
	for len(th.quiet) > 1 && th.quiet[0].at.Before(cut) {
		th.quiet = th.quiet[1:]
	}
	cpus := make([]float64, len(th.quiet))
	mems := make([]uint64, len(th.quiet))
	for i, q := range th.quiet {
		cpus[i], mems[i] = q.cpu, q.mem
	}
	sort.Float64s(cpus)
	slices.Sort(mems)
	n := len(cpus)
	th.idle = Level{CPU: cpus[n/2], Mem: mems[n/2]}
}

// window says whether the use over the window w is over the thresholds, and
// the mean cores above the idle level. The window is cut in three parts; CPU
// is over when the mean in each part is at or above the threshold, memory
// when the lowest working set of the whole window, less the idle level, is.
// A window the readings do not cover yet is not over.
func (th *dthread) window(now time.Time, w time.Duration, cpuThr, memThr float64) (cpuOver, memOver bool, cpuUse float64) {
	start := now.Add(-w)
	part := w / 3
	slack := ProbeEvery * 3 / 2
	var cpus [3][]float64
	var all []float64
	var lowest uint64
	first := true
	coveredCPU, coveredMem := false, false
	for _, x := range th.hist {
		if !x.at.After(start) {
			continue
		}
		if first || x.work < lowest {
			lowest, first = x.work, false
		}
		if !coveredMem && !x.at.After(start.Add(slack)) {
			coveredMem = true
		}
		if !x.hasCPU {
			continue
		}
		if !coveredCPU && !x.at.After(start.Add(slack)) {
			coveredCPU = true
		}
		k := int(x.at.Sub(start) / part)
		if x.at.Sub(start)%part == 0 {
			k-- // a reading at a boundary closes the part before
		}
		k = min(max(k, 0), 2)
		cpus[k] = append(cpus[k], x.cpu)
		all = append(all, x.cpu)
	}
	if coveredCPU {
		cpuOver = true
		for _, p := range cpus {
			if len(p) == 0 || mean(p)-th.idle.CPU < cpuThr {
				cpuOver = false
			}
		}
	}
	cpuUse = max(0, mean(all)-th.idle.CPU)
	if coveredMem && float64(lowest)-float64(th.idle.Mem) >= memThr {
		memOver = true
	}
	return cpuOver, memOver, cpuUse
}

func (d *Detector) publishLocked() {
	var fl []Flag
	lv := map[string]Level{}
	for name, th := range d.threads {
		lv[name] = th.idle
		if ep := th.ep; ep != nil {
			f := Flag{Thread: name, Since: ep.since, FirstOver: ep.firstOver, CPU: ep.cpu, Mem: ep.mem, OverCPU: ep.overCPU, OverMem: ep.overMem, MemOver: ep.memOver,
				Queued: ep.queued, Command: ep.command, Idle: th.idle, Window: ep.window}
			fl = append(fl, f)
		}
	}
	sort.Slice(fl, func(i, j int) bool {
		if !fl[i].Since.Equal(fl[j].Since) {
			return fl[i].Since.Before(fl[j].Since)
		}
		return fl[i].Thread < fl[j].Thread
	})
	d.pub.Store(&fl)
	d.levels.Store(&lv)
}
