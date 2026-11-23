package cgroup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (l *Leftovers) end(byThread map[string][]Leftover, why string) {
	wait := l.TermWait
	if wait == 0 {
		wait = 5 * time.Second
	}
	proc := l.Proc
	if l.Tree == nil && l.PS != nil {
		proc = "-" // no /proc: signal by pid, no start-time check
	}
	for th, xs := range byThread {
		var done []Leftover
		for _, x := range xs {
			if endPIDs(proc, x.pids, x.starts, wait, l.Signal) > 0 {
				done = append(done, x)
			}
		}
		if len(done) == 0 {
			continue
		}
		var names []string
		for _, x := range done {
			names = append(names, x.Name)
		}
		l.logf("leftovers: %s: ended %s (%s)", th, strings.Join(names, ", "), why)
		if l.OnEnd != nil {
			l.OnEnd(th, done, why)
		}
	}
}

// endPIDs sends SIGTERM to each pid whose start time is still the one seen,
// waits up to wait for them to go, then SIGKILLs the rest. It returns how many
// it signalled. proc "-" skips the start-time check (no /proc).
func endPIDs(proc string, ps []int, starts map[int]uint64, wait time.Duration, signal func(int, syscall.Signal) error) int {
	if proc == "" {
		proc = "/proc"
	}
	if signal == nil {
		signal = syscall.Kill
	}
	same := func(p int) bool {
		if p <= 1 || p == os.Getpid() {
			return false
		}
		if proc == "-" {
			return syscall.Kill(p, 0) == nil
		}
		st, ok := readProcStat(proc, p)
		return ok && st.started == starts[p]
	}
	var sent []int
	for _, p := range ps {
		if same(p) && signal(p, syscall.SIGTERM) == nil {
			sent = append(sent, p)
		}
	}
	n := len(sent)
	deadline := time.Now().Add(wait)
	for len(sent) > 0 {
		left := sent[:0]
		for _, p := range sent {
			if same(p) && !zombie(proc, p) {
				left = append(left, p)
			}
		}
		sent = left
		if len(sent) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, p := range sent {
		if same(p) {
			signal(p, syscall.SIGKILL) // a frozen cgroup's process too
		}
	}
	return n
}

func zombie(proc string, p int) bool {
	if proc == "-" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(p), "stat"))
	if err != nil {
		return true
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || strings.HasPrefix(strings.TrimSpace(s[i+1:]), "Z")
}
