// V0.load-sim: a deterministic simulation of the load Fyr manages. It runs
// the real decision code (slots.Pool with internal/admission's policy, the
// AIMD budget, HeavyGate, the engine's freezeGuard, turns, the wake rule and
// memoryGuard) against the host model of simhost_test.go, over scenarios the
// owner's hosts have met, and checks invariants. Each scenario also runs with
// no Fyr (every job starts when it arrives) and is compared with running its
// jobs one after another (serial).
//
//	nix develop -c go test ./internal/engine/ -run TestSim -count=1 -v
//
// A violation the current code is known to make is listed in the scenario's
// expect with its reason; it is logged, not failed, until it is fixed.
package engine

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/store"
)

// The invariants.
const (
	invOomd       = "oomd-kill"     // oomd killed a thread while another thread's heavy work could have been frozen
	invThawCrit   = "thaw-critical" // a thaw into the agents' critical stall, before the 30 min ceiling, of work that had not ended
	invMaxFrozen  = "max-frozen"    // more threads frozen than the threads with heavy work less one (V0.sim-fixes)
	invOldest     = "oldest-frozen" // the oldest running hold was frozen
	invService    = "service-frozen"
	invAllFrozen  = "all-frozen"  // live heavy work and none of it ran for 60 s
	invStarved    = "starved"     // a job had not finished at the horizon
	invOscillates = "oscillation" // a part was frozen more than 10 times outside turns
)

type scenario struct {
	name    string
	params  func(*simParams)
	idle    int // agents that run with no heavy work
	units   []*simUnit
	learned map[string]resource.Learned
	desktop func(t time.Duration) float64
	restart time.Duration // fyrd restarts at this time; 0 never
	// expect: the violations the current code makes, with the reason.
	expect map[string]string
}

type simResult struct {
	name                           string
	jobs, done, killed             int
	makespan, noFyr, serial        time.Duration
	noFyrKills                     int
	freezes, maxCycles             int
	maxWait                        time.Duration
	critSecs, noFyrCrit, livelockS int
	peakSwap                       float64
	viol                           map[string][]string
}

// simRun is one run of a scenario with Fyr.
type simRun struct {
	t       *testing.T
	sc      scenario
	h       *simHost
	st      *store.Store
	e       *Engine
	now     *time.Time
	t0      time.Time
	ids     map[string]string // sim thread → engine thread
	sim     map[string]string // engine thread → sim thread
	logs    *bytes.Buffer
	dir     string
	res     simResult
	allStop time.Duration // since when all live heavy work is frozen, -1 when not
	lastNow time.Duration
	ended   map[string]bool
}

func (r *simRun) viol(kind, format string, a ...any) {
	msg := fmt.Sprintf("t=%s ", r.h.t) + fmt.Sprintf(format, a...)
	if len(r.res.viol[kind]) < 5 {
		r.res.viol[kind] = append(r.res.viol[kind], msg)
	} else {
		r.res.viol[kind] = append(r.res.viol[kind][:5], "…")
	}
}

// newEngine builds an engine on the run's store, as fyrd wires one
// (cmd/fyrd/main.go), with the host model behind every dependency.
func (r *simRun) newEngine() {
	e := New(r.st)
	if err := e.Load(); err != nil {
		r.t.Fatal(err)
	}
	e.Now = func() time.Time { return *r.now }
	pool := slots.Open(2, r.st)
	pool.Now = e.Now
	adm := guard.NewAdmission(pool, 2, simSampler{r}, simJobs{r}, 0, 0)
	adm.Now = e.Now
	adm.Open = e.HeavyOpen
	if !r.h.p.OldConcurrency {
		adm.OpenCalls = e.HeavyOpenCalls
	}
	pool.Install(adm)
	e.UseSlots(pool)
	h := r.h
	u2s := func(th string) string { return r.sim[th] }
	e.SlotDeps = SlotDeps{
		SlotActive: func(o string) (time.Time, bool) {
			hd := h.holds[u2s(o)]
			if hd == nil {
				return time.Time{}, false
			}
			return r.t0.Add(hd.active), true
		},
		SlotSignals: func() resource.Signals { return h.signals() },
		SlotGrowth:  func(o string) uint64 { return uint64(r.growth(u2s(o))) },
		SlotPolicy:  adm.Policy,
		SlotOver:    adm.Over,
		SlotFreeze: func(o string) bool {
			th := u2s(o)
			us := h.threadUnits(th, kAdmitted)
			if len(us) == 0 {
				return false
			}
			r.checkFreeze(o, th, true)
			h.freeze(us, false)
			h.reclaim(th)
			r.res.freezes++
			return true
		},
		SlotThaw:  func(o string) { r.checkThaw(u2s(o), kAdmitted); h.thaw(h.threadUnits(u2s(o), kAdmitted)) },
		SlotEmpty: func(o string) bool { return len(h.threadUnits(u2s(o), kAdmitted)) == 0 },
		SlotMem:   func(th string) uint64 { return uint64(h.threadMem(u2s(th))) },
		GuardService: func(th string) bool {
			return len(h.threadUnits(u2s(th), kService)) > 0
		},
		SlotClassNeed: adm.ClassNeed,
		SlotBaseline: func(th string) {
			if hd := h.holds[u2s(th)]; hd != nil {
				hd.mem0 = 0
			}
		},
		SlotAIMD: &guard.AIMD{N: 2, Path: filepath.Join(r.dir, "slot-budget.json"), Policy: adm.Policy},
	}
	e.UndeclaredDeps = UndeclaredDeps{
		Undeclared: func() []resource.Undeclared { return r.flags() },
		UndeclaredFreeze: func(o string) bool {
			th := u2s(o)
			us := h.threadUnits(th, kUndeclared)
			if len(us) == 0 {
				return false
			}
			r.checkFreeze(o, th, false)
			h.freeze(us, true)
			h.reclaim(th)
			r.res.freezes++
			return true
		},
		UndeclaredThaw: func(o string) {
			r.checkThaw(u2s(o), kUndeclared)
			us := h.threadUnits(u2s(o), kUndeclared)
			h.thaw(us)
			for _, u := range us {
				u.turns = false // endTurns: its limit comes back
			}
		},
		UndeclaredEmpty: func(o string) bool { return len(h.threadUnits(u2s(o), kUndeclared)) == 0 },
		UndeclaredRootsMem: func(o string) (uint64, bool) {
			th := u2s(o)
			if f := h.flags[th]; f == nil || !f.flagged {
				return 0, false
			}
			m := 0.0
			for _, u := range h.threadUnits(th, kUndeclared) {
				m += u.res
			}
			return uint64(m), true
		},
		UndeclaredUnits: func(o string) []resource.WorkUnit {
			var out []resource.WorkUnit
			for _, u := range h.threadUnits(u2s(o), kUndeclared) {
				out = append(out, resource.WorkUnit{PID: u.pid, RSS: uint64(u.res), Command: u.Name})
			}
			return out
		},
		UndeclaredRotate: func(o string, units []resource.WorkUnit, active int) bool {
			th := u2s(o)
			var stop []*simUnit
			for _, u := range h.threadUnits(th, kUndeclared) {
				if u.pid == active {
					if u.frozen {
						h.thaw([]*simUnit{u})
					}
					continue
				}
				if !u.frozen {
					stop = append(stop, u)
				}
			}
			for _, u := range h.threadUnits(th, kUndeclared) {
				u.turns = true // V0.guard-tidy: liftForTurns
			}
			if len(stop) > 0 {
				r.checkFreeze(o, th, false)
				h.freeze(stop, true)
				h.reclaim(th)
			}
			return true
		},
		UndeclaredFaults: func(o string) uint64 {
			f := 0.0
			for _, u := range h.units {
				if u.Thread == u2s(o) {
					f += u.faults
				}
			}
			return uint64(f)
		},
	}
	r.e = e
	for sth, id := range r.ids {
		if h.threads[sth] {
			running(e, id)
		}
	}
}

// growth is what thread's hold grew since its grant (cgroup.Jobs.Growth).
func (r *simRun) growth(th string) float64 {
	hd := r.h.holds[th]
	if hd == nil || !hd.unit.live() {
		return 0
	}
	return max(0, hd.unit.res-hd.mem0)
}

// flags are the detector's flags as the engine reads them.
func (r *simRun) flags() []resource.Undeclared {
	h := r.h
	var out []resource.Undeclared
	ths := make([]string, 0, len(h.flags))
	for th := range h.flags {
		ths = append(ths, th)
	}
	sort.Strings(ths)
	for _, th := range ths {
		f := h.flags[th]
		if !f.flagged {
			continue
		}
		mem, cpu := h.undMem(th)
		cmd := ""
		if us := h.threadUnits(th, kUndeclared); len(us) > 0 {
			cmd = us[0].Name
		}
		over := mem >= 0.08*h.p.Budget
		out = append(out, resource.Undeclared{Thread: r.ids[th], Name: th, CPU: cpu, Mem: uint64(mem), Since: r.t0.Add(f.since), Command: cmd,
			OverCPU: cpu >= 0.025*h.p.Cores, OverMem: mem >= 0.008*h.p.Budget, MemOver: over, Kind: resource.Test, Window: 45 * time.Second})
	}
	return out
}

// simSampler is admission's Host.
type simSampler struct{ r *simRun }

func (s simSampler) Signals() resource.Signals         { return s.r.h.signals() }
func (s simSampler) Undeclared() []resource.Undeclared { return s.r.flags() }

// ServiceExtras reserves a running service's peak (V0.services-not-jobs).
func (s simSampler) ServiceExtras() []resource.Extra {
	var out []resource.Extra
	for _, u := range s.r.h.units {
		if u.Kind == kService && u.live() {
			out = append(out, resource.Extra{Owner: s.r.ids[u.Thread], Name: u.Thread, Need: resource.Need{CPU: u.CPU, Mem: uint64(u.Peak)}, Service: true})
		}
	}
	return out
}

// simJobs is admission's Jobs (cgroup.Jobs).
type simJobs struct{ r *simRun }

func (j simJobs) Learned(class string) (resource.Learned, bool) {
	l, ok := j.r.h.learned[class]
	return l, ok
}

func (j simJobs) ClassOf(thread, _ string) string {
	th := j.r.sim[thread]
	for _, u := range j.r.h.units {
		if u.Thread == th && u.Kind == kAdmitted && (u.state == sRunning || u.state == sGated) {
			return u.Class
		}
	}
	return ""
}

func (j simJobs) Qualify(_, class string) string { return class }

// RaisePeak: the simulation wires no rerun classes.
func (j simJobs) RaisePeak(string, float64) bool { return false }

func (j simJobs) LiveUse(thread string) (float64, uint64, bool) {
	hd := j.r.h.holds[j.r.sim[thread]]
	if hd == nil || !hd.unit.live() {
		return 0, 0, false
	}
	return hd.unit.used, uint64(j.r.growth(j.r.sim[thread])), true
}

func (j simJobs) Active(thread string) (time.Time, bool) {
	hd := j.r.h.holds[j.r.sim[thread]]
	if hd == nil {
		return time.Time{}, false
	}
	return j.r.t0.Add(hd.active), true
}

// ---- the invariants checked at the guard's calls ----

var thawLine = regexp.MustCompile(`guard: thawed .* thread=(\S+) after=(\S+) .*reason="([^"]*)"`)

// checkFreeze: never a service, never the oldest running hold.
func (r *simRun) checkFreeze(o, th string, job bool) {
	if len(r.h.threadUnits(th, kService)) > 0 {
		r.viol(invService, "froze thread %s, which runs a service", th)
	}
	if !job {
		return
	}
	var oldest *resource.Holder
	for _, hd := range r.e.Slots.Snapshot().Held {
		fz := false
		for _, u := range r.h.threadUnits(r.sim[hd.Owner], kAdmitted) {
			fz = fz || u.frozen
		}
		if !fz && (oldest == nil || hd.Since.Before(oldest.Since)) {
			hd := hd
			oldest = &hd
		}
	}
	if oldest != nil && oldest.Owner == o {
		r.viol(invOldest, "froze %s, the oldest running hold", th)
	}
}

// checkThaw: no thaw into the agents' critical stall unless the work ended or
// it was frozen 30 min.
func (r *simRun) checkThaw(th string, kind unitKind) {
	us := r.h.threadUnits(th, kind)
	if len(us) == 0 || r.h.agentsAvg*100 < cgroup.StallCritical {
		return
	}
	frozen := time.Duration(0)
	for _, u := range us {
		if u.frozen {
			frozen = max(frozen, r.h.t-u.frozenAt)
		}
	}
	if frozen == 0 || frozen >= guard.FreezeCeiling {
		return
	}
	why := "?"
	tail := r.logs.Bytes()
	tail = tail[max(0, len(tail)-16384):]
	sc := bufio.NewScanner(bytes.NewReader(tail))
	for sc.Scan() {
		if m := thawLine.FindStringSubmatch(sc.Text()); m != nil && m[1] == r.ids[th] {
			why = m[3]
		}
	}
	r.viol(invThawCrit, "thawed %s after %s frozen at agents' stall %.0f%%: %s", th, frozen, r.h.agentsAvg*100, why)
}

// ---- the driver ----

// gate asks HeavyGate for u, as the gate hook does when the agent runs it.
func (r *simRun) gate(u *simUnit) {
	u.lastTry = r.h.t
	why := r.e.HeavyGate(HeavyCall{Thread: r.ids[u.Thread], Class: u.Class, ToolUse: u.tool, Background: u.Background})
	if why == "" {
		if u.state == sGated {
			u.waited = r.h.t - u.gatedAt
		}
		r.h.start(u)
		return
	}
	if u.state != sGated {
		u.state, u.gatedAt = sGated, r.h.t
	}
}

func (r *simRun) arrive(u *simUnit, fyr bool) {
	if r.e != nil && (r.ended[u.Thread] || !r.h.threads[u.Thread]) {
		r.ended[u.Thread] = false
		running(r.e, r.ids[u.Thread])
	}
	r.h.threads[u.Thread] = true
	if !fyr || u.Kind != kAdmitted {
		r.h.start(u)
		return
	}
	r.gate(u)
}

func (r *simRun) busyThread(th string) bool {
	for _, u := range r.h.units {
		if u.Thread == th && (u.state == sPending || u.state == sGated || (u.live() && u.heavy())) {
			return true
		}
	}
	return false
}

// run runs the scenario; fyr false is the same host with no Fyr.
func (r *simRun) run(fyr bool, horizon time.Duration) {
	h := r.h
	if fyr {
		r.newEngine()
	}
	lastGuard := time.Duration(-time.Hour)
	restarted := false
	for h.t < horizon {
		if r.sc.desktop != nil {
			h.desktop = r.sc.desktop(h.t)
		}
		for _, u := range h.units {
			if u.state == sPending && u.At <= h.t {
				r.arrive(u, fyr)
			}
			// The agent runs a queued or held command again when Fyr says so;
			// it is asked again every 10 s, which answers the same until then.
			if fyr && u.state == sGated && h.t-u.lastTry >= 10*time.Second {
				r.gate(u)
			}
		}
		h.step()
		if fyr {
			*r.now = r.t0.Add(h.t)
		}
		if os.Getenv("FYR_SIM_TRACE") == r.sc.name && fyr && h.t%(30*time.Second) == 0 {
			run, gated, frz := map[string]int{}, 0, 0
			for _, u := range h.units {
				switch {
				case u.state == sRunning && u.frozen:
					frz++
				case u.state == sRunning:
					run[u.Thread]++
				case u.state == sGated:
					gated++
				}
			}
			s := h.signals()
			fmt.Printf("TRACE t=%4.0fs running=%v gated=%d frozen=%d agentsMem=%.1fGB swap=%.1fGB stall=%.0f%% head=%.0f%%\n", h.t.Seconds(), run, gated, frz, h.agentsRes/gb, h.swapUsed/gb, *s.UnitStall, *s.Headroom*100)
		}
		if v := h.oomd(); v != "" {
			k := simKill{at: h.t, thread: v, guardable: r.guardable(v)}
			h.kills = append(h.kills, k)
			if fyr {
				switch {
				case h.victimFrozen:
					r.viol(invOomd, "oomd killed thread %s, whose work the guard had frozen (its reclaimed pages are oomd's pgscan)", v)
				case k.guardable:
					r.viol(invOomd, "oomd killed thread %s; another thread's heavy work ran unfrozen", v)
				}
				r.endThread(v)
			}
		}
		for _, u := range h.units {
			if u.state == sDone && u.ended == h.t && fyr && u.Kind == kAdmitted && !u.Background {
				r.e.HeavyDone(r.ids[u.Thread], u.tool)
			}
		}
		if fyr {
			for th := range h.threads {
				if !r.busyThread(th) && !r.ended[th] && !r.idleThread(th) {
					r.endThread(th)
				}
			}
			if r.sc.restart > 0 && !restarted && h.t >= r.sc.restart {
				restarted = true
				r.restart()
			}
			if h.t%(10*time.Second) == 0 {
				r.e.Tick()
				r.checkTick()
			} else if h.psiTrigger() && h.t-lastGuard >= 2*time.Second {
				lastGuard = h.t
				r.e.GuardNow()
			}
		}
		if r.allDone() {
			break
		}
	}
}

func (r *simRun) idleThread(th string) bool { return strings.HasPrefix(th, "idle") }

func (r *simRun) endThread(th string) {
	if r.ended[th] {
		return
	}
	r.ended[th] = true
	delete(r.h.threads, th)
	if _, err := r.e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: r.ids[th], Args: core.RunEndedArgs{}}); err != nil {
		r.t.Fatal(err)
	}
}

// guardable: when oomd killed v, another thread's heavy work ran unfrozen,
// so the guard had something to freeze.
func (r *simRun) guardable(v string) bool {
	others := map[string]bool{}
	for _, u := range r.h.units {
		if u.live() && u.heavy() && !u.frozen && len(r.h.threadUnits(u.Thread, kService)) == 0 {
			others[u.Thread] = true
		}
	}
	others[v] = true // its own counts once
	return len(others) >= 2
}

func (r *simRun) allDone() bool {
	for _, u := range r.h.units {
		if u.Work > 0 && (u.state == sPending || u.state == sGated || u.state == sRunning) {
			return false
		}
	}
	return true
}

// restart is fyrd restarting mid-freeze: the engine's state is gone, what
// was frozen thaws at start (V0.thaw-at-start), the pool and the budget are
// read back from the store.
func (r *simRun) restart() {
	var frozen []*simUnit
	for _, u := range r.h.units {
		if u.live() && u.frozen {
			frozen = append(frozen, u)
		}
	}
	r.h.thaw(frozen)
	r.newEngine()
}

// checkTick checks the invariants that hold between ticks.
func (r *simRun) checkTick() {
	h := r.h
	heavyTh, runsTh := map[string]bool{}, map[string]bool{}
	liveHeavy, runningHeavy := 0, 0
	for _, u := range h.units {
		if u.live() && u.heavy() {
			liveHeavy++
			heavyTh[u.Thread] = true
			if !u.frozen {
				runningHeavy++
				runsTh[u.Thread] = true
			}
		}
	}
	// A thread is frozen when none of its heavy work runs (taking turns, one
	// part does).
	// V0.sim-fixes (3): frozen work left alone wakes at once unless memory
	// stalls critically or near oomd's limit, as the guard reads it (a fresh
	// PSI trigger is critical, V0.freeze-fast-class-name): no thaw into a
	// critical stall (decision 0036 E2, V0.load-invariants). Only then may it
	// stay frozen with no other heavy work running.
	if frozen, n := len(heavyTh)-len(runsTh), len(heavyTh); frozen > max(n-1, 0) {
		r.e.mu.Lock()
		trig := r.e.guardTrig
		r.e.mu.Unlock()
		sig := h.signals().Fresh(trig, r.e.Now())
		if crit, _ := sig.Critical(); !crit && !sig.NearOomd() {
			r.viol(invMaxFrozen, "%d threads frozen with %d with heavy work", frozen, n)
		}
	}
	switch {
	case liveHeavy == 0 || runningHeavy > 0:
		r.allStop = -1
	case r.allStop < 0:
		r.allStop = h.t
	case h.t-r.allStop >= 60*time.Second:
		r.viol(invAllFrozen, "all %d live heavy parts frozen since %s", liveHeavy, r.allStop)
		r.allStop = h.t
	}
}

// ---- the scenarios ----

func unit(th, name, class string, kind unitKind, at time.Duration, cpu, peakGB, work float64) *simUnit {
	return &simUnit{Name: name, Thread: th, Class: class, Kind: kind, At: at, CPU: cpu, Base: 0.1 * gb, Peak: peakGB * gb, Work: work}
}

func learnedClass(cpu, peakGB, wall float64) resource.Learned {
	return resource.Learned{Demand: cpu, Peak: uint64(peakGB * gb), Walls: []float64{wall, wall, wall}, Runs: 5}
}

func scenarios() []scenario {
	var out []scenario
	add := func(s scenario) { out = append(out, s) }
	mn := time.Minute

	// 1. A steady mix of small learned test jobs from six threads.
	{
		s := scenario{name: "steady-small", idle: 1, learned: map[string]resource.Learned{"go test": learnedClass(2, 1, 60)}}
		for i := range 30 {
			th := fmt.Sprintf("t%d", i%6)
			s.units = append(s.units, unit(th, "go test ./...", "go test", kAdmitted, time.Duration(i)*2*mn, 2, 1, 120))
		}
		add(s)
	}
	// 2. One job bigger than the agents' budget, and a small one beside it.
	add(scenario{name: "huge-job", idle: 1, units: []*simUnit{
		unit("big", "yarn e2e:all", "e2e all", kAdmitted, 0, 4, 16, 1200),
		unit("small", "go vet ./...", "go vet", kAdmitted, 2*mn, 1, 0.5, 60),
	}, learned: map[string]resource.Learned{"go vet": learnedClass(1, 0.5, 60)}})
	// 3. Decision 0032's incident: one thread's undeclared turbo run with four
	// heavy parts that fit one at a time.
	{
		s := scenario{name: "turns-4x4GB", idle: 2}
		for i := range 4 {
			u := unit("lint", fmt.Sprintf("tsc %d", i), "", kUndeclared, 0, 1.5, 4, 240)
			u.Ramp = 0.1
			s.units = append(s.units, u)
		}
		add(s)
		s2 := s
		s2.name = "turns-old-reclaim" // the reclaim before V0.freeze-charge
		s2.params = func(p *simParams) { p.ReclaimReachesCLI = false }
		s2.units = cloneUnits(s.units)
		add(s2)
	}
	// 4. Three medium jobs; the desktop takes 40 % of RAM more for 20 mn.
	add(scenario{name: "desktop+40%", idle: 1, units: []*simUnit{
		unit("a", "go test", "go test", kAdmitted, 0, 3, 3, 1800),
		unit("b", "go test", "go test", kAdmitted, 30*time.Second, 3, 3, 1800),
		unit("c", "go test", "go test", kAdmitted, time.Minute, 2, 3, 1200),
	}, learned: map[string]resource.Learned{"go test": learnedClass(3, 3, 600)},
		desktop: func(t time.Duration) float64 {
			if t >= 5*mn && t < 25*mn {
				return 5*gb + 0.4*24*gb
			}
			return 5 * gb
		}})
	// 5. Page-cache-heavy jobs that refault.
	{
		a := unit("a", "webpack", "webpack", kAdmitted, 0, 2, 7, 900)
		b := unit("b", "webpack", "webpack", kAdmitted, 10*time.Second, 2, 7, 900)
		a.File, b.File = 0.7, 0.7
		add(scenario{name: "file-cache", idle: 1, units: []*simUnit{a, b}, learned: map[string]resource.Learned{"webpack": learnedClass(2, 3, 450)}})
	}
	// 6. A burst of ten learned jobs at once.
	{
		s := scenario{name: "burst-10", idle: 1, learned: map[string]resource.Learned{"jest": learnedClass(3, 2, 200)}}
		for i := range 10 {
			s.units = append(s.units, unit(fmt.Sprintf("t%d", i), "jest", "jest", kAdmitted, 0, 3, 2, 600))
		}
		add(s)
	}
	// 7. Two holds whose class says 1.5 GB, each growing to 7 GB.
	add(scenario{name: "two-growers", idle: 1, units: []*simUnit{
		unit("a", "pytest", "pytest", kAdmitted, 0, 2, 7, 1200),
		unit("b", "pytest", "pytest", kAdmitted, 20*time.Second, 2, 7, 1200),
	}, learned: map[string]resource.Learned{"pytest": learnedClass(2, 1.5, 300)}})
	// 8. A dev stack (a service) and e2e jobs of two threads beside it.
	{
		svc := unit("dev", "process-compose up", "", kService, 0, 0.5, 4, 0)
		add(scenario{name: "service+jobs", idle: 1, units: []*simUnit{svc,
			unit("dev", "yarn e2e", "e2e", kAdmitted, time.Minute, 3, 3, 900),
			unit("b", "yarn e2e", "e2e", kAdmitted, time.Minute, 3, 3, 900),
			unit("c", "yarn e2e", "e2e", kAdmitted, 2*time.Minute, 3, 3, 900),
		}, learned: map[string]resource.Learned{"e2e": learnedClass(3, 3, 300)}})
	}
	// 9. CPU only: six jobs of 4 cores, little memory.
	{
		s := scenario{name: "cpu-only", idle: 1, learned: map[string]resource.Learned{"cargo": learnedClass(4, 0.5, 150)}}
		for i := range 6 {
			s.units = append(s.units, unit(fmt.Sprintf("t%d", i), "cargo build", "cargo", kAdmitted, time.Duration(i)*10*time.Second, 4, 0.5, 600))
		}
		add(s)
	}
	// 10. A slow leak in undeclared work (20 MB/s up to 10 GB), and learned
	// jobs of another thread.
	{
		leak := unit("leaky", "node server.js --watch", "", kUndeclared, 0, 1, 1.5, 3600)
		leak.Leak, leak.LeakTo = 20e6, 10*gb
		s := scenario{name: "leak", idle: 1, units: []*simUnit{leak}, learned: map[string]resource.Learned{"go test": learnedClass(2, 2, 120)}}
		for i := range 8 {
			s.units = append(s.units, unit("b", "go test", "go test", kAdmitted, time.Duration(2+i*6)*mn, 2, 2, 240))
		}
		add(s)
	}
	// 11. fyrd restarts while a job is frozen.
	add(scenario{name: "restart-frozen", idle: 1, restart: 4 * mn, units: []*simUnit{
		unit("a", "pytest", "pytest", kAdmitted, 0, 2, 7, 1200),
		unit("b", "pytest", "pytest", kAdmitted, 20*time.Second, 2, 7, 1200),
	}, learned: map[string]resource.Learned{"pytest": learnedClass(2, 1.5, 300)}})
	// 12. The agents' swap is nearly gone: three undeclared jobs of 5 GB.
	add(scenario{name: "swap-full", idle: 1, params: func(p *simParams) { p.SwapCap = 2 * gb }, units: []*simUnit{
		unit("a", "tsc -b", "", kUndeclared, 0, 2, 5, 900),
		unit("b", "tsc -b", "", kUndeclared, 20*time.Second, 2, 5, 900),
		unit("c", "tsc -b", "", kUndeclared, 40*time.Second, 2, 5, 900),
	}})
	// 13. Undeclared work of an unknown command is frozen while memory is
	// short: V0.job-limits sizes it from the free budget at the move (the
	// 73 MB limit of 19 Nov).
	{
		mk := func() []*simUnit {
			a := unit("a", "vite build", "", kUndeclared, mn, 2, 5.5, 1500)
			a.Ramp = 0.9 // it grows over most of its run, so most of it comes after the move
			return []*simUnit{unit("base", "make build", "build", kAdmitted, 0, 2, 7, 1500), a}
		}
		l := map[string]resource.Learned{"build": learnedClass(2, 7, 750)}
		add(scenario{name: "unknown-limit", idle: 1, units: mk(), learned: l})
		add(scenario{name: "unknown-limit-old", idle: 1, params: func(p *simParams) { p.LimitFloor = false }, units: mk(), learned: l}) // the limits before V0.freeze-charge
	}
	// 14. Near oomd's limit: oomd at 50 %, three heavy undeclared threads.
	add(scenario{name: "near-oomd", idle: 1, params: func(p *simParams) { p.OomdLimit = 50 }, units: []*simUnit{
		unit("a", "next build", "", kUndeclared, 0, 3, 5, 900),
		unit("b", "next build", "", kUndeclared, 5*time.Second, 3, 5, 900),
		unit("c", "next build", "", kUndeclared, 10*time.Second, 3, 5, 900),
	}})
	// 15. Background jobs that outlive their call, growing past their class.
	{
		a := unit("a", "make check &", "make check", kAdmitted, 0, 2, 7, 1500)
		b := unit("b", "make check &", "make check", kAdmitted, 20*time.Second, 2, 7, 1500)
		a.Background, b.Background = true, true
		add(scenario{name: "background", idle: 1, units: []*simUnit{a, b}, learned: map[string]resource.Learned{"make check": learnedClass(2, 1.5, 300)}})
	}
	// 16. V0.freeze-controller: the 26 Nov laptop. One thread's lint run of
	// four heavy parts (7.5 GB with the stack), a 5 GB smoke run of another,
	// and six threads with small jobs, nine agents in all, swap cap 10 GB.
	{
		s := scenario{name: "incident-26nov", idle: 3, params: func(p *simParams) { p.SwapCap = 10 * gb; p.Budget = 12 * gb; p.RAM = 30 * gb }}
		for i, g := range []float64{2.2, 1.9, 0.9, 0.6} {
			u := unit("lint", fmt.Sprintf("lint part %d", i), "", kUndeclared, 0, 1.2, g, 300)
			u.Ramp = 0.1
			s.units = append(s.units, u)
		}
		smoke := unit("smoke", "yarn smoke --db", "", kUndeclared, 20*time.Second, 2.5, 5, 240)
		smoke.Ramp = 0.2
		s.units = append(s.units, smoke)
		for i := range 6 {
			s.units = append(s.units, unit(fmt.Sprintf("s%d", i), "go test ./internal/...", "go test", kAdmitted, time.Duration(30+20*i)*time.Second, 2, 1.2, 150))
		}
		s.learned = map[string]resource.Learned{"go test": learnedClass(2, 1.2, 150)}
		add(s)
	}
	// 16b. The same laptop with what the log also showed: the desktop and the
	// user's session hold 9 GB, and the lint thread starts a new run every 30 s
	// (its agent retries what timed out), so parts pile up while the first
	// ones are slowed by the stall.
	{
		s := scenario{name: "incident-retry", idle: 3, params: func(p *simParams) { p.SwapCap = 10 * gb; p.Budget = 12 * gb; p.RAM = 30 * gb },
			desktop: func(time.Duration) float64 { return 9 * gb }}
		for i := range 30 {
			u := unit("lint", fmt.Sprintf("yarn lint %d", i), "", kUndeclared, time.Duration(30*i)*time.Second, 1.2, 2.2, 240)
			u.Ramp = 0.1
			s.units = append(s.units, u)
		}
		smoke := unit("smoke", "yarn smoke --db", "", kUndeclared, 20*time.Second, 2.5, 5, 240)
		smoke.Ramp = 0.2
		s.units = append(s.units, smoke)
		for i := range 6 {
			s.units = append(s.units, unit(fmt.Sprintf("s%d", i), "go test ./internal/...", "go test", kAdmitted, time.Duration(30+20*i)*time.Second, 2, 1.2, 150))
		}
		s.learned = map[string]resource.Learned{"go test": learnedClass(2, 1.2, 150)}
		add(s)
	}
	// 16c. incident-retry with the lint command known to the gate (a class,
	// as it is once the book has learned it): each run is an admitted job.
	{
		s := scenario{name: "incident-retry-class", idle: 3, params: func(p *simParams) { p.SwapCap = 10 * gb; p.Budget = 12 * gb; p.RAM = 30 * gb },
			desktop: func(time.Duration) float64 { return 9 * gb }}
		for i := range 30 {
			s.units = append(s.units, unit("lint", fmt.Sprintf("yarn lint %d", i), "yarn lint", kAdmitted, time.Duration(30*i)*time.Second, 1.2, 2.2, 240))
		}
		smoke := unit("smoke", "yarn smoke --db", "", kUndeclared, 20*time.Second, 2.5, 5, 240)
		smoke.Ramp = 0.2
		s.units = append(s.units, smoke)
		for i := range 6 {
			s.units = append(s.units, unit(fmt.Sprintf("s%d", i), "go test ./internal/...", "go test", kAdmitted, time.Duration(30+20*i)*time.Second, 2, 1.2, 150))
		}
		s.learned = map[string]resource.Learned{"go test": learnedClass(2, 1.2, 150), "yarn lint": learnedClass(1.2, 2.2, 240)}
		add(s)
	}
	// 16d. V0.freeze-controller: one thread starts n heavy 3 GB commands of
	// one class at once (an agent running its lint in several shells or
	// retrying what timed out), beside two small threads. Old is the tree
	// that counted the thread's open calls as one need.
	for _, n := range []int{4, 8, 16} {
		for _, old := range []bool{true, false} {
			name := fmt.Sprintf("concurrent-%d", n)
			if old {
				name += "-old"
			}
			s := scenario{name: name, idle: 2, learned: map[string]resource.Learned{"yarn lint": learnedClass(1.5, 3, 200), "go test": learnedClass(2, 1, 100)}}
			if old {
				s.params = func(p *simParams) { p.OldConcurrency = true }
			}
			for i := range n {
				s.units = append(s.units, unit("lint", fmt.Sprintf("yarn lint %d", i), "yarn lint", kAdmitted, time.Duration(10*i)*time.Second, 1.5, 3, 300))
			}
			for i := range 4 {
				s.units = append(s.units, unit(fmt.Sprintf("s%d", i), "go test", "go test", kAdmitted, time.Duration(20+30*i)*time.Second, 2, 1, 120))
			}
			add(s)
		}
	}
	// 16e. incident-retry when the repeat of a command is a class at once but
	// nothing is learned of it: the gate charges the prior (1/N of the host).
	{
		s := scenario{name: "incident-retry-unknown", idle: 3, params: func(p *simParams) { p.SwapCap = 10 * gb; p.Budget = 12 * gb; p.RAM = 30 * gb },
			desktop: func(time.Duration) float64 { return 9 * gb }}
		for i := range 30 {
			s.units = append(s.units, unit("lint", fmt.Sprintf("yarn lint %d", i), "yarn lint", kAdmitted, time.Duration(30*i)*time.Second, 1.2, 2.2, 240))
		}
		smoke := unit("smoke", "yarn smoke --db", "", kUndeclared, 20*time.Second, 2.5, 5, 240)
		smoke.Ramp = 0.2
		s.units = append(s.units, smoke)
		for i := range 6 {
			s.units = append(s.units, unit(fmt.Sprintf("s%d", i), "go test ./internal/...", "go test", kAdmitted, time.Duration(30+20*i)*time.Second, 2, 1.2, 150))
		}
		s.learned = map[string]resource.Learned{"go test": learnedClass(2, 1.2, 150)}
		add(s)
	}
	// 17. V0.freeze-controller: one law from light to heavy. n threads each
	// run an undeclared 3 GB, 2 core job; the budget is 12 GB.
	for _, n := range []int{2, 4, 6, 8} {
		s := scenario{name: fmt.Sprintf("ladder-%d", n), idle: 1}
		for i := range n {
			u := unit(fmt.Sprintf("l%d", i), "tsc -b", "", kUndeclared, time.Duration(5*i)*time.Second, 2, 3, 300)
			u.Ramp = 0.15
			s.units = append(s.units, u)
		}
		add(s)
	}
	// 17b. The ladder with the tool a class (unknown, nothing learned): what
	// it would be if the gate named the same heavy command from several threads.
	for _, n := range []int{4, 8} {
		s := scenario{name: fmt.Sprintf("ladder-class-%d", n), idle: 1}
		for i := range n {
			s.units = append(s.units, unit(fmt.Sprintf("l%d", i), "tsc -b", "tsc -b", kAdmitted, time.Duration(5*i)*time.Second, 2, 3, 300))
		}
		add(s)
	}
	// V0.sim-fixes fixed the rest of V0.load-sim's list: a violation of
	// those scenarios now fails the test.
	known := map[string]map[string]string{
		"swap-full":      {invOomd: whyFrozenKill},
		"incident-retry": {invOomd: whyRetryStorm},
	}
	for i := range out {
		out[i].expect = known[out[i].name]
	}
	return out
}

// Why the current code makes the violations the table lists (V0.load-sim).
// Each is a finding to fix; remove its line when the fix lands.
const (
	// V0.freeze-controller: the 26 Nov laptop with the retry the log showed
	// (a new lint run every 30 s from a thread whose agent retries what timed
	// out) and the desktop at 9 GB: 31 threads lost to oomd, 6 of 37 jobs done.
	// The undeclared parts start whatever the guard holds, so freezing cannot
	// outrun them. Tried and not taken (see docs/changes/freeze-controller.md):
	// the heaviest thread first, a wait that shrinks with the stall, and
	// freezing all the heavy work at oomd's limit; each lost more threads to
	// oomd on ladder-4/6/8 than today's rules.
	whyRetryStorm = "a thread starts a new heavy part every 30 s and the guard cannot hold what the gate does not see: parts pile up faster than freezes take memory out"
	// V0.sim-fixes (5): left as a finding of the model. The 2 GB of swap
	// cannot hold the two parts that run, so the stall reaches oomd's limit
	// with or without Fyr (no Fyr loses a thread too); the model counts the
	// kernel's reclaim of the frozen part's cold pages as its pgscan, so oomd
	// takes the frozen thread. Fyr's own reclaim is already off near the limit
	// (V0.guard-tidy); how real oomd ranks a frozen cgroup is not measured.
	whyFrozenKill = "oomd picks the thread whose frozen pages the kernel reclaimed: the model counts that as its pgscan, so the frozen thread is oomd's first choice once the two that run fill the 2 GB of swap"
)

func cloneUnits(us []*simUnit) []*simUnit {
	out := make([]*simUnit, len(us))
	for i, u := range us {
		c := *u
		out[i] = &c
	}
	return out
}

// serial is the jobs run one after another from the first arrival, each
// alone on the host (its memory past the agents' room thrashes as m(phi)).
func serial(sc scenario, p simParams) time.Duration {
	var first time.Duration = -1
	total := 0.0
	room := p.Budget - p.CLI*2
	for _, u := range sc.units {
		if u.Work == 0 {
			continue
		}
		if first < 0 || u.At < first {
			first = u.At
		}
		total += u.Work / min(u.CPU, p.Cores) * slow(min(1, room/u.Peak))
	}
	return first + time.Duration(total*float64(time.Second))
}

func (r *simRun) finish(fyr bool) {
	h := r.h
	end := time.Duration(0)
	for _, u := range h.units {
		if u.Work == 0 {
			continue
		}
		switch u.state {
		case sDone:
			r.res.done++
			end = max(end, u.ended)
		case sKilled:
			r.res.killed++
		default:
			if fyr {
				r.viol(invStarved, "%s of %s did not finish (state %d, %.0f of %.0f core-s)", u.Name, u.Thread, u.state, u.done, u.Work)
			}
		}
		r.res.jobs++
		r.res.maxWait = max(r.res.maxWait, u.waited)
		if u.state == sGated {
			r.res.maxWait = max(r.res.maxWait, h.t-u.gatedAt)
		}
		cyc := u.freezes
		if r.turnsThread(u.Thread) {
			cyc = 0
		}
		r.res.maxCycles = max(r.res.maxCycles, cyc)
		if fyr && cyc > 10 {
			r.viol(invOscillates, "%s of %s frozen %d times", u.Name, u.Thread, cyc)
		}
	}
	r.res.makespan = end
	if r.res.done+r.res.killed < r.res.jobs {
		r.res.makespan = -1
	}
	r.res.critSecs, r.res.livelockS, r.res.peakSwap = h.critSecs, h.livelockS, h.peakSwap
}

// turnsThread: the thread's undeclared work has two or more parts (it takes turns).
func (r *simRun) turnsThread(th string) bool {
	n := 0
	for _, u := range r.h.units {
		if u.Thread == th && u.Kind == kUndeclared {
			n++
		}
	}
	return n >= 2
}

func runScenario(t *testing.T, sc scenario) simResult {
	p := defaultParams()
	if sc.params != nil {
		sc.params(&p)
	}
	ser := serial(sc, p)
	horizon := min(8*time.Hour, 4*ser+time.Hour)
	mk := func(fyr bool) *simRun {
		hp := p
		if !fyr {
			hp.JobLimits = false
		}
		h := newSimHost(hp)
		h.desktop = 5 * gb
		for _, u := range cloneUnits(sc.units) {
			h.add(u)
			u.tool = fmt.Sprintf("tu-%d", u.pid)
		}
		for k, v := range sc.learned {
			h.learned[k] = v
		}
		r := &simRun{t: t, sc: sc, h: h, ids: map[string]string{}, sim: map[string]string{}, ended: map[string]bool{}, allStop: -1,
			res: simResult{name: sc.name, viol: map[string][]string{}}, dir: t.TempDir(), logs: &bytes.Buffer{}}
		for i := range sc.idle {
			h.threads[fmt.Sprintf("idle%d", i)] = true
		}
		if fyr {
			e, st, now := bare(t)
			_ = e
			r.st, r.now, r.t0 = st, now, *now
			var names []string
			for _, u := range h.units {
				if !slices.Contains(names, u.Thread) {
					names = append(names, u.Thread)
				}
			}
			for th := range h.threads {
				names = append(names, th)
			}
			sort.Strings(names)
			for _, th := range names {
				id := bareThread(t, e)
				r.ids[th], r.sim[id] = id, th
			}
		}
		return r
	}
	with := mk(true)
	log.SetOutput(with.logs)
	with.run(true, horizon)
	log.SetOutput(os.Stderr)
	with.finish(true)
	without := mk(false)
	without.run(false, horizon)
	without.finish(false)
	res := with.res
	res.noFyr, res.noFyrKills, res.noFyrCrit, res.serial = without.res.makespan, without.res.killed, without.h.critSecs, ser
	if testing.Verbose() && os.Getenv("FYR_SIM_LOG") == sc.name {
		io.Copy(os.Stderr, with.logs)
	}
	return res
}

func dur(d time.Duration) string {
	if d < 0 {
		return "—"
	}
	return fmt.Sprintf("%.0fm", d.Minutes())
}

// TestSimLoad runs every scenario and prints one row each. FYR_SIM_LOG=<name>
// with -v prints that scenario's guard log.
func TestSimLoad(t *testing.T) {
	var rows []string
	failed := false
	for _, sc := range scenarios() {
		res := runScenario(t, sc)
		var vs []string
		for k := range res.viol {
			vs = append(vs, k)
		}
		sort.Strings(vs)
		rows = append(rows, fmt.Sprintf("%-20s %3d/%-3d %4d %5s %6s %6s %5d %5d %5s %6d %6d %6.1f  %s",
			res.name, res.done, res.jobs, res.killed, dur(res.makespan), dur(res.noFyr)+fmt.Sprintf("/%d", res.noFyrKills), dur(res.serial),
			res.freezes, res.maxCycles, dur(res.maxWait), res.critSecs, res.noFyrCrit, res.peakSwap/gb, strings.Join(vs, ",")))
		for _, k := range vs {
			lines := strings.Join(res.viol[k], "\n    ")
			if why, ok := sc.expect[k]; ok {
				t.Logf("%s: expected %s (%s):\n    %s", sc.name, k, why, lines)
				continue
			}
			failed = true
			t.Errorf("%s: %s:\n    %s", sc.name, k, lines)
		}
		for k := range sc.expect {
			if _, ok := res.viol[k]; !ok {
				t.Logf("%s: the expected %s did not happen: remove it from expect", sc.name, k)
			}
		}
	}
	head := fmt.Sprintf("%-20s %7s %4s %5s %8s %6s %5s %5s %5s %6s %6s %6s  %s", "scenario", "done", "oomd", "fyr", "nofyr/k", "serial", "frz", "cyc", "wait", "crit", "crit0", "swapGB", "violations")
	t.Logf("\n%s\n%s", head, strings.Join(rows, "\n"))
	if failed {
		t.Log("an unexpected violation: fix it, or add it to known in scenarios() with its reason")
	}
}
