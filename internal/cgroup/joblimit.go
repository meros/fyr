package cgroup

// V0.job-limits (decision 0036 (C)): a job sizes itself. When fyrd moves a job
// into job/ or undeclared/, the leaf gets a memory.high from its class's
// learned peak and a cpu.max from its learned demand. The guard's policy
// decides the values (guard.JobLimit, V0.guard-finish); this file reads what
// it decides from and writes them. Node sizes its heap from
// the smaller of memory.max and memory.high of its own cgroup; Go 1.25's
// GOMAXPROCS (and so `go test -p`) and Jest follow cpu.max. There is no
// memory.max, so the limit never kills. memory.high with no swap room
// livelocks, so every job limit lifts while the agents' swap is nearly full
// (the valve of decision 0032 slice 3). Each limit is logged (`guard: job
// limit ...`); a job that hits its memory.high raises its class's peak.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/meros/fyr/internal/guard/resource"
)

// sizedJob is what was written to one job cgroup, under JobGroups.mu.
type sizedJob struct {
	thread, class string
	undeclared    bool
	size          resource.JobSize
	high, cpu     string
	events        uint64 // memory.events high when the limit was set, or at its last hit
	hit           bool
	// raised is the least memory.high the job's hits and stalls have earned:
	// its use × resource.HoldMargin at its last hit or while it stalled (V0.sim-fixes).
	raised uint64
	// V0.joblimit-slice: the limits of a class not learned yet, sized once
	// when the job was moved; only the swap valve changes them after.
	kept *resource.Limit
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
// caller, when they changed; the first call of a job asks its class. The
// policy (Limit) decides them from what is read here.
func (g *JobGroups) sizeJob(thread, dir string, undeclared bool) {
	if g == nil || g.Tree == nil || g.Size == nil || g.Limit == nil {
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
	g.mu.Lock()
	turns := g.turns[dir]
	g.mu.Unlock()
	ask := resource.LimitAsk{Size: s.size, N: g.N, Budget: budget, Used: used, Cores: cores, Lifted: lifted,
		Undeclared: undeclared, First: !known, Kept: s.kept, Raised: s.raised, Turns: turns, Page: uint64(os.Getpagesize())}
	// V0.sim-fixes (2): the job's own memory full stall, at which its limit lifts.
	if p, ok := readPSI(filepath.Join(dir, "memory.pressure")); ok && p.hasFull && p.full >= OwnStallLift {
		ask.Stalled, ask.Current = true, readBytes(filepath.Join(dir, "memory.current"))
	}
	lim := g.Limit(ask)
	if lim.Keep != nil {
		s.kept = lim.Keep
	}
	s.raised = lim.Raised
	high, cpu, why := lim.High, lim.CPU, lim.Why
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

// LimitSettled is whether a `guard: job limit` decision rests on a class
// learned from resource.LearnRuns runs (V0.limit-raised-settled): its learned
// limit, and a raise of it, are not sized below the prior.
func LimitSettled(why string, runs int) bool {
	return (why == "learned" || why == "raised") && runs >= resource.LearnRuns
}

// OwnStallLift is the job's own memory full stall (avg10, percent) at which
// its memory.high lifts (V0.sim-fixes (2)): the job throttles itself into the
// stall V0.job-limits meant to avoid. StallCritical, as for the agents.
const OwnStallLift = StallCritical

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
// job's own limit to its use × resource.HoldMargin at each hit (V0.sim-fixes (2)).
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
			cur.raised = max(cur.raised, uint64(resource.HoldMargin*float64(use)))
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
	goMemShare     = resource.UnknownShare // GOMEMLIMIT: the most an unknown job's memory.high allows
	workerCPUShare = 0.5                   // pytest-xdist, Playwright, turbo: half the agents' cores
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
