package cgroup

// V0.164 section 4.5: the processes behind an undeclared episode (the
// job's roots and their command).

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	in := undeclaredPids(td)
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
//
// V0.201: the CLI runs a command quoted inside its shell wrapper, so the two
// are compared without quotes, backslashes and white space. A wrapper that
// matches nothing gives "": its first 300 characters are only the shell's
// environment and name no command.
func CommandOf(r Root, seen []string) string {
	line := bareCommand(r.Cmdline)
	for i := len(seen) - 1; i >= 0; i-- {
		c := strings.TrimSpace(seen[i])
		if len(c) >= 4 && strings.Contains(line, bareCommand(c)) {
			return cut(c)
		}
	}
	if strings.Contains(r.Cmdline, "/shell-snapshots/snapshot-") {
		return ""
	}
	return cut(r.Cmdline)
}

// bareCommand is s without quotes, backslashes and white space.
func bareCommand(s string) string {
	return strings.Map(func(c rune) rune {
		if c == '\'' || c == '"' || c == '\\' || unicode.IsSpace(c) {
			return -1
		}
		return c
	}, s)
}

func cut(s string) string {
	if r := []rune(s); len(r) > MaxCommand {
		return string(r[:MaxCommand])
	}
	return s
}
