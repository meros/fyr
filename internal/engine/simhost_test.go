// V0.load-sim: the host model the load simulation (sim_test.go) runs Fyr's
// real admission and guard against. Every figure is a share or a rate of a
// host like the owner's laptop (decision 0029 section 0); the curves are
// loosely fitted to decision 0032's measurements and are a model, not a
// measurement. One step is one second.
//
// The model, in short:
//   - CPU: the running parts ask for their cores; past the agents' cores each
//     gets its share (cores/demand). The probe's slowdown is demand/cores.
//   - Memory: the agents may hold Budget (memory.high), less what the desktop
//     takes past RAM. A frozen part keeps its pages until the kernel moves the
//     cold ones to swap (ColdReclaim a second) or Fyr's reclaim does
//     (V0.guard-swap). When the running parts want more than fits, each keeps
//     the same share phi of what it wants and the rest goes to swap (anon) or
//     is dropped (page cache). Anon that swap cannot take is a livelock: no
//     progress, a full stall (decision 0032: memory.high with no swap room
//     never made progress in 25 s).
//   - A part at phi runs m(phi) = 1 + 51.6 (1-phi)^1.44 times slower: 8× at
//     75 %, 20× at 50 % (decision 0032), about 7× worse than serial for four
//     parts that fit one at a time (0032 measured 4.7–8.5×). A part whose
//     pages are in swap reads them back at ReadBack bytes a second (shared),
//     at a quarter of its speed while it does.
//   - Stall: a part counts as fully stalled 1 - m^-0.5 of its time (at least
//     0.5 while it reads back; see stalled); the agents' memory full avg10 is
//     the CPU-weighted mean of that,
//     averaged over 10 s. The host's adds the desktop's own pressure near RAM.
//   - oomd: the agents' slice's full avg10 over OomdLimit for OomdFor kills
//     the thread with the most pages scanned lately (memory.oom.group).
//   - V0.job-limits: a job's memory.high is 1.25 × its class's peak (at least
//     the prior under 3 runs), or for an unknown class the free budget, at
//     most half; it lifts while swap is at 90 % of its cap. V0.sim-fixes: a
//     hit raises it to the job's use × 1.25, and it lifts while the job's
//     own stall is critical.
package engine

import (
	"math"
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/guard/resource"
)

const gb = 1e9

// simParams is the host. Switches select the tree before a fix, so the table
// can show what the fix changed.
type simParams struct {
	Cores       float64
	Budget      float64 // the agents' memory.high
	RAM         float64
	HostBase    float64 // kernel, fyrd, the desktop's floor outside Desktop
	CLI         float64 // one running agent CLI
	SwapCap     float64 // the agents' memory.swap.max
	ReadBack    float64 // bytes a second read back from swap
	ColdReclaim float64 // bytes a second the kernel moves of frozen (cold) pages to swap under pressure
	Evict       float64 // bytes a second the kernel takes from one running part over its share
	OomdLimit   float64 // percent
	OomdFor     time.Duration
	JobLimits   bool // V0.job-limits
	// ReclaimReachesCLI: Fyr's reclaim of frozen undeclared work also reaches
	// the pages still charged to the thread's cli/ (V0.freeze-charge (4); off
	// is the tree before it).
	ReclaimReachesCLI bool
	// LimitFloor: V0.freeze-charge (2, 3): undeclared work the guard moves
	// gets no memory.high, and an unknown job's is never below the prior, half
	// the budget. Off is the tree before it, which sized both from the free
	// budget at the move (73 MB on 19 Nov).
	LimitFloor bool
}

func defaultParams() simParams {
	return simParams{Cores: 8, Budget: 12 * gb, RAM: 24 * gb, HostBase: 1.5 * gb, CLI: 0.4 * gb, SwapCap: 8 * gb,
		ReadBack: 400e6, ColdReclaim: 200e6, Evict: 1e9, OomdLimit: 85, OomdFor: 20 * time.Second, JobLimits: true,
		ReclaimReachesCLI: true, LimitFloor: true}
}

type unitKind int

const (
	kAdmitted unitKind = iota
	kUndeclared
	kService
)

type unitState int

const (
	sPending unitState = iota
	sGated
	sRunning
	sDone
	sKilled
)

// simUnit is one part of a thread's work: an admitted job, a part of
// undeclared work, or a service.
type simUnit struct {
	Name, Thread, Class string
	Kind                unitKind
	Background          bool
	At                  time.Duration
	CPU                 float64 // cores it can use
	Base, Peak          float64 // bytes at the start and once ramped
	Ramp                float64 // share of the work over which memory ramps (0: 0.3)
	File                float64 // share of its memory that is page cache
	Leak                float64 // bytes a second past Peak while it runs
	LeakTo              float64
	Work                float64 // core-seconds; 0 for a service

	pid             int
	state           unitState
	started, ended  time.Duration
	gatedAt, waited time.Duration
	lastTry         time.Duration
	tool            string
	frozen          bool
	frozenAt        time.Duration
	freezes, thaws  int
	done, ran       float64
	ws, wsPrev      float64
	res             float64
	cli             float64 // bytes charged to the thread's cli/ (undeclared work moved after it allocated)
	moved           bool
	high            float64
	allowed, target float64
	faults          float64
	scan            float64
	used            float64 // cores used last step
	active          time.Duration
	peakSeen        float64
	catching        bool
	m               float64
	lastReclaim     time.Duration
	reclaimed       bool
	hit             bool // its memory.high was hit (V0.job-limits)
	turns           bool // its thread's parts take turns: undeclared/'s limit is lifted (V0.guard-tidy)
}

func (u *simUnit) live() bool { return u.state == sRunning }

func (u *simUnit) heavy() bool { return u.Kind != kService }

// want is the memory the part wants now: Base ramping to Peak over the first
// Ramp of its work (a service over its first minute), then the leak.
func (u *simUnit) want() float64 {
	ramp := u.Ramp
	if ramp == 0 {
		ramp = 0.3
	}
	f := 1.0
	if u.Work > 0 {
		f = min(1, u.done/(ramp*u.Work))
	} else {
		f = min(1, u.ran/60)
	}
	w := u.Base + (u.Peak-u.Base)*f
	if u.Leak > 0 && f >= 1 {
		w = min(max(u.LeakTo, u.Peak), w+u.Leak*u.ran)
	}
	return w
}

// slow is decision 0032's slowdown of a part that keeps phi of its memory.
func slow(phi float64) float64 {
	if phi >= 1 {
		return 1
	}
	return 1 + 51.6*math.Pow(1-max(0, phi), 1.44)
}

// stalled is the share of its time a part m times slower counts as a full
// memory stall: 1 - m^-0.5, so the 18 Nov incident (9.7 GB of work in a
// 9 GB cgroup with swap room, phi about 0.9) reads about 40 %, near the 53 %
// decision 0032 saw, and only a part at under a third of its memory reaches
// oomd's 85 %. A part's own time stalled is 1 - 1/m; PSI full counts only
// the time every task waits, which is less.
func stalled(m float64) float64 { return 1 - 1/math.Sqrt(m) }

// simFlag is the detector's flag of a thread's undeclared work (V0.164:
// 8 % of the agents' memory or a quarter of their cores for 45 s).
type simFlag struct {
	overSince time.Duration
	over      bool
	flagged   bool
	since     time.Duration
}

// simHost is the modelled host.
type simHost struct {
	p        simParams
	t        time.Duration
	units    []*simUnit
	threads  map[string]bool // sim threads whose agent runs (a CLI's memory)
	desktop  float64
	learned  map[string]cgroup.Learned
	flags    map[string]*simFlag
	holds    map[string]*simHold // the job tracker's holds, by sim thread
	nextPID  int
	lifted   bool
	livelock bool

	agentsInst, agentsAvg, hostAvg float64
	inst2                          []float64 // the last two seconds, for the PSI trigger
	agentsRes, swapUsed, cpuSlow   float64
	frozenFile                     float64
	oomdOver                       time.Duration // since when over the limit, -1 when not
	victimFrozen                   bool          // oomd's last victim had frozen work
	lastScan                       time.Duration

	// metrics
	critSecs  int
	peakSwap  float64
	kills     []simKill
	readBack  float64
	livelockS int
}

type simHold struct {
	mem0   float64
	active time.Duration
	unit   *simUnit
}

type simKill struct {
	at        time.Duration
	thread    string
	guardable bool
}

func newSimHost(p simParams) *simHost {
	return &simHost{p: p, threads: map[string]bool{}, learned: map[string]cgroup.Learned{}, flags: map[string]*simFlag{},
		holds: map[string]*simHold{}, oomdOver: -1, nextPID: 1000, cpuSlow: 1}
}

func (h *simHost) add(u *simUnit) {
	h.nextPID++
	u.pid = h.nextPID
	h.units = append(h.units, u)
}

func (h *simHost) start(u *simUnit) {
	u.state, u.started, u.active = sRunning, h.t, h.t
	if u.Kind == kAdmitted {
		h.holds[u.Thread] = &simHold{mem0: 0, active: h.t, unit: u}
		if h.p.JobLimits {
			u.high = h.limitFor(u.Class, false)
		}
	}
}

// limitFor is V0.job-limits's memory.high for a part of class (V0.joblimit
// sizeFor and jobLimits), 0 for none; undeclared: the guard moved it.
func (h *simHost) limitFor(class string, undeclared bool) float64 {
	l, ok := h.learned[class]
	if ok && l.Peak > 0 {
		peak := float64(l.Peak)
		if l.Runs < resource.LearnRuns {
			peak = max(peak, h.p.Budget/2)
		}
		if m := 1.25 * peak; m < h.p.Budget {
			return m
		}
		return 0
	}
	if undeclared && h.p.LimitFloor {
		return 0
	}
	free := max(0, h.p.Budget-h.agentsRes)
	m := min(free, 0.5*h.p.Budget)
	if h.p.LimitFloor {
		m = max(m, h.p.Budget/2) // the prior of 2 slots
	}
	return max(m, 4096)
}

func (h *simHost) threadUnits(th string, kind unitKind) []*simUnit {
	var out []*simUnit
	for _, u := range h.units {
		if u.Thread == th && u.Kind == kind && u.live() {
			out = append(out, u)
		}
	}
	return out
}

func (h *simHost) threadMem(th string) float64 {
	m := 0.0
	if h.threads[th] {
		m = h.p.CLI
	}
	for _, u := range h.units {
		if u.Thread == th && u.live() {
			m += u.res
		}
	}
	return m
}

// freeze stops units; move is undeclared work moved into undeclared/ (what
// it allocated before stays charged to cli/, and V0.job-limits sizes it now).
func (h *simHost) freeze(us []*simUnit, move bool) {
	for _, u := range us {
		if !u.frozen {
			u.frozen, u.frozenAt = true, h.t
			u.freezes++
		}
		if move && !u.moved {
			u.moved, u.cli = true, u.res
			if h.p.JobLimits {
				u.high = h.limitFor("", true) // an undeclared command's class is not known
			}
		}
	}
}

func (h *simHost) thaw(us []*simUnit) {
	for _, u := range us {
		if u.frozen {
			u.frozen = false
			u.thaws++
		}
	}
}

// reclaim is V0.guard-swap: the frozen parts' anonymous memory goes to swap,
// within 90 % of the cap, at most once per 30 s a thread, and not near oomd's
// limit (V0.guard-tidy).
func (h *simHost) reclaim(th string) {
	if h.p.OomdLimit > 0 && h.agentsAvg*100 >= h.p.OomdLimit-resource.OomdNearMargin {
		return
	}
	for _, u := range h.units {
		if u.Thread != th || !u.live() || !u.frozen || (u.reclaimed && h.t-u.lastReclaim < 30*time.Second) {
			continue
		}
		u.reclaimed, u.lastReclaim = true, h.t
		reach := u.res
		if u.Kind == kUndeclared && !h.p.ReclaimReachesCLI {
			reach = max(0, u.res-u.cli)
		}
		room := max(0, 0.9*h.p.SwapCap-h.swapUsed)
		anon := reach * (1 - u.File)
		take := min(anon, room)
		if take < 64e6 {
			continue
		}
		moved := take / (1 - u.File + 1e-9)
		moved = min(moved, u.res)
		u.res -= moved
		u.scan += moved
		h.swapUsed += take
	}
}

// step moves the host one second.
func (h *simHost) step() {
	const dt = 1.0
	h.t += time.Second
	nCLI := 0
	for range h.threads {
		nCLI++
	}
	avail := min(h.p.Budget, h.p.RAM-h.p.HostBase-h.desktop) - h.p.CLI*float64(nCLI)
	h.lifted = h.swapUsed >= 0.9*h.p.SwapCap
	var frozenRes, runWant, frozenSwap float64
	var run, frz []*simUnit
	for _, u := range h.units {
		if !u.live() {
			continue
		}
		u.scan *= 0.8 // pgscan "lately"
		if u.frozen {
			frz = append(frz, u)
			frozenRes += u.res
			frozenSwap += (u.ws - u.res) * (1 - u.File)
			continue
		}
		u.wsPrev = u.ws
		u.ws = u.want()
		u.peakSeen = max(u.peakSeen, u.ws)
		u.allowed = u.ws
		// V0.sim-fixes (2): while the part's own stall is critical its limit
		// lifts, and its use × 1.25 is the least it comes back at.
		ownStall := u.m > 1 && stalled(u.m)*100 >= cgroup.OwnStallLift
		if h.p.JobLimits && u.high > 0 && !h.lifted && !u.turns && ownStall {
			u.high = max(u.high, 1.25*(u.res-u.cli))
		}
		if h.p.JobLimits && u.high > 0 && !h.lifted && !u.turns && !ownStall {
			u.allowed = min(u.ws, u.cli+u.high)
			// V0.job-limits: a job that hits its memory.high raises its
			// class's peak by 1.2 once. V0.sim-fixes (2): and the resize (every
			// 10 s) raises its limit to its use × 1.25 at each hit, none at
			// or past the budget.
			if u.ws > u.allowed && h.t%(10*time.Second) == 0 {
				if !u.hit && u.Kind == kAdmitted && u.Class != "" {
					if l, ok := h.learned[u.Class]; ok {
						l.Peak = uint64(float64(l.Peak) * 1.2)
						h.learned[u.Class] = l
					}
				}
				u.hit = true
				u.high = max(u.high, 1.25*(u.allowed-u.cli))
				if u.high >= h.p.Budget {
					u.high = 0
				}
				u.allowed = u.ws
				if u.high > 0 {
					u.allowed = min(u.ws, u.cli+u.high)
				}
			}
		}
		run = append(run, u)
		runWant += u.allowed
	}
	// The kernel moves cold (frozen) pages to swap first.
	if over := runWant + frozenRes - avail; over > 0 && frozenRes > 0 {
		take := min(over, h.p.ColdReclaim*dt, frozenRes)
		room := max(0, h.p.SwapCap-h.swapUsed)
		for _, u := range frz {
			x := take * u.res / frozenRes
			x = min(x, room/(1-u.File+1e-9))
			u.res -= x
			u.scan += x
			room -= x * (1 - u.File)
		}
	}
	frozenRes, frozenSwap, h.frozenFile = 0, 0, 0
	for _, u := range frz {
		frozenRes += u.res
		frozenSwap += (u.ws - u.res) * (1 - u.File)
		h.frozenFile += u.res * u.File
	}
	space := avail - frozenRes
	phi := 1.0
	if runWant > space {
		phi = max(0, space) / runWant
	}
	need := frozenSwap
	for _, u := range run {
		u.target = phi * u.allowed
		need += (u.ws - u.target) * (1 - u.File)
	}
	h.livelock = need > h.p.SwapCap+1e6 && len(run) > 0
	// Pages in: fresh allocations at once, read-back shared.
	var readers int
	for _, u := range run {
		if u.res > u.target {
			x := min(u.res-u.target, h.p.Evict*dt)
			u.scan += x
			u.res -= x
			continue
		}
		fresh := max(0, u.ws-u.wsPrev)
		u.res = min(u.target, u.res+fresh)
		if u.target-u.res > 1e6 {
			readers++
		}
	}
	for _, u := range run {
		u.catching = false
		if gap := u.target - u.res; gap > 1e6 {
			r := min(gap, h.p.ReadBack*dt/float64(readers))
			u.res += r
			u.faults += r
			h.readBack += r
			u.catching = u.target-u.res > 1e6
		}
		if phi < 1 { // a thrashing part keeps reading its own pages back
			u.faults += (1 - phi) * h.p.ReadBack * 0.5 * dt / float64(len(run))
		}
	}
	// CPU and progress.
	demand := 0.0
	for _, u := range run {
		demand += u.CPU
	}
	share := 1.0
	if demand > h.p.Cores {
		share = h.p.Cores / demand
	}
	h.cpuSlow = max(1, demand/h.p.Cores)
	var wsum, ssum float64
	for _, u := range run {
		g := u.CPU * share
		u.m = slow(u.target / max(u.ws, 1))
		rate := g / u.m
		st := stalled(u.m)
		if u.catching {
			rate *= 0.25
			st = max(st, 0.5)
		}
		if h.livelock {
			rate, st = 0, 1
		}
		u.used = rate
		if rate >= 0.25 {
			u.active = h.t
			if hd := h.holds[u.Thread]; hd != nil && hd.unit == u {
				hd.active = h.t
			}
		}
		u.done += rate * dt
		u.ran += dt
		wsum += g
		ssum += g * st
	}
	for _, u := range frz {
		u.used = 0
	}
	h.agentsInst = 0
	if wsum > 0 {
		h.agentsInst = ssum / wsum
	}
	a := 1 - math.Exp(-dt/10)
	h.agentsAvg += a * (h.agentsInst - h.agentsAvg)
	h.inst2 = append(h.inst2, h.agentsInst)
	if len(h.inst2) > 2 {
		h.inst2 = h.inst2[1:]
	}
	// Memory now.
	h.agentsRes = h.p.CLI * float64(nCLI)
	h.swapUsed = 0
	for _, u := range h.units {
		if u.live() {
			h.agentsRes += u.res
			h.swapUsed += max(0, (u.ws-u.res)*(1-u.File))
		}
	}
	used := h.desktop + h.p.HostBase + h.agentsRes
	desk := 0.3 * min(1, max(0, (used/h.p.RAM-0.95)/0.05))
	h.hostAvg += a * (0.6*h.agentsInst + desk - h.hostAvg)
	// Ends.
	for _, u := range h.units {
		if u.live() && u.Work > 0 && u.done >= u.Work {
			u.state, u.ended, u.res, u.frozen = sDone, h.t, 0, false
			h.learn(u)
		}
	}
	// Metrics.
	if h.agentsAvg >= 0.10 {
		h.critSecs++
	}
	if h.livelock {
		h.livelockS++
	}
	h.peakSwap = max(h.peakSwap, h.swapUsed)
	h.detect()
}

// learn is the job tracker's class table after a run (cgroup.Jobs.learn).
func (h *simHost) learn(u *simUnit) {
	if u.Kind != kAdmitted || u.Class == "" {
		return
	}
	l := h.learned[u.Class]
	l.Runs++
	l.Demand = u.CPU
	l.Peak = max(l.Peak, uint64(u.peakSeen))
	l.Walls = append(l.Walls, (u.ended - u.started).Seconds())
	if len(l.Walls) > 10 {
		l.Walls = l.Walls[1:]
	}
	h.learned[u.Class] = l
}

// detect is the undeclared-work detector (V0.164): a thread whose undeclared
// parts hold 8 % of the budget or use a quarter of the cores for 45 s is
// flagged until they end.
func (h *simHost) detect() {
	seen := map[string]bool{}
	for _, u := range h.units {
		if u.Kind == kUndeclared && u.live() {
			seen[u.Thread] = true
		}
	}
	for th := range h.flags {
		if !seen[th] {
			delete(h.flags, th)
		}
	}
	for th := range seen {
		f := h.flags[th]
		if f == nil {
			f = &simFlag{overSince: -1}
			h.flags[th] = f
		}
		mem, cpu := h.undMem(th)
		over := mem >= 0.08*h.p.Budget || cpu >= 0.25*h.p.Cores
		switch {
		case f.flagged:
		case over && f.overSince < 0:
			f.overSince = h.t
		case over && h.t-f.overSince >= 45*time.Second:
			f.flagged, f.since = true, f.overSince
		case !over:
			f.overSince = -1
		}
	}
}

func (h *simHost) undMem(th string) (mem, cpu float64) {
	mem = h.p.CLI
	for _, u := range h.threadUnits(th, kUndeclared) {
		mem += u.res
		cpu += u.used
	}
	return mem, cpu
}

// oomd is systemd-oomd on the agents' slice: over its limit for OomdFor, with
// pages scanned lately, it kills the thread with the most (memory.oom.group).
// It returns the killed thread, "" for none.
func (h *simHost) oomd() string {
	if h.p.OomdLimit <= 0 || h.agentsAvg*100 <= h.p.OomdLimit {
		h.oomdOver = -1
		return ""
	}
	if h.oomdOver < 0 {
		h.oomdOver = h.t
	}
	if h.t-h.oomdOver < h.p.OomdFor {
		return ""
	}
	scan := map[string]float64{}
	for _, u := range h.units {
		if u.live() {
			scan[u.Thread] += u.scan + u.res*1e-6 // memory breaks a tie
		}
	}
	victim, best := "", 0.0
	for th, s := range scan {
		if s > best || (s == best && th < victim) {
			victim, best = th, s
		}
	}
	if victim == "" {
		return ""
	}
	h.victimFrozen = false
	for _, u := range h.units {
		if u.Thread == victim && u.live() && u.frozen {
			h.victimFrozen = true
		}
	}
	for _, u := range h.units {
		if u.Thread == victim && (u.live() || u.state == sGated || u.state == sPending) {
			u.state, u.ended, u.res, u.frozen = sKilled, h.t, 0, false
		}
	}
	delete(h.threads, victim)
	h.oomdOver = -1
	h.agentsAvg *= 0.5 // the kill frees the memory; the average follows
	return victim
}

// signals is what cgroup.Sampler.Signals would read on this host.
func (h *simHost) signals() resource.Signals {
	hostAvail := h.p.RAM - h.p.HostBase - h.desktop - h.agentsRes
	ws := h.agentsRes - h.frozenFile
	hr := min(hostAvail/h.p.Budget, (h.p.Budget-ws)/h.p.Budget)
	hr = float64(int(min(1, max(0, hr))*1000+0.5)) / 1000
	s := h.cpuSlow
	unit := h.agentsAvg * 100
	return resource.Signals{Provider: "cgroups", Slowdown: &s, Headroom: &hr, Stall: level(h.hostAvg), AgentsStall: level(h.agentsAvg),
		CPU: h.p.Cores, Memory: uint64(h.p.Budget), SwapFull: h.swapUsed >= cgroup.SwapFullShare*h.p.SwapCap,
		AgentsMem: uint64(h.agentsRes), UnitStall: &unit, OomdLimit: h.p.OomdLimit}
}

func level(avg float64) resource.Stall {
	switch {
	case avg*100 >= cgroup.StallCritical:
		return resource.StallCritical
	case avg*100 >= cgroup.StallElevated:
		return resource.StallElevated
	}
	return resource.StallOK
}

// psiTrigger is fyrd's trigger: 200 ms of full stall in 2 s, the critical
// level (V0.freeze-fast-class-name (3); V0.guard-wake set it at 150 ms).
func (h *simHost) psiTrigger() bool {
	s := 0.0
	for _, v := range h.inst2 {
		s += v
	}
	return len(h.inst2) == 2 && s/2 >= cgroup.StallCritical/100
}
