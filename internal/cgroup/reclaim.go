package cgroup

// V0.guard-swap: a freeze stops work but leaves its memory where it is, so on
// 19 Nov a frozen 4 GB dev stack still held its RAM inside agents/ while the
// others thrashed against memory.high. After a freeze Fyr asks the kernel to
// move the frozen work's anonymous memory to swap (memory.reclaim, anon only),
// within the room the agents' swap cap leaves, so the RAM goes to the work
// that runs. The work reads it back when it is thawed.

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// reclaimMargin is the share of the agents' swap cap a reclaim leaves free.
const reclaimMargin = 0.10

// reclaimEvery is how often one thread's frozen work is reclaimed again: a
// rotation re-applies its turn every tick, and the unit that just lost its
// turn is worth moving out once, not every ten seconds.
const reclaimEvery = 30 * time.Second

// reclaimMin is the least anonymous memory worth a reclaim.
const reclaimMin = 64 << 20

// frozenDirs are thread's cgroups frozen now: its job, its undeclared work
// and the units under it whose cgroup.freeze is 1.
func (g *JobGroups) frozenDirs(thread string) []string {
	var out []string
	frozen := func(d string) bool {
		b, err := os.ReadFile(filepath.Join(d, "cgroup.freeze"))
		return err == nil && strings.TrimSpace(string(b)) == "1"
	}
	if d := g.dir(thread); frozen(d) {
		out = append(out, d)
	}
	ud := g.udir(thread)
	if frozen(ud) {
		return append(out, ud) // hierarchical: its units are in it
	}
	for _, d := range unitDirs(ud) {
		if frozen(d) {
			out = append(out, d)
		}
	}
	return out
}

// swapRoom is how much more the agents may put in swap: their cap less the
// margin, or the host's free swap less the margin when they have no cap.
func (g *JobGroups) swapRoom() uint64 {
	agents := g.Tree.agentsDir()
	limit := readBytes(filepath.Join(agents, "memory.swap.max"))
	used := readBytes(filepath.Join(agents, "memory.swap.current"))
	if limit == 0 {
		total, free := swapInfo(g.proc())
		limit, used = total, total-free
	}
	keep := uint64(float64(limit) * (1 - reclaimMargin))
	if used >= keep {
		return 0
	}
	return keep - used
}

// swapInfo reads SwapTotal and SwapFree from /proc/meminfo, in bytes.
func swapInfo(proc string) (total, free uint64) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
		switch k {
		case "SwapTotal":
			total = n << 10
		case "SwapFree":
			free = n << 10
		}
	}
	return total, free
}

// ReclaimFrozen moves the anonymous memory of thread's frozen work to swap in
// the background, at most once per reclaimEvery per thread; nothing on a host
// without the tree (a stopped process keeps its memory there). It returns at
// once: memory.reclaim blocks for as long as the kernel works.
func (g *JobGroups) ReclaimFrozen(thread string) {
	if g == nil || g.Tree == nil {
		return
	}
	now := time.Now()
	g.mu.Lock()
	if g.reclaimed == nil {
		g.reclaimed = map[string]time.Time{}
	}
	if last, ok := g.reclaimed[thread]; ok && (last.IsZero() || now.Sub(last) < reclaimEvery) {
		g.mu.Unlock()
		return // running (zero) or done lately
	}
	g.reclaimed[thread] = time.Time{}
	g.mu.Unlock()
	go func() {
		moved := g.reclaimFrozen(thread)
		if moved > 0 && g.Tree.logf != nil {
			g.Tree.logf("cgroups: moved %s of %s's frozen work to swap", gib(moved), thread)
		}
		g.mu.Lock()
		g.reclaimed[thread] = time.Now()
		g.mu.Unlock()
	}()
}

// reclaimFrozen asks for each frozen cgroup's anonymous memory within the
// swap room, and reports what the cgroups gave up.
func (g *JobGroups) reclaimFrozen(thread string) uint64 {
	if g.nearOomd(thread) {
		return 0
	}
	var moved uint64
	for _, d := range g.frozenDirs(thread) {
		room := g.swapRoom()
		anon, _ := readKey(filepath.Join(d, "memory.stat"), "anon")
		want := min(anon, room)
		if want < reclaimMin {
			continue
		}
		before := readBytes(filepath.Join(d, "memory.current"))
		// swappiness=200 reclaims anonymous memory only, which leaves the
		// page cache the running work shares; a kernel before 6.13 refuses
		// the key. A partial reclaim answers EAGAIN, which is fine.
		v := strconv.FormatUint(want, 10)
		if err := reclaim(d, v+" swappiness=200"); err != nil && strings.Contains(err.Error(), "invalid argument") {
			reclaim(d, v)
		}
		if after := readBytes(filepath.Join(d, "memory.current")); after < before {
			moved += before - after
		}
	}
	return moved
}

// nearOomd says the unit oomd watches for the agents stalls within
// OomdNearMargin of oomd's limit, and logs it once a minute; false while the
// limit or the stall is unknown. V0.guard-tidy (decision 0036 "Not chosen"):
// a reclaim raises the cgroup's pgscan, so oomd would pick the work fyrd just
// reclaimed, and with memory.oom.group=1 on the thread's scope kill the
// paused thread whole. Away from the limit the reclaim stays (V0.guard-swap).
func (g *JobGroups) nearOomd(thread string) bool {
	limit := g.Tree.OomdLimit()
	if limit <= 0 {
		return false
	}
	stall, ok := g.Tree.unitStall()
	if !ok || stall < limit-OomdNearMargin {
		return false
	}
	now := time.Now()
	g.mu.Lock()
	quiet := now.Sub(g.reclaimSkipped) < time.Minute
	if !quiet {
		g.reclaimSkipped = now
	}
	g.mu.Unlock()
	if !quiet && g.Tree.logf != nil {
		g.Tree.logf("guard: no reclaim of %s's frozen work near oomd's limit: unit_full=%.0f%% limit=%.0f%%; a reclaim would make it oomd's first pick", thread, stall, limit)
	}
	return true
}

// reclaim writes memory.reclaim of d; it never creates the file, so a fake
// cgroupfs without one is left alone.
func reclaim(d, v string) error {
	f, err := os.OpenFile(filepath.Join(d, "memory.reclaim"), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString(v)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func gib(b uint64) string {
	return strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64) + " GiB"
}
