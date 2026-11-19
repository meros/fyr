package cgroup

// V0.164 section 10, V0.process-tree-hosts: a host without the cgroup tree (macOS, Linux
// without delegation) reads each thread from the process tree under its CLI:
// what it uses, the job a slot runs, the roots of undeclared work, and which
// processes to stop under the memory guard (procstop.go).

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TreePS is the `ps -A -o` format TreeReader.PS gives (macOS: rss in KB, etime
// [[dd-]hh:]mm:ss, time [h:]m:ss.cc).
const TreePS = "pid=,ppid=,rss=,etime=,time=,state=,args="

// treeKeep is how long a thread's CPU count outlives its CLI, so a CLI that
// comes back goes on counting from the same figure.
const treeKeep = time.Hour

// treeGapMax is the longest gap a rate is read over.
const treeGapMax = time.Minute

// procRow is one live process of the table.
type procRow struct {
	pid, ppid int
	rss       uint64
	cpu       float64 // seconds, user + system
	at        time.Time
	args      string
}

// procTable is the host's live processes by pid; zombies are left out.
type procTable map[int]procRow

func (t procTable) kids() map[int][]int {
	out := map[int][]int{}
	for p, r := range t {
		out[r.ppid] = append(out[r.ppid], p)
	}
	return out
}

// under is root and its descendants, parents first.
func under(kids map[int][]int, root int) []int {
	seen := map[int]bool{root: true}
	out := []int{root}
	for i := 0; i < len(out); i++ {
		for _, c := range kids[out[i]] {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// roots are the job roots under cli for work that began at cut or later: the
// processes started since whose parent is older (ReadRoots without a cgroup),
// and a Bash shell of the CLI started before that holds at least minOld bytes
// (V0.freeze-old-work; 0 takes none). A detached job left the tree under the
// CLI and is not seen here.
func (t procTable) roots(cli int, cut time.Time, minOld uint64) []Root {
	if _, ok := t[cli]; !ok {
		return nil
	}
	kids := t.kids()
	here := map[int]bool{}
	for _, p := range under(kids, cli) {
		if p != cli {
			here[p] = true
		}
	}
	sum := func(p int) uint64 {
		var rss uint64
		for _, q := range under(kids, p) {
			rss += t[q].rss
		}
		return rss
	}
	var out []Root
	elder := map[int]bool{}
	if minOld > 0 {
		for _, p := range kids[cli] {
			r := t[p]
			if r.at.Before(cut) && agentShell(r.args) {
				if rss := sum(p); rss >= minOld {
					elder[p] = true
					out = append(out, Root{PID: p, Cmdline: r.args, RSS: rss})
				}
			}
		}
	}
	for p := range here {
		r := t[p]
		if r.at.Before(cut) || (here[r.ppid] && !t[r.ppid].at.Before(cut)) {
			continue
		}
		inElder := false
		for q, n := r.ppid, 0; here[q] && n < len(here); q, n = t[q].ppid, n+1 {
			if elder[q] {
				inElder = true
				break
			}
		}
		if inElder {
			continue
		}
		out = append(out, Root{PID: p, Cmdline: r.args, RSS: sum(p)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// etimeSeconds reads ps's etime, [[dd-]hh:]mm:ss.
func etimeSeconds(et string) int {
	days := 0
	if d, rest, ok := strings.Cut(et, "-"); ok {
		days, _ = strconv.Atoi(d)
		et = rest
	}
	sec := 0
	for _, part := range strings.Split(et, ":") {
		v, _ := strconv.Atoi(part)
		sec = sec*60 + v
	}
	return days*86400 + sec
}

// parseTreePS reads the output of `ps -A -o` TreePS.
func parseTreePS(out string, now time.Time) procTable {
	t := procTable{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 6 || strings.HasPrefix(f[5], "Z") {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		rss, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		var sec float64
		for _, part := range strings.Split(f[4], ":") {
			v, _ := strconv.ParseFloat(part, 64)
			sec = sec*60 + v
		}
		t[pid] = procRow{pid: pid, ppid: ppid, rss: rss * 1024, cpu: sec,
			at: now.Add(-time.Duration(etimeSeconds(f[3])) * time.Second), args: strings.Join(f[6:], " ")}
	}
	return t
}

// TreeReader reads the process table for a host without the cgroup tree:
// /proc on Linux, `ps` on macOS (PS gives its output, in the form TreePS).
//
// A thread's CPU time counts per process, so a child that ends takes nothing
// back; a process that starts and ends between two reads is not seen, so the
// figure reads low. Memory is the sum of RSS, so shared pages count more than
// once and it reads high.
type TreeReader struct {
	Proc string // default /proc
	PS   func() (string, error)

	marks map[string]*treeMark // Read's, which one goroutine calls at a time
}

type treeMark struct {
	at   time.Time
	used float64
	cpu  map[int]float64
}

// Table is the host's live processes now; false when they cannot be read.
func (r *TreeReader) Table(now time.Time) (procTable, bool) {
	if r.PS != nil {
		out, err := r.PS()
		if err != nil {
			return nil, false
		}
		return parseTreePS(out, now), true
	}
	proc := r.Proc
	if proc == "" {
		proc = "/proc"
	}
	es, err := os.ReadDir(proc)
	if err != nil {
		return nil, false
	}
	boot, _ := bootTime(proc)
	t := procTable{}
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, ok := readProcStat(proc, pid)
		if !ok || zombie(proc, pid) {
			continue
		}
		t[pid] = procRow{pid: pid, ppid: st.ppid, rss: readRSS(proc, pid), cpu: float64(st.utime) / clkTck,
			at: boot.Add(time.Duration(st.started) * time.Second / clkTck), args: readCmdline(proc, pid)}
	}
	return t, true
}

// Read gives one reading per thread with a live CLI (its pid by thread), in
// thread order. Used counts on from the thread's first read.
func (r *TreeReader) Read(now time.Time, clis map[string]int) []Reading {
	t, ok := r.Table(now)
	if !ok {
		return nil
	}
	if r.marks == nil {
		r.marks = map[string]*treeMark{}
	}
	kids := t.kids()
	var out []Reading
	for th, cli := range clis {
		if _, ok := t[cli]; !ok {
			continue
		}
		m := r.marks[th]
		first := m == nil
		if first {
			m = &treeMark{}
			r.marks[th] = m
		}
		rd := Reading{Thread: th}
		cpu := map[int]float64{}
		grew := 0.0
		for _, p := range under(kids, cli) {
			row := t[p]
			rd.Work += row.rss
			cpu[p] = row.cpu
			if was, known := m.cpu[p]; known && row.cpu >= was {
				grew += row.cpu - was
			} else if !first {
				grew += row.cpu // it started since the last read
			}
		}
		if gap := now.Sub(m.at); !first && gap > 0 && gap <= treeGapMax {
			c := grew / gap.Seconds()
			rd.CPU = &c
		}
		m.at, m.cpu = now, cpu
		m.used += grew
		rd.Used = time.Duration(m.used * float64(time.Second))
		out = append(out, rd)
	}
	for th, m := range r.marks {
		if now.Sub(m.at) > treeKeep {
			delete(r.marks, th)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Thread < out[j].Thread })
	return out
}
