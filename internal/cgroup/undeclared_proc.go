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

// Elders are the older work an episode's roots also take (V0.freeze-old-work):
// a job that started before the episode's window but holds its memory, such
// as a long build whose memory grew slowly. An older process is a root when
// its subtree holds at least Min bytes and it is either a Bash shell the CLI
// started or a process whose parent is outside the thread's cgroup and is not
// the CLI (a job detached with setsid or nohup &). MCP servers and other
// children of the CLI stay residents. A zero CLI or Min takes no elder.
type Elders struct {
	CLI int
	Min uint64
	// Service says a command line is a service (V0.services-not-jobs): a root
	// whose subtree holds one is neither a root nor an elder, so the guard
	// never freezes a service or names one as a part in a turn. nil is none.
	Service func(cmdline string) bool
}

// agentShell reports the CLI's wrapper around one Bash command.
func agentShell(cmdline string) bool {
	return strings.Contains(cmdline, "/shell-snapshots/snapshot-")
}

// ReadRoots are the job roots of thread's cgroup for an episode whose first
// over reading was at after, in pid order, each with its subtree's memory.
// The processes moved into undeclared/ still count, so the command is known
// after a freeze.
func ReadRoots(tr *Tree, proc, thread string, after time.Time) []Root {
	return ReadRootsWith(tr, proc, thread, after, Elders{})
}

// ReadRootsWith is ReadRoots that also takes the elders el allows.
func ReadRootsWith(tr *Tree, proc, thread string, after time.Time, el Elders) []Root {
	if tr == nil {
		return nil
	}
	if proc == "" {
		proc = "/proc"
	}
	td := tr.threadDir(unsafe.ReplaceAllString(thread, "_"))
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
	var sum func(int) uint64
	sum = func(q int) uint64 {
		t := readRSS(proc, q)
		for _, c := range children[q] {
			t += sum(c)
		}
		return t
	}
	var roots []Root
	elder := map[int]bool{}
	if el.CLI > 0 && el.Min > 0 {
		for p := range here {
			pp := parent[p]
			if p == el.CLI || !start[p].Before(cut) {
				continue
			}
			line := readCmdline(proc, p)
			if (pp == el.CLI && agentShell(line)) || !here[pp] {
				if rss := sum(p); rss >= el.Min {
					elder[p] = true
					roots = append(roots, Root{PID: p, Cmdline: line, RSS: rss})
				}
			}
		}
	}
	inElder := func(p int) bool { // p is in an elder's subtree; a pid reused mid-read cannot loop it
		for q, n := parent[p], 0; here[q] && n < len(here); q, n = parent[q], n+1 {
			if elder[q] {
				return true
			}
		}
		return false
	}
	for p := range here {
		// V0.guard-tidy: a CLI restarted during the episode (a handover) is
		// new too, but it is never a root: frozen, it would take its MCP
		// servers with it. Of its children only a Bash shell is work.
		if start[p].Before(cut) || (el.CLI > 0 && p == el.CLI) {
			continue
		}
		if pp := parent[p]; here[pp] && !start[pp].Before(cut) && (pp != el.CLI || !agentShell(readCmdline(proc, p))) {
			continue
		}
		if inElder(p) {
			continue
		}
		roots = append(roots, Root{PID: p, Cmdline: readCmdline(proc, p), RSS: sum(p)})
	}
	if el.Service != nil {
		roots = skipServices(roots, here, children, func(p int) string { return readCmdline(proc, p) }, el.Service)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].PID < roots[j].PID })
	return roots
}

// skipServices drops the roots whose subtree holds a service.
func skipServices(roots []Root, here map[int]bool, children map[int][]int, cmd func(int) string, service func(string) bool) []Root {
	var holds func(p, depth int) bool
	holds = func(p, depth int) bool {
		if depth > 64 || !here[p] {
			return false
		}
		if service(cmd(p)) {
			return true
		}
		for _, c := range children[p] {
			if holds(c, depth+1) {
				return true
			}
		}
		return false
	}
	out := roots[:0]
	for _, r := range roots {
		if !holds(r.PID, 0) {
			out = append(out, r)
		}
	}
	return out
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
