package cgroup

// Decision 0029 S3 (#187): each finished record teaches its class. The
// table keeps raw measures (cores, bytes, seconds), so a changed capacity or
// another host does not invalidate them; the cost is computed at admission
// (slots.NeedOf, V0.163: CPU and memory apart).

import (
	"encoding/json"
	"log"
	"slices"
	"time"
)

// costsKey is where the table is kept in the store's KV table.
const costsKey = "slot-costs"

// Learning rates: demand and the memory peak follow a run's high and decay by
// peakDecay a run (V0.load-tuning: the average under-predicted the runs that
// matter). A starved run (its demand factor above starvedAt) says little about
// the demand, so it only moves it by α/2.
const (
	alpha     = 0.3
	starvedAt = 3.0
	peakDecay = 0.9
	keepWalls = 10
)

// seenFor is how long a thread's named job (a refused or announced heavy
// command) names its class while it waits.
const seenFor = 30 * time.Minute

// Learned is one class's raw measures.
type Learned struct {
	Demand float64   `json:"demand"`
	Peak   uint64    `json:"peak"`
	Walls  []float64 `json:"walls"`
	Runs   int       `json:"runs"`
}

// WallP90 is the 90th percentile of the last wall times, 0 without any.
func (l Learned) WallP90() float64 {
	if len(l.Walls) == 0 {
		return 0
	}
	w := slices.Clone(l.Walls)
	slices.Sort(w)
	return w[min(len(w)-1, int(0.9*float64(len(w))))]
}

// learn adds r to its class; holds j.mu (and j.load ran). It does no I/O
// (V0.147): the caller stores tableRaw after unlocking. A record that measured
// nothing teaches nothing: it would make the class cost almost nothing.
func (j *Jobs) learn(r JobRecord) {
	if r.Demand == 0 && r.Peak == 0 {
		return
	}
	if j.table == nil {
		j.table = map[string]Learned{}
	}
	l := j.table[r.Class]
	starved := r.Cores > 0 && r.Demand/r.Cores > starvedAt
	if l.Runs == 0 {
		l.Demand, l.Peak = r.Demand, r.Peak
	} else {
		if starved {
			l.Demand = round2((1-alpha/2)*l.Demand + alpha/2*r.Demand)
		} else {
			l.Demand = round2(max(r.Demand, peakDecay*l.Demand))
		}
		l.Peak = max(r.Peak, uint64(peakDecay*float64(l.Peak)))
	}
	l.Walls = append(l.Walls, r.Wall)
	if len(l.Walls) > keepWalls {
		l.Walls = l.Walls[len(l.Walls)-keepWalls:]
	}
	l.Runs++
	j.table[r.Class] = l
}

// tableRaw is the table to store; call it with j.mu held. nil without a KV.
func (j *Jobs) tableRaw() []byte {
	if j.KV == nil {
		return nil
	}
	b, _ := json.Marshal(j.table)
	return b
}

// storeTable writes raw (from tableRaw) without j.mu, ordered by saveMu.
func (j *Jobs) storeTable(raw []byte) {
	if raw == nil {
		return
	}
	j.saveMu.Lock()
	defer j.saveMu.Unlock()
	if err := j.KV.SetKV(costsKey, string(raw)); err != nil {
		log.Printf("jobs: store costs: %v", err)
	}
}

func (j *Jobs) loadTable() {
	j.table = map[string]Learned{}
	if j.KV == nil {
		return
	}
	if raw, _ := j.KV.GetKV(costsKey); raw != "" {
		if err := json.Unmarshal([]byte(raw), &j.table); err != nil {
			log.Printf("jobs: stored costs unreadable, starting empty: %v", err)
			j.table = map[string]Learned{}
		}
	}
	// V0.process-tree-hosts: a Mac learned classes from holds it could not measure.
	for c, l := range j.table {
		if l.Demand == 0 && l.Peak == 0 {
			delete(j.table, c)
		}
	}
}

// Learned is class's measures; false before its first run.
func (j *Jobs) Learned(class string) (Learned, bool) {
	if j == nil {
		return Learned{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.load()
	l, ok := j.table[class]
	return l, ok
}

// RaisePeak multiplies class's learned memory peak by f (V0.rerun-classes,
// decision 0036 (F): ×1.2 after Fyr killed a job of it to free memory, as
// VPA raises an estimate after an overrun), so its rerun is admitted only
// where the larger peak fits. False when the class has no peak yet.
func (j *Jobs) RaisePeak(class string, f float64) bool {
	if j == nil {
		return false
	}
	j.mu.Lock()
	j.load()
	l, ok := j.table[class]
	if !ok || l.Peak == 0 {
		j.mu.Unlock()
		return false
	}
	l.Peak = uint64(float64(l.Peak) * f)
	j.table[class] = l
	raw := j.tableRaw()
	j.mu.Unlock()
	j.storeTable(raw)
	return true
}

// Reset forgets class (the owner's Reset); false when it was not learned.
func (j *Jobs) Reset(class string) bool {
	j.mu.Lock()
	j.load()
	if _, ok := j.table[class]; !ok {
		j.mu.Unlock()
		return false
	}
	delete(j.table, class)
	raw := j.tableRaw()
	j.mu.Unlock()
	j.storeTable(raw)
	return true
}

// ClassOf is the class a thread's slot runs or will run: its project and
// the heavy command of its hold, or the one it named while waiting (within
// seenFor), else the slot's kind.
func (j *Jobs) ClassOf(thread, kind string) string {
	j.mu.Lock()
	c := ""
	if h := j.holds[thread]; h != nil && h.class != "" {
		c = h.class
	} else if s, ok := j.seen[thread]; ok && j.now().Sub(j.seenAt[thread]) < seenFor {
		c = s
	}
	p := j.projects[thread]
	j.mu.Unlock()
	if c == "" {
		c = kind
	}
	if p != "" {
		return p + ": " + c
	}
	return c
}

// RefreshProjects caches the project names of threads, asking Project
// outside every lock (#206's order: nothing under Jobs.mu or the pool's lock
// calls the engine). The sampler's Tick calls it for the pool's owners.
func (j *Jobs) RefreshProjects(threads []string) {
	if j == nil || j.Project == nil {
		return
	}
	names := make(map[string]string, len(threads))
	for _, th := range threads {
		names[th] = j.Project(th)
	}
	j.mu.Lock()
	j.projects = names
	j.mu.Unlock()
}

// LiveUse is thread's hold now: its cores over the last reading and how much
// its memory grew since the grant; false without a hold or a reading.
func (j *Jobs) LiveUse(thread string) (cores float64, grew uint64, ok bool) {
	if j == nil {
		return 0, 0, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	h := j.holds[thread]
	if h == nil || h.rateAt.IsZero() {
		return 0, 0, false
	}
	if h.mem > h.mem0 {
		grew = h.mem - h.mem0
	}
	return max(0, h.rate-h.base), grew, true
}
