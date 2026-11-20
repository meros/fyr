package cgroup

// V0.job-limits (decision 0036 (C)): a job sizes itself. When fyrd moves a job
// into job/ or undeclared/, the leaf gets a memory.high from its class's
// learned peak and a cpu.max from its learned demand. Node sizes its heap from
// the smaller of memory.max and memory.high of its own cgroup; Go 1.25's
// GOMAXPROCS (and so `go test -p`) and Jest follow cpu.max. There is no
// memory.max, so the limit never kills. memory.high with no swap room
// livelocks, so every job limit lifts while the agents' swap is nearly full
// (the valve of decision 0032 slice 3). Each limit is logged (`guard: job
// limit ...`); a job that hits its memory.high raises its class's peak.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/meros/fyr/internal/slots"
)

// JobSize is what a job's class has learned; Known is false before its first
// run (or with no class). Runs is how many runs it is learned from.
type JobSize struct {
	Class  string
	Peak   uint64  // bytes
	Demand float64 // cores
	Known  bool
	Runs   int
}

// sizeFor is z as a job is sized from it: under slots.LearnRuns runs a
// class's peak and demand are never below the prior's (1/N of budget and
// cores), as admission counts them (slots.NeedOf; decision 0029 section 2).
// V0.admission-memory: a first run its memory.high held back taught a low
// peak, and the next limit, sized from it, held the next run lower still.
// Without N (the host's slots setting) z is as learned.
func (g *JobGroups) sizeFor(z JobSize, budget uint64, cores float64) JobSize {
	if !z.Known || z.Runs >= slots.LearnRuns || g.N <= 0 {
		return z
	}
	p := slots.Prior(slots.Signals{CPU: cores, Memory: budget}, g.N)
	z.Peak, z.Demand = max(z.Peak, p.Mem), max(z.Demand, p.CPU)
	return z
}

// sizedJob is what was written to one job cgroup, under JobGroups.mu.
type sizedJob struct {
	thread, class string
	undeclared    bool
	size          JobSize
	high, cpu     string
	events        uint64 // memory.events high when the limit was set, or at its last hit
	hit           bool
	// raised is the least memory.high the job's hits and stalls have earned:
	// its use × holdMargin at its last hit or while it stalled (V0.sim-fixes).
	raised uint64
	// V0.joblimit-slice: the limits of a class not learned yet, sized once
	// when the job was moved; only the swap valve changes them after.
	kept                       bool
	keptHigh, keptCPU, keptWhy string
}

// unknownShare caps an unknown class's memory.high at this share of the
// agents' memory budget.
const unknownShare = 0.5

// cpuPeriod is the cpu.max period fyrd writes.
const cpuPeriod = 100000

// jobLimits are the memory.high and cpu.max for a job of size z: why says
// where they come from. budget and used are the agents' memory budget and
// use now; cores the agents' CPU; lifted is the swap valve.
func jobLimits(z JobSize, budget, used uint64, cores float64, lifted bool) (high, cpu, why string) {
	if lifted {
		return "max", "max", "swap-full"
	}
	page := uint64(os.Getpagesize())
	var mem uint64
	if z.Known && z.Peak > 0 {
		mem, why = uint64(holdMargin*float64(z.Peak)), "learned"
	} else if budget > 0 {
		free := uint64(0)
		if budget > used {
			free = budget - used
		}
		mem, why = min(free, uint64(unknownShare*float64(budget))), "unknown"
	}
	high = "max"
	if mem > 0 && (budget == 0 || mem < budget) {
		high = strconv.FormatUint(max(mem/page*page, page), 10)
	}
	cpu = "max"
	if z.Known && z.Demand > 0 {
		// The margin of the memory limit, so a job its cap held back can show
		// more demand next run: a cap at the demand itself would only ratchet down.
		c := max(1, math.Ceil(holdMargin*z.Demand))
		if cores <= 0 || c < cores {
			cpu = fmt.Sprintf("%d %d", int(c)*cpuPeriod, cpuPeriod)
		}
	}
	if why == "" {
		why = "no-budget"
	}
	return high, cpu, why
}

// unknownHigh is the memory.high kept for a job of a class not learned yet,
// given high, the agents' free memory at the move (jobLimits). V0.freeze-charge:
// on 19 Nov at 20:48 the guard moved work into undeclared/ while 73 MB were
// free, and that became its memory.high: after the thaw every allocation was
// throttled. Undeclared work is moved only by the guard, to freeze it, so
// always when memory is short and the free memory least: it gets no limit
// (the freeze bounds it, and after the thaw the guard's next tick does). Its
// pages stay charged to cli/ besides,
// so a limit there would count only what it allocates after the thaw. A job
// moved into job/ is never limited below the prior, 1/N of the agents, as
// admission counts it (decision 0029 section 2).
func (g *JobGroups) unknownHigh(high string, budget uint64, cores float64, undeclared bool) (string, string) {
	if undeclared {
		return "max", "unknown-frozen"
	}
	if g.N <= 0 || budget == 0 || high == "max" {
		return high, "unknown"
	}
	p := slots.Prior(slots.Signals{CPU: cores, Memory: budget}, g.N).Mem
	if n, err := strconv.ParseUint(high, 10, 64); err == nil && n >= p {
		return high, "unknown"
	}
	if p >= budget {
		return "max", "prior"
	}
	page := uint64(os.Getpagesize())
	return strconv.FormatUint(max(p/page*page, page), 10), "prior"
}

// agentsNow is the agents' memory budget, its use, its cores and whether the
// swap valve is open.
func (g *JobGroups) agentsNow() (budget, used uint64, cores float64, lifted bool) {
	agents := g.Tree.agentsDir() // fyr-agents.slice in the scope layout (V0.joblimit-slice)
	n := g.Cores
	if n <= 0 {
		n = runtime.NumCPU()
	}
	c := capacity(g.Tree, g.proc(), n, 0)
	return c.Memory, readBytes(filepath.Join(agents, "memory.current")), c.CPU, swapFull(agents)
}

// sizeJob writes the limits of the job cgroup dir of thread, made by the
// caller, when they changed; the first call of a job asks its class.
func (g *JobGroups) sizeJob(thread, dir string, undeclared bool) {
	if g == nil || g.Tree == nil || g.Size == nil {
		return
	}
	g.mu.Lock()
	s, known := g.sized[dir]
	g.mu.Unlock()
	if !known {
		s = sizedJob{thread: thread, undeclared: undeclared, size: g.Size(thread, undeclared)}
		s.class = s.size.Class
		s.events, _ = readKey(filepath.Join(dir, "memory.events"), "high")
	}
	budget, used, cores, lifted := g.agentsNow()
	z := g.sizeFor(s.size, budget, cores) // V0.admission-memory: the 3-run prior
	if !known {
		// V0.joblimit-slice: a class not learned yet is sized from the agents'
		// free memory once, here. used counts the job itself, so sizing it again
		// each tick would shrink its limit as it grows.
		if h, c, w := jobLimits(z, budget, used, cores, false); w != "learned" {
			if w == "unknown" {
				h, w = g.unknownHigh(h, budget, cores, undeclared)
			}
			s.kept, s.keptHigh, s.keptCPU, s.keptWhy = true, h, c, w
		}
	}
	var high, cpu, why string
	if s.kept && !lifted {
		high, cpu, why = s.keptHigh, s.keptCPU, s.keptWhy
	} else {
		high, cpu, why = jobLimits(z, budget, used, cores, lifted)
	}
	if !lifted && high != "max" {
		high, why = s.grown(dir, high, why, budget)
	}
	g.mu.Lock()
	turns := g.turns[dir]
	g.mu.Unlock()
	if turns && high != "max" {
		high, why = "max", "turns" // V0.guard-tidy (liftForTurns)
	}
	if known && high == s.high && cpu == s.cpu {
		g.mu.Lock()
		if cur, ok := g.sized[dir]; ok && s.raised > cur.raised {
			cur.raised = s.raised // grown keeps what a stall earned
			g.sized[dir] = cur
		}
		g.mu.Unlock()
		return
	}
	var bad []string
	for f, v := range map[string]string{"memory.high": high, "cpu.max": cpu} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil && v == "max" {
			continue // a cgroup without the controller (or a test) has nothing to lift
		}
		// A memory.high below what the cgroup holds makes the writer reclaim
		// down to it before the write returns: that waits off the guard's tick.
		if n, err := strconv.ParseUint(v, 10, 64); err == nil && f == "memory.high" && readBytes(filepath.Join(dir, "memory.current")) > n {
			go func(d, f, v string) {
				if err := put(d, f, v); err != nil {
					g.Tree.logf("guard: job limit could not set %s/%s: %v", d, f, err)
				}
			}(dir, f, v)
			continue
		}
		if err := put(dir, f, v); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", f, err))
		}
	}
	was := "none"
	if known {
		was = s.high + "/" + s.cpu
	}
	s.high, s.cpu = high, cpu
	g.mu.Lock()
	if g.sized == nil {
		g.sized = map[string]sizedJob{}
	}
	if cur, ok := g.sized[dir]; ok {
		s.raised = max(s.raised, cur.raised) // resize may have raised it since
	}
	g.sized[dir] = s
	g.mu.Unlock()
	kind := jobDir
	if undeclared {
		kind = undeclaredDir
	}
	if len(bad) > 0 {
		g.Tree.logf("guard: job limit could not set thread=%s cgroup=%s class=%q: %s", thread, kind, s.class, strings.Join(bad, "; "))
		return
	}
	g.Tree.logf("guard: job limit thread=%s cgroup=%s class=%q high=%s cpu=%s why=%s learned_peak=%s learned_demand=%.2f agents_mem=%s/%s was=%s",
		thread, kind, s.class, mbOr(high), cpuOr(cpu), why, inMB(s.size.Peak), s.size.Demand, inMB(used), inMB(budget), was)
	if g.Limited != nil { // V0.load-invariants
		g.Limited(thread, s.class, why, high, budget, s.size.Runs)
	}
}

// OwnStallLift is the job's own memory full stall (avg10, percent) at which
// its memory.high lifts (V0.sim-fixes (2)): the job throttles itself into the
// stall V0.job-limits meant to avoid. StallCritical, as for the agents.
const OwnStallLift = StallCritical

// grown is memory.high as the job's hits and its own stall leave it
// (V0.sim-fixes (2)). The load simulation (V0.load-sim) ran a class learned at
// 1.5 GB whose jobs grew to 7 GB: one ×1.2 on the class left each job a third
// of its memory, about 30× slower, holding the agents' stall critical.
//   - A hit raises the limit to the job's use × holdMargin (resize), again at
//     each hit; at or past the agents' budget there is no limit.
//   - While the job's own memory full stall is OwnStallLift or more, the limit
//     lifts; its use × holdMargin is kept as the least it comes back at.
//
// s.raised is updated here when the job stalls; the caller stores s.
func (s *sizedJob) grown(dir, high, why string, budget uint64) (string, string) {
	if p, ok := readPSI(filepath.Join(dir, "memory.pressure")); ok && p.hasFull && p.full >= OwnStallLift {
		s.raised = max(s.raised, uint64(holdMargin*float64(readBytes(filepath.Join(dir, "memory.current")))))
		return "max", "own-stall"
	}
	n, err := strconv.ParseUint(high, 10, 64)
	if err != nil || s.raised <= n {
		return high, why
	}
	if budget > 0 && s.raised >= budget {
		return "max", "raised"
	}
	page := uint64(os.Getpagesize())
	return strconv.FormatUint(max(s.raised/page*page, page), 10), "raised"
}

func mbOr(v string) string {
	if n, err := strconv.ParseUint(v, 10, 64); err == nil {
		return inMB(n)
	}
	return v
}

func cpuOr(v string) string {
	if c := strings.Fields(v); len(c) == 2 {
		q, _ := strconv.ParseFloat(c[0], 64)
		p, _ := strconv.ParseFloat(c[1], 64)
		if p > 0 {
			return fmt.Sprintf("%gcores", q/p)
		}
	}
	return v
}

// resize follows the swap valve for every sized job and notes a job that hit
// its memory.high: its class's learned peak is raised once per job, and the
// job's own limit to its use × holdMargin at each hit (V0.sim-fixes (2)).
func (g *JobGroups) resize() {
	if g == nil || g.Tree == nil {
		return
	}
	g.mu.Lock()
	dirs := make(map[string]sizedJob, len(g.sized))
	for d, s := range g.sized {
		dirs[d] = s
	}
	g.mu.Unlock()
	for d, s := range dirs {
		if _, err := os.Stat(d); err != nil {
			g.forgetSize(d)
			continue
		}
		g.sizeJob(s.thread, d, s.undeclared)
		g.mu.Lock()
		s = g.sized[d]
		g.mu.Unlock()
		if s.high == "max" {
			continue
		}
		ev, ok := readKey(filepath.Join(d, "memory.events"), "high")
		if !ok || ev <= s.events {
			continue
		}
		use := readBytes(filepath.Join(d, "memory.current"))
		first := !s.hit
		g.mu.Lock()
		if cur, ok := g.sized[d]; ok {
			cur.hit, cur.events = true, ev
			cur.raised = max(cur.raised, uint64(holdMargin*float64(use)))
			g.sized[d] = cur
		}
		g.mu.Unlock()
		g.Tree.logf("guard: job limit hit thread=%s class=%q high=%s mem=%s high_events=+%d learned_peak=%s",
			s.thread, s.class, mbOr(s.high), inMB(use), ev-s.events, inMB(s.size.Peak))
		if first && g.Raise != nil && s.class != "" {
			g.Raise(s.class)
		}
		g.sizeJob(s.thread, d, s.undeclared) // the raised limit, now
	}
}

// liftForTurns lifts the memory.high of thread's undeclared/ (dir) while its
// parts take turns (V0.guard-tidy). undeclared/ holds the frozen parts too, so
// its limit counted their memory against the one part that runs, and the
// work FreezeUndeclared sized was throttled by memory not its own. A rotation
// never sized undeclared/, so a cgroup it made first has no limit to lift.
// The limit comes back when the turns end (endTurns).
func (g *JobGroups) liftForTurns(thread, dir string) {
	g.mu.Lock()
	if g.turns == nil {
		g.turns = map[string]bool{}
	}
	was := g.turns[dir]
	g.turns[dir] = true
	_, sized := g.sized[dir]
	g.mu.Unlock()
	if !was && sized {
		g.sizeJob(thread, dir, true)
	}
}

// endTurns puts undeclared/'s own limit back once its parts no longer take turns.
func (g *JobGroups) endTurns(thread, dir string) {
	g.mu.Lock()
	was := g.turns[dir]
	delete(g.turns, dir)
	_, sized := g.sized[dir]
	g.mu.Unlock()
	if was && sized {
		g.sizeJob(thread, dir, true)
	}
}

// forgetSize drops what was written to dir (its job ended).
func (g *JobGroups) forgetSize(dir string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.sized, dir)
	delete(g.turns, dir)
	g.mu.Unlock()
}

// Shares of the agents' capacity for the tools that do not read cgroups
// (V0.job-limits): a Go process's soft memory target, and the workers of a test
// runner. Shares, not numbers, so they fit any host (decision 0029 section 0).
const (
	goMemShare     = unknownShare // GOMEMLIMIT: the most an unknown job's memory.high allows
	workerCPUShare = 0.5          // pytest-xdist, Playwright, turbo: half the agents' cores
)

// Workers is how many workers a parallel test runner should start on a host
// of capacity c: workerCPUShare of the agents' cores, at least 1.
func Workers(c Capacity) int {
	return max(1, int(c.CPU*workerCPUShare))
}

// JobEnv is the environment fyrd gives an agent CLI, so the tools its jobs run
// that ignore cgroups size themselves from the same shares: GOMEMLIMIT (Go
// reads cpu.max for GOMAXPROCS but not memory limits) and
// PYTEST_XDIST_AUTO_NUM_WORKERS (`pytest -n auto`). Playwright and turbo read
// no variable for their workers; the gate hook names their flags (WorkerHint).
func JobEnv(c Capacity) []string {
	var env []string
	if c.Memory > 0 {
		env = append(env, "GOMEMLIMIT="+strconv.FormatUint(uint64(goMemShare*float64(c.Memory))>>20, 10)+"MiB")
	}
	if c.CPU > 0 {
		env = append(env, "PYTEST_XDIST_AUTO_NUM_WORKERS="+strconv.Itoa(Workers(c)))
	}
	return env
}
