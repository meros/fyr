package cgroup

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// readArgv is the command line; a process with none (a kernel thread, one
// caught in exec) has its comm.
func readArgv(proc string, pid int) []string {
	b, _ := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "cmdline"))
	if a := strings.FieldsFunc(string(b), func(r rune) bool { return r == 0 }); len(a) > 0 {
		return a
	}
	c, _ := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "comm"))
	if s := strings.TrimSpace(string(c)); s != "" {
		return []string{s}
	}
	return nil
}

func readP(proc string, pid int, boot time.Time) (pinfo, bool) {
	st, ok := readProcStat(proc, pid)
	if !ok {
		return pinfo{}, false
	}
	return pinfo{pid: pid, ppid: st.ppid, start: st.started, at: boot.Add(time.Duration(st.started) * time.Second / clkTck),
		rss: readRSS(proc, pid), argv: readArgv(proc, pid)}, true
}

// ScanLeftovers reads every thread's cgroup under agents/ and returns its
// leftovers, roots only. clis gives each thread's live CLI pid; 0 is a CLI
// that is live or starting with no pid known, and that thread is skipped.
func ScanLeftovers(tr *Tree, proc string, clis map[string]int) []Leftover {
	if tr == nil {
		return nil
	}
	if proc == "" {
		proc = "/proc"
	}
	boot, ok := bootTime(proc)
	if !ok {
		return nil
	}
	var out []Leftover
	for _, c := range tr.threadCgroups() {
		name := c.Name
		if name == DeployDir {
			continue
		}
		cli, live := clis[name]
		if live && cli == 0 {
			continue
		}
		var in []int
		filepath.WalkDir(c.Dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "cgroup.procs" {
				in = append(in, pids(p)...)
			}
			return nil
		})
		table := map[int]pinfo{}
		for _, p := range in {
			if pi, ok := readP(proc, p, boot); ok {
				table[p] = pi
			}
		}
		out = append(out, rootsOf(name, table, cli)...)
	}
	return out
}

// rootsOf groups the processes of one thread's cgroup (table) that are not
// cli's tree into roots: a process whose parent is not in the set.
func rootsOf(thread string, table map[int]pinfo, cli int) []Leftover {
	children := map[int][]int{}
	for p, pi := range table {
		children[pi.ppid] = append(children[pi.ppid], p)
	}
	mine := map[int]bool{}
	if cli > 0 {
		var walk func(int)
		walk = func(p int) {
			mine[p] = true
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(cli)
	}
	var out []Leftover
	for p, pi := range table {
		if mine[p] {
			continue
		}
		if _, ok := table[pi.ppid]; ok && !mine[pi.ppid] {
			continue // its parent is a leftover: part of that root
		}
		l := Leftover{Thread: thread, PID: p, Started: pi.at, Command: cut(strings.Join(pi.argv, " ")), Name: shortName(pi.argv),
			ID: fmt.Sprintf("%d@%d", p, pi.start), starts: map[int]uint64{}}
		var walk func(int)
		var until time.Duration
		var untilFrom time.Time
		walk = func(q int) {
			x := table[q]
			l.pids = append(l.pids, q)
			l.starts[q] = x.start
			l.RSS += x.rss
			if until == 0 {
				if d := timeoutOf(x.argv); d > 0 {
					until, untilFrom = d, x.at
				}
			}
			kids := children[q]
			sort.Ints(kids)
			for _, c := range kids {
				walk(c)
			}
		}
		walk(p)
		l.Procs = len(l.pids)
		if until > 0 {
			u := untilFrom.Add(until)
			l.Until = &u
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// timeoutOf is the duration of a `timeout [opts] DURATION cmd` command line
// (coreutils: s, m, h, d, a fraction allowed), or 0.
func timeoutOf(argv []string) time.Duration {
	if len(argv) < 2 || filepath.Base(argv[0]) != "timeout" {
		return 0
	}
	i := 1
	for i < len(argv) && strings.HasPrefix(argv[i], "-") {
		a := argv[i]
		i++
		if (a == "-s" || a == "-k" || a == "--signal" || a == "--kill-after") && i < len(argv) {
			i++
		}
	}
	if i >= len(argv)-1 {
		return 0
	}
	s, unit := argv[i], time.Second
	switch {
	case strings.HasSuffix(s, "s"):
		s = strings.TrimSuffix(s, "s")
	case strings.HasSuffix(s, "m"):
		s, unit = strings.TrimSuffix(s, "m"), time.Minute
	case strings.HasSuffix(s, "h"):
		s, unit = strings.TrimSuffix(s, "h"), time.Hour
	case strings.HasSuffix(s, "d"):
		s, unit = strings.TrimSuffix(s, "d"), 24*time.Hour
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return time.Duration(f * float64(unit))
}

// shortName names a command line for a person: the program, past the
// wrappers (timeout, setsid, nohup, env), a Python module (-m), a node
// script, and a kubectl verb.
func shortName(argv []string) string {
	for len(argv) > 0 {
		b := filepath.Base(argv[0])
		switch b {
		case "setsid", "nohup", "env", "nice", "stdbuf":
			argv = argv[1:]
			for len(argv) > 0 && (strings.HasPrefix(argv[0], "-") || strings.Contains(argv[0], "=")) {
				argv = argv[1:]
			}
			continue
		case "timeout":
			i := 1
			for i < len(argv) && strings.HasPrefix(argv[i], "-") {
				a := argv[i]
				i++
				if (a == "-s" || a == "-k" || a == "--signal" || a == "--kill-after") && i < len(argv) {
					i++
				}
			}
			if i+1 > len(argv) {
				return b
			}
			argv = argv[i+1:]
			continue
		}
		if strings.HasPrefix(b, "python") {
			for i, a := range argv {
				if a == "-m" && i+1 < len(argv) {
					return argv[i+1]
				}
			}
			if len(argv) > 1 {
				return filepath.Base(argv[1])
			}
		}
		if b == "node" || b == "bun" || b == "deno" {
			for _, a := range argv[1:] {
				if !strings.HasPrefix(a, "-") {
					return filepath.Base(a)
				}
			}
		}
		if b == "kubectl" && len(argv) > 1 {
			return b + " " + argv[1]
		}
		return b
	}
	return ""
}

// ---- the process-tree mode ----

// table reads the whole process table: /proc, or ps.
func (l *Leftovers) table(now time.Time) map[int]pinfo {
	if l.PS != nil {
		out, err := l.PS()
		if err != nil {
			return nil
		}
		return psTable(out, now)
	}
	proc := l.Proc
	if proc == "" {
		proc = "/proc"
	}
	boot, ok := bootTime(proc)
	if !ok {
		return nil
	}
	es, _ := os.ReadDir(proc)
	t := map[int]pinfo{}
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pi, ok := readP(proc, pid, boot); ok && !zombie(proc, pid) {
			t[pid] = pi
		}
	}
	return t
}

// psTable reads `ps -A -o pid=,ppid=,rss=,etime=,args=` (rss in KB, etime
// [[dd-]hh:]mm:ss). A start read from etime moves by a second between reads,
// so a process's identity there is its pid and a hash of its command line.
func psTable(out string, now time.Time) map[int]pinfo {
	t := map[int]pinfo{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		rss, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		at := now.Add(-time.Duration(etimeSeconds(f[3])) * time.Second)
		h := fnv.New64a()
		h.Write([]byte(strings.Join(f[4:], " ")))
		t[pid] = pinfo{pid: pid, ppid: ppid, rss: rss * 1024, at: at, start: h.Sum64() >> 1, argv: f[4:]}
	}
	return t
}

// scanTrees is ScanLeftovers without a cgroup tree: each live CLI's
// descendants are remembered as its thread's; a remembered process outside
// its live CLI's tree is a leftover.
func (l *Leftovers) scanTrees(clis map[string]int, now time.Time) []Leftover {
	t := l.table(now)
	if t == nil {
		return nil
	}
	children := map[int][]int{}
	for p, pi := range t {
		children[pi.ppid] = append(children[pi.ppid], p)
	}
	key := func(pi pinfo) string { return fmt.Sprintf("%d@%d", pi.pid, pi.start) }
	l.mu.Lock()
	if l.owned == nil {
		l.owned = map[string]string{}
	}
	under := map[int]string{}
	for th, cli := range clis {
		if _, ok := t[cli]; !ok || cli == 0 {
			continue
		}
		var walk func(int)
		walk = func(p int) {
			under[p] = th
			if p != cli {
				l.owned[key(t[p])] = th
			}
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(cli)
	}
	byThread := map[string]map[int]pinfo{}
	for k, th := range l.owned {
		pidS, _, _ := strings.Cut(k, "@")
		pid, _ := strconv.Atoi(pidS)
		pi, ok := t[pid]
		if !ok || key(pi) != k {
			delete(l.owned, k) // it ended
			continue
		}
		if cli, live := clis[th]; live && cli == 0 {
			continue
		}
		if under[pid] == th {
			continue
		}
		if byThread[th] == nil {
			byThread[th] = map[int]pinfo{}
		}
		var walk func(int)
		walk = func(p int) {
			if _, done := byThread[th][p]; done || under[p] != "" {
				return
			}
			byThread[th][p] = t[p]
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(pid)
	}
	l.mu.Unlock()
	var out []Leftover
	for th, tab := range byThread {
		out = append(out, rootsOf(th, tab, 0)...)
	}
	return out
}
