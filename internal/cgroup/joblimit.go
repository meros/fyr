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
	events        uint64 // memory.events high when the limit was set
	hit           bool
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

// agentsNow is the agents' memory budget, its use, its cores and whether the
// swap valve is open.
func (g *JobGroups) agentsNow() (budget, used uint64, cores float64, lifted bool) {
	agents := filepath.Join(g.Tree.root, "agents")
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
	high, cpu, why := jobLimits(g.sizeFor(s.size, budget, cores), budget, used, cores, lifted)
	if known && high == s.high && cpu == s.cpu {
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
// its memory.high: its class's learned peak is raised once per job.
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
		if s.hit || s.high == "max" {
			continue
		}
		ev, ok := readKey(filepath.Join(d, "memory.events"), "high")
		if !ok || ev <= s.events {
			continue
		}
		g.mu.Lock()
		if cur, ok := g.sized[d]; ok {
			cur.hit = true
			g.sized[d] = cur
		}
		g.mu.Unlock()
		g.Tree.logf("guard: job limit hit thread=%s class=%q high=%s mem=%s high_events=+%d learned_peak=%s",
			s.thread, s.class, mbOr(s.high), inMB(readBytes(filepath.Join(d, "memory.current"))), ev-s.events, inMB(s.size.Peak))
		if g.Raise != nil && s.class != "" {
			g.Raise(s.class)
		}
	}
}

// forgetSize drops what was written to dir (its job ended).
func (g *JobGroups) forgetSize(dir string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.sized, dir)
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
