package cgroup

// Decision 0029 section 2 (#185): a record per slot hold, measured from the
// holder's thread cgroup: its class (project + the heavy command the gate
// hook saw, else the slot's kind), core-seconds, mean cores, CPU demand,
// memory peak and wall time. The thread's own baseline (its CLI, just
// before the grant) is taken off. S1 only measures; nothing is admitted by
// these numbers yet.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// jobsKey is where the records are kept in the store's KV table.
const jobsKey = "slot-jobs"

// KeepJobs is how many records are kept.
const KeepJobs = 200

// baselineMax caps the cores taken off as the thread's baseline, so a
// thread that was already busy before the grant does not hide its job.
const baselineMax = 0.25

// demandMax caps CPU demand at this many times the CPU the job got.
const demandMax = 4.0

// JobRecord is one finished slot hold.
type JobRecord struct {
	Class   string    `json:"class"`
	Thread  string    `json:"thread"`
	Project string    `json:"project,omitempty"`
	Kind    string    `json:"kind"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	// Wall is the hold's length in seconds.
	Wall        float64 `json:"wall"`
	CoreSeconds float64 `json:"coreSeconds"`
	Cores       float64 `json:"cores"`
	// Demand is Cores over the share of time the job's cgroup was not
	// stalled on CPU: what it would have used, capped at 4×.
	Demand float64 `json:"demand"`
	// Peak is the memory peak above the thread's memory at the grant.
	Peak uint64 `json:"peak"`
	// Slowdown is the host's mean s during the hold; 0 without a probe.
	Slowdown float64 `json:"slowdown,omitempty"`
}

// JobClass sums a class's records for the load: its runs, the newest
// record's measures and the median wall time of its last ten.
type JobClass struct {
	Class  string    `json:"class"`
	Runs   int       `json:"runs"`
	Cores  float64   `json:"cores"`
	Demand float64   `json:"demand"`
	Peak   uint64    `json:"peak"`
	Wall   float64   `json:"wall"`
	At     time.Time `json:"at"`
}

type hold struct {
	kind              string
	since, last       time.Time
	active            time.Time // #174: the newest heavy command or busy reading
	class             string
	cpu0, stall0      uint64
	cpu, stall        uint64
	mem0, maxMem, mem uint64
	base              float64
	peak              *os.File
	sSum              float64
	sN                int
	hasStall, hasPeak bool
}

// Jobs tracks the holds of a pool and keeps their records. Project names a
// thread's project; optional.
type Jobs struct {
	KV      slots.KV
	Project func(thread string) string
	// Now is the clock of Saw; default time.Now.
	Now func() time.Time

	mu     sync.Mutex
	holds  map[string]*hold
	seen   map[string]string // thread → class of its newest heavy command in a hold
	recs   []JobRecord
	loaded bool
}

func (j *Jobs) load() {
	if j.loaded {
		return
	}
	j.loaded = true
	if j.holds == nil {
		j.holds, j.seen = map[string]*hold{}, map[string]string{}
	}
	if j.KV == nil {
		return
	}
	if raw, _ := j.KV.GetKV(jobsKey); raw != "" {
		if err := json.Unmarshal([]byte(raw), &j.recs); err != nil {
			log.Printf("jobs: stored records unreadable, starting empty: %v", err)
			j.recs = nil
		}
	}
}

// Saw tells the tracker that thread ran a heavy command of class (the gate
// hook's match); it names the thread's hold, if it has one now or gets one
// within the run.
func (j *Jobs) Saw(thread, class string) {
	if j == nil || class == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.load()
	if h := j.holds[thread]; h != nil {
		h.class = class
		h.active = j.now()
	}
	j.seen[thread] = class
}

func (j *Jobs) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// busyAbove is how many cores above its baseline a holder must use to count
// as running its job (#174).
const busyAbove = 0.25

// Growth is how much thread's memory grew since its slot was granted, at
// the last reading (decision 0029 S2's memory guard); 0 without a hold.
func (j *Jobs) Growth(thread string) uint64 {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if h := j.holds[thread]; h != nil && h.mem > h.mem0 {
		return h.mem - h.mem0
	}
	return 0
}

// Active is when thread's hold last ran its job: its newest heavy command
// or a reading above its baseline; false without a hold (#174).
func (j *Jobs) Active(thread string) (time.Time, bool) {
	if j == nil {
		return time.Time{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	h := j.holds[thread]
	if h == nil {
		return time.Time{}, false
	}
	return h.active, true
}

// Records are the kept records, the oldest first.
func (j *Jobs) Records() []JobRecord {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.load()
	return slices.Clone(j.recs)
}

// Live is the class of thread's hold now; "" without one.
func (j *Jobs) Live(thread string) string {
	if j == nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if h := j.holds[thread]; h != nil {
		return h.class
	}
	return ""
}

// Classes sums the records per class, the newest class first.
func (j *Jobs) Classes() []JobClass {
	recs := j.Records()
	by := map[string]*JobClass{}
	walls := map[string][]float64{}
	var order []string
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		c := by[r.Class]
		if c == nil {
			c = &JobClass{Class: r.Class, Cores: r.Cores, Demand: r.Demand, Peak: r.Peak, At: r.End}
			by[r.Class] = c
			order = append(order, r.Class)
		}
		c.Runs++
		if len(walls[r.Class]) < 10 {
			walls[r.Class] = append(walls[r.Class], r.Wall)
		}
	}
	out := make([]JobClass, 0, len(order))
	for _, k := range order {
		c := by[k]
		c.Wall = median(walls[k])
		out = append(out, *c)
	}
	return out
}

// tick follows the pool: a new holder gets its baseline, a holder still
// there its newest reading, a holder gone its record. rates are the
// threads' cores over the last read (the baseline); s is the host's
// slowdown now.
func (j *Jobs) tick(t *Tree, snap slots.Snapshot, rates map[string]*float64, s *float64, now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.load()
	now = now.UTC()
	live := map[string]bool{}
	for _, sh := range snap.Held {
		live[sh.Owner] = true
		h := j.holds[sh.Owner]
		if h != nil && !h.since.Equal(sh.Since) {
			j.end(sh.Owner, h) // released and taken again between two ticks
			h = nil
		}
		dir := ""
		if t != nil {
			dir = filepath.Join(t.root, "agents", unsafe.ReplaceAllString(sh.Owner, "_"))
		}
		if h == nil {
			h = &hold{kind: sh.Kind, since: sh.Since, last: now, active: now, class: j.seen[sh.Owner]}
			if r := rates[sh.Owner]; r != nil {
				h.base = min(*r, baselineMax)
			}
			if dir != "" {
				h.cpu0, _ = readKey(filepath.Join(dir, "cpu.stat"), "usage_usec")
				h.cpu = h.cpu0
				if p, ok := readPSI(filepath.Join(dir, "cpu.pressure")); ok {
					h.stall0, h.stall, h.hasStall = p.someTotal, p.someTotal, true
				}
				h.mem0 = readBytes(filepath.Join(dir, "memory.current"))
				h.maxMem, h.mem = h.mem0, h.mem0
				// memory.peak reset per fd (kernel 6.12+): this fd reads the
				// job's own peak; without it the sampled maximum counts.
				if f, err := os.OpenFile(filepath.Join(dir, "memory.peak"), os.O_RDWR, 0); err == nil {
					if _, err := f.WriteString("reset\n"); err == nil {
						h.peak, h.hasPeak = f, true
					} else {
						f.Close()
					}
				}
			}
			j.holds[sh.Owner] = h
			continue
		}
		prevAt, prevCPU := h.last, h.cpu
		h.last = now
		if dir != "" {
			if u, ok := readKey(filepath.Join(dir, "cpu.stat"), "usage_usec"); ok {
				h.cpu = u
				if dt := now.Sub(prevAt).Seconds(); dt > 0 && u >= prevCPU && float64(u-prevCPU)/1e6/dt > h.base+busyAbove {
					h.active = now
				}
			}
			if p, ok := readPSI(filepath.Join(dir, "cpu.pressure")); ok {
				h.stall = p.someTotal
			}
			h.mem = readBytes(filepath.Join(dir, "memory.current"))
			h.maxMem = max(h.maxMem, h.mem)
		}
		if s != nil {
			h.sSum += *s
			h.sN++
		}
	}
	for owner, h := range j.holds {
		if !live[owner] {
			j.end(owner, h)
		}
	}
	for th := range j.seen {
		if !live[th] {
			delete(j.seen, th)
		}
	}
}

// end makes h's record; holds j.mu.
func (j *Jobs) end(owner string, h *hold) {
	delete(j.holds, owner)
	peak := h.maxMem
	if h.peak != nil {
		buf := make([]byte, 32)
		if n, _ := h.peak.ReadAt(buf, 0); n > 0 {
			if v, err := strconv.ParseUint(strings.TrimSpace(string(buf[:n])), 10, 64); err == nil {
				peak = max(peak, v)
			}
		}
		h.peak.Close()
	}
	// The hold ends at its last reading while held: the pool does not say
	// when it was released, and a late tick would add idle time.
	end := h.last.UTC()
	wall := end.Sub(h.since).Seconds()
	if wall <= 0 {
		return
	}
	cs := float64(h.cpu-h.cpu0)/1e6 - h.base*wall
	cs = max(0, cs)
	cores := cs / wall
	demand := cores
	if h.hasStall {
		stalled := float64(h.stall-h.stall0) / 1e6 / wall
		demand = cores / max(1-clamp01(stalled), 1/demandMax)
	}
	r := JobRecord{
		Class: h.class, Thread: owner, Kind: h.kind, Start: h.since.UTC(), End: end,
		Wall: round2(wall), CoreSeconds: round2(cs), Cores: round2(cores), Demand: round2(demand),
	}
	if r.Class == "" {
		r.Class = h.kind
	}
	if j.Project != nil {
		if p := j.Project(owner); p != "" {
			r.Project = p
			r.Class = p + ": " + r.Class
		}
	}
	if peak > h.mem0 {
		r.Peak = peak - h.mem0
	}
	if h.sN > 0 {
		r.Slowdown = round2(h.sSum / float64(h.sN))
	}
	j.recs = append(j.recs, r)
	if len(j.recs) > KeepJobs {
		j.recs = j.recs[len(j.recs)-KeepJobs:]
	}
	if j.KV != nil {
		b, _ := json.Marshal(j.recs)
		if err := j.KV.SetKV(jobsKey, string(b)); err != nil {
			log.Printf("jobs: store: %v", err)
		}
	}
}
