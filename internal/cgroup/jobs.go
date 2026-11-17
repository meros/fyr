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
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/lockwatch"
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
	// Decision 0029 S3: the class's cost on this host now and how many
	// runs it is learned from; 0 when not learned.
	Cost    float64 `json:"cost,omitempty"`
	Learned int     `json:"learned,omitempty"`
}

type hold struct {
	kind              string
	since, last       time.Time
	active            time.Time // #174: the newest heavy command or busy reading
	rate              float64   // decision 0029 S3: cores over the last reading
	rateAt            time.Time
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

	mu     lockwatch.Mutex
	saveMu sync.Mutex // orders the KV writes; taken without mu
	view   atomic.Pointer[JobsView]
	holds  map[string]*hold
	seen   map[string]string    // thread → class of its newest heavy command (in a hold or while waiting)
	seenAt map[string]time.Time // and when
	recs   []JobRecord
	table  map[string]Learned // decision 0029 S3: per class
	// projects: the pool's owners' project names, cached by RefreshProjects
	projects map[string]string
	loaded   bool
}

func (j *Jobs) load() {
	if j.loaded {
		return
	}
	j.loaded = true
	if j.holds == nil {
		j.holds, j.seen, j.seenAt = map[string]*hold{}, map[string]string{}, map[string]time.Time{}
	}
	j.loadTable()
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
	j.seen[thread], j.seenAt[thread] = class, j.now()
	j.publishLocked()
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
func (j *Jobs) Classes() []JobClass { return classes(j.Records()) }

func classes(recs []JobRecord) []JobClass {
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

// reading is one cgroup's measures, read without j.mu (V0.147).
type reading struct {
	cpu, stall       uint64
	mem              uint64
	hasCPU, hasStall bool
}

func readCgroup(dir string) reading {
	var r reading
	r.cpu, r.hasCPU = readKey(filepath.Join(dir, "cpu.stat"), "usage_usec")
	if p, ok := readPSI(filepath.Join(dir, "cpu.pressure")); ok {
		r.stall, r.hasStall = p.someTotal, true
	}
	r.mem = readBytes(filepath.Join(dir, "memory.current"))
	return r
}

// openPeak opens dir's memory.peak and resets it for this fd (kernel
// 6.12+): the fd then reads the job's own peak; nil without it, and the
// sampled maximum counts.
func openPeak(dir string) *os.File {
	f, err := os.OpenFile(filepath.Join(dir, "memory.peak"), os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	if _, err := f.WriteString("reset\n"); err != nil {
		f.Close()
		return nil
	}
	return f
}

// ended is a hold that ended, to be recorded without j.mu.
type ended struct {
	owner string
	h     *hold
}

// tick follows the pool: a new holder gets its baseline, a holder still
// there its newest reading, a holder gone its record. rates are the
// threads' cores over the last read (the baseline); s is the host's
// slowdown now. V0.147: the cgroup files are read, the project asked and
// the records stored without j.mu, so j.mu is never held across I/O or a
// call out of the tracker (the engine's tick asks Active under e.mu).
func (j *Jobs) tick(t *Tree, snap slots.Snapshot, rates map[string]*float64, s *float64, now time.Time) {
	now = now.UTC()
	dirOf := func(owner string) string {
		if t == nil {
			return ""
		}
		return filepath.Join(t.root, "agents", unsafe.ReplaceAllString(owner, "_"))
	}
	// 1. Which holds are new (under j.mu, no I/O).
	j.mu.Lock()
	j.load()
	fresh := map[string]bool{}
	for _, sh := range snap.Held {
		if h := j.holds[sh.Owner]; h == nil || !h.since.Equal(sh.Since) {
			fresh[sh.Owner] = true
		}
	}
	j.mu.Unlock()
	// 2. The readings and the new holds' peak fds (no lock).
	reads := map[string]reading{}
	peaks := map[string]*os.File{}
	for _, sh := range snap.Held {
		dir := dirOf(sh.Owner)
		if dir == "" {
			continue
		}
		reads[sh.Owner] = readCgroup(dir)
		if fresh[sh.Owner] {
			peaks[sh.Owner] = openPeak(dir)
		}
	}
	// 3. Apply (under j.mu, no I/O).
	var done []ended
	j.mu.Lock()
	live := map[string]bool{}
	for _, sh := range snap.Held {
		live[sh.Owner] = true
		h := j.holds[sh.Owner]
		if h != nil && !h.since.Equal(sh.Since) {
			delete(j.holds, sh.Owner) // released and taken again between two ticks
			done = append(done, ended{sh.Owner, h})
			h = nil
		}
		r, hasDir := reads[sh.Owner]
		if h == nil {
			h = &hold{kind: sh.Kind, since: sh.Since, last: now, active: now, class: j.seen[sh.Owner]}
			if r := rates[sh.Owner]; r != nil {
				h.base = min(*r, baselineMax)
			}
			if hasDir {
				h.cpu0, h.cpu = r.cpu, r.cpu
				if r.hasStall {
					h.stall0, h.stall, h.hasStall = r.stall, r.stall, true
				}
				h.mem0, h.maxMem, h.mem = r.mem, r.mem, r.mem
			}
			if f := peaks[sh.Owner]; f != nil {
				h.peak, h.hasPeak = f, true
				delete(peaks, sh.Owner)
			}
			j.holds[sh.Owner] = h
			continue
		}
		prevAt, prevCPU := h.last, h.cpu
		h.last = now
		if hasDir {
			if r.hasCPU {
				u := r.cpu
				h.cpu = u
				if dt := now.Sub(prevAt).Seconds(); dt > 0 && u >= prevCPU {
					h.rate, h.rateAt = float64(u-prevCPU)/1e6/dt, now
					if h.rate > h.base+busyAbove {
						h.active = now
					}
				}
			}
			if r.hasStall {
				h.stall = r.stall
			}
			h.mem = r.mem
			h.maxMem = max(h.maxMem, h.mem)
		}
		if s != nil {
			h.sSum += *s
			h.sN++
		}
	}
	for owner, h := range j.holds {
		if !live[owner] {
			delete(j.holds, owner)
			done = append(done, ended{owner, h})
		}
	}
	for th, at := range j.seenAt {
		if !live[th] && now.Sub(at) >= seenFor {
			delete(j.seen, th)
			delete(j.seenAt, th)
		}
	}
	j.publishLocked()
	j.mu.Unlock()
	for _, f := range peaks {
		if f != nil {
			f.Close() // a concurrent tick made the hold first
		}
	}
	j.finish(done)
}

// finish makes the ended holds' records: the peak and the project are read
// without j.mu, the records added under it and stored after it.
func (j *Jobs) finish(done []ended) {
	if len(done) == 0 {
		return
	}
	var recs []JobRecord
	for _, e := range done {
		if r, ok := j.record(e.owner, e.h); ok {
			recs = append(recs, r)
		}
	}
	if len(recs) == 0 {
		return
	}
	j.mu.Lock()
	for _, r := range recs {
		j.learn(r) // decision 0029 S3: the class's measures, no I/O under j.mu
	}
	table := j.tableRaw()
	j.recs = append(j.recs, recs...)
	if len(j.recs) > KeepJobs {
		j.recs = j.recs[len(j.recs)-KeepJobs:]
	}
	var raw []byte
	if j.KV != nil {
		raw, _ = json.Marshal(j.recs)
	}
	j.publishLocked()
	j.mu.Unlock()
	j.storeTable(table)
	if raw != nil {
		j.saveMu.Lock()
		defer j.saveMu.Unlock()
		if err := j.KV.SetKV(jobsKey, string(raw)); err != nil {
			log.Printf("jobs: store: %v", err)
		}
	}
}

// record is h's record; false for a hold of no length. Call it without j.mu:
// it reads h's peak fd and asks Project (the engine).
func (j *Jobs) record(owner string, h *hold) (JobRecord, bool) {
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
		return JobRecord{}, false
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
	return r, true
}

// JobsView is what GET /api/load shows of the tracker, published after
// each change so a read takes no lock (V0.147).
type JobsView struct {
	Classes []JobClass
	Live    map[string]string // thread → the class of its hold now
}

// publishLocked publishes the view; call it with j.mu held.
func (j *Jobs) publishLocked() {
	live := make(map[string]string, len(j.holds))
	for th, h := range j.holds {
		live[th] = h.class
	}
	j.view.Store(&JobsView{Classes: classes(j.recs), Live: live})
}

// View is the newest published view; never nil.
func (j *Jobs) View() *JobsView {
	if j == nil {
		return &JobsView{}
	}
	if v := j.view.Load(); v != nil {
		return v
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.load()
	j.publishLocked()
	return j.view.Load()
}
