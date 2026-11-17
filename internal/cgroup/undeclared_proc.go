package cgroup

// V0.164 sections 4.5 and 10: the processes behind an undeclared episode (the
// job's roots and their command), and, on a host without the cgroup tree, a
// thread's use read from the process tree under its CLI.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func pageSize() int { return os.Getpagesize() }

// clkTck is the kernel's USER_HZ (100 on Linux).
const clkTck = 100

// Root is the first process of a job in a thread's cgroup: new since the
// episode began, and started by something older (or outside the cgroup). The
// CLI, its MCP servers and anything else already there are residents.
type Root struct {
	PID     int
	Cmdline string
	RSS     uint64 // the whole subtree's resident memory, bytes
}

// procStat is what /proc/<pid>/stat gives that the readers use.
type procStat struct {
	ppid    int
	utime   uint64 // ticks, user + system
	started uint64 // ticks after boot
}

func readProcStat(proc string, pid int) (procStat, bool) {
	b, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return procStat{}, false
	}
	f := strings.Fields(s[i+1:])
	// f[0] is field 3 (state): ppid is f[1], utime and stime f[11] and f[12],
	// starttime f[19].
	if len(f) < 20 {
		return procStat{}, false
	}
	n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
	return procStat{ppid: int(n(1)), utime: n(11) + n(12), started: n(19)}, true
}

func readRSS(proc string, pid int) uint64 {
	b, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "statm"))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.ParseUint(f[1], 10, 64)
	return n * uint64(pageSize())
}

func readCmdline(proc string, pid int) string {
	b, _ := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "cmdline"))
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
}

func bootTime(proc string) (time.Time, bool) {
	b, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return time.Time{}, false
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(ln, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(n, 0), err == nil
		}
	}
	return time.Time{}, false
}

// rootGrace is how long before the first over reading a job may have started
// and still be the episode's.
const rootGrace = 15 * time.Second

// ReadRoots are the job roots of thread's cgroup for an episode whose first
// over reading was at after, in pid order, each with its subtree's memory.
// The processes moved into undeclared/ still count, so the command is known
// after a freeze.
func ReadRoots(tr *Tree, proc, thread string, after time.Time) []Root {
	if tr == nil {
		return nil
	}
	if proc == "" {
		proc = "/proc"
	}
	td := filepath.Join(tr.root, "agents", unsafe.ReplaceAllString(thread, "_"))
	in := append(pids(filepath.Join(td, "cgroup.procs")), pids(filepath.Join(td, undeclaredDir, "cgroup.procs"))...)
	boot, ok := bootTime(proc)
	if !ok || len(in) == 0 {
		return nil
	}
	cut := after.Add(-rootGrace)
	start := map[int]time.Time{}
	parent := map[int]int{}
	here := map[int]bool{}
	for _, p := range in {
		st, ok := readProcStat(proc, p)
		if !ok {
			continue
		}
		here[p] = true
		parent[p] = st.ppid
		start[p] = boot.Add(time.Duration(st.started) * time.Second / clkTck)
	}
	children := map[int][]int{}
	for p, pp := range parent {
		children[pp] = append(children[pp], p)
	}
	var roots []Root
	for p := range here {
		if start[p].Before(cut) {
			continue
		}
		if pp := parent[p]; here[pp] && !start[pp].Before(cut) {
			continue
		}
		var sum func(int) uint64
		sum = func(q int) uint64 {
			t := readRSS(proc, q)
			for _, c := range children[q] {
				t += sum(c)
			}
			return t
		}
		roots = append(roots, Root{PID: p, Cmdline: readCmdline(proc, p), RSS: sum(p)})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].PID < roots[j].PID })
	return roots
}

// Heaviest is the root whose subtree holds the most memory.
func Heaviest(roots []Root) (Root, bool) {
	var best Root
	ok := false
	for _, r := range roots {
		if !ok || r.RSS > best.RSS {
			best, ok = r, true
		}
	}
	return best, ok
}

// MaxCommand is the longest command text kept (V0.164 4.5).
const MaxCommand = 300

// CommandOf is the command behind a root: the newest Bash command the gate
// saw (seen is oldest first) whose text is found in the root's command line,
// else the command line itself; cut to MaxCommand characters. A command under
// four characters matches anything and is not used.
func CommandOf(r Root, seen []string) string {
	for i := len(seen) - 1; i >= 0; i-- {
		c := strings.TrimSpace(seen[i])
		if len(c) >= 4 && strings.Contains(r.Cmdline, c) {
			return cut(c)
		}
	}
	return cut(r.Cmdline)
}

func cut(s string) string {
	if r := []rune(s); len(r) > MaxCommand {
		return string(r[:MaxCommand])
	}
	return s
}

// ---- hosts without cgroups ----

// TreeUse is a thread's process tree: resident bytes and CPU seconds.
type TreeUse struct {
	RSS        uint64
	CPUSeconds float64
}

// TreeReader reads a thread's use from the process tree under its CLI's pid
// (V0.164 section 10), for a host without the cgroup tree: /proc on Linux,
// `ps` on macOS (PS gives its output). Cores are the growth of the tree's CPU
// time over the gap, so the time of children that ended in between is lost
// and it reads low; memory is the sum of RSS, so shared pages count more than
// once and it reads high.
type TreeReader struct {
	Proc string // default /proc
	PS   func() (string, error)

	prev map[string]cpuMark
}

type cpuMark struct {
	at  time.Time
	sec float64
}

// Read gives one reading per thread (its CLI pid by name).
func (r *TreeReader) Read(now time.Time, clis map[string]int) []Reading {
	trees := r.trees(clis)
	if r.prev == nil {
		r.prev = map[string]cpuMark{}
	}
	var out []Reading
	for th, u := range trees {
		rd := Reading{Thread: th, Work: u.RSS}
		if p, ok := r.prev[th]; ok && now.After(p.at) {
			c := max(0, (u.CPUSeconds-p.sec)/now.Sub(p.at).Seconds())
			rd.CPU = &c
		}
		r.prev[th] = cpuMark{now, u.CPUSeconds}
		out = append(out, rd)
	}
	for th := range r.prev {
		if _, ok := trees[th]; !ok {
			delete(r.prev, th)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Thread < out[j].Thread })
	return out
}

func (r *TreeReader) trees(clis map[string]int) map[string]TreeUse {
	if r.PS != nil {
		if out, err := r.PS(); err == nil {
			return TreesFromPS(out, clis)
		}
		return nil
	}
	proc := r.Proc
	if proc == "" {
		proc = "/proc"
	}
	es, _ := os.ReadDir(proc)
	type pr struct {
		ppid int
		sec  float64
		rss  uint64
	}
	all := map[int]pr{}
	children := map[int][]int{}
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, ok := readProcStat(proc, pid)
		if !ok {
			continue
		}
		all[pid] = pr{st.ppid, float64(st.utime) / clkTck, readRSS(proc, pid)}
		children[st.ppid] = append(children[st.ppid], pid)
	}
	out := map[string]TreeUse{}
	for th, cli := range clis {
		if _, ok := all[cli]; !ok {
			continue
		}
		var u TreeUse
		var walk func(int)
		walk = func(p int) {
			u.RSS += all[p].rss
			u.CPUSeconds += all[p].sec
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(cli)
		out[th] = u
	}
	return out
}

// TreesFromPS reads `ps -A -o pid=,ppid=,rss=,time=` (macOS: rss in KB, time
// as [h:]m:ss.cc) into each thread's tree under its CLI pid.
func TreesFromPS(out string, clis map[string]int) map[string]TreeUse {
	type pr struct {
		rss uint64
		sec float64
	}
	all := map[int]pr{}
	children := map[int][]int{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		rss, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		var sec float64
		for _, part := range strings.Split(f[3], ":") {
			v, _ := strconv.ParseFloat(part, 64)
			sec = sec*60 + v
		}
		all[pid] = pr{rss * 1024, sec}
		children[ppid] = append(children[ppid], pid)
	}
	res := map[string]TreeUse{}
	for th, cli := range clis {
		if _, ok := all[cli]; !ok {
			continue
		}
		var u TreeUse
		var walk func(int)
		walk = func(p int) {
			u.RSS += all[p].rss
			u.CPUSeconds += all[p].sec
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(cli)
		res[th] = u
	}
	return res
}
