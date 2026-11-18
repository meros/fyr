package cgroup

// V0.process-tree-hosts, decision 0029 section 0 (c): a host without cgroups pauses a job
// with SIGSTOP and resumes it with SIGCONT. A stop outlives fyrd, so the pids
// are stored before the first signal and the next start resumes them; SIGCONT
// to a pid that was reused does nothing to a running process.

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"syscall"
	"time"
)

// stoppedKey is where the stopped pids are kept in the store's KV table.
const stoppedKey = "stopped-procs"

// stopPasses is how often a stop looks again for children forked meanwhile.
const stopPasses = 3

const (
	stopJob        = "job/"
	stopUndeclared = "undeclared/"
)

func (g *JobGroups) canStop() bool {
	return g != nil && g.Tree == nil && g.Trees != nil && g.CLIs != nil
}

func (g *JobGroups) signal(pid int, sig syscall.Signal) error {
	if g.Kill != nil {
		return g.Kill(pid, sig)
	}
	return syscall.Kill(pid, sig)
}

// loadStopped reads the stored pids once; call it with stopMu held.
func (g *JobGroups) loadStopped() {
	if g.stopped != nil {
		return
	}
	g.stopped = map[string][]int{}
	if g.KV == nil {
		return
	}
	if raw, _ := g.KV.GetKV(stoppedKey); raw != "" {
		if err := json.Unmarshal([]byte(raw), &g.stopped); err != nil {
			log.Printf("stopped processes: stored list unreadable: %v", err)
			g.stopped = map[string][]int{}
		}
	}
}

func (g *JobGroups) saveStopped() error {
	if g.KV == nil {
		return nil
	}
	b, err := json.Marshal(g.stopped)
	if err != nil {
		return err
	}
	return g.KV.SetKV(stoppedKey, string(b))
}

// stop sends SIGSTOP to what pick chooses under thread's CLI, never the CLI
// itself; it looks again for what they forked meanwhile. False when nothing
// was stopped.
func (g *JobGroups) stop(key, thread string, pick func(t procTable, kids map[int][]int, cli int) []int) bool {
	cli := g.CLIs()[thread]
	if cli <= 0 {
		return false
	}
	g.stopMu.Lock()
	defer g.stopMu.Unlock()
	g.loadStopped()
	done := map[int]bool{}
	for _, p := range g.stopped[key] {
		done[p] = true
	}
	stopped := false
	for pass := 0; pass < stopPasses; pass++ {
		t, ok := g.Trees.Table(time.Now())
		if !ok {
			break
		}
		kids := t.kids()
		mine := map[int]bool{}
		for _, p := range under(kids, cli) {
			mine[p] = true
		}
		var fresh []int
		for _, p := range pick(t, kids, cli) {
			if mine[p] && p != cli && p > 1 && p != os.Getpid() && !done[p] {
				done[p] = true
				fresh = append(fresh, p)
			}
		}
		if len(fresh) == 0 {
			break
		}
		g.stopped[key] = append(g.stopped[key], fresh...)
		if err := g.saveStopped(); err != nil {
			log.Printf("stopped processes: not stopping %s: %v", thread, err)
			g.stopped[key] = g.stopped[key][:len(g.stopped[key])-len(fresh)]
			break
		}
		for _, p := range fresh {
			if g.signal(p, syscall.SIGSTOP) == nil {
				stopped = true
			}
		}
	}
	return stopped
}

// cont resumes what stop stopped under key.
func (g *JobGroups) cont(key string) {
	g.stopMu.Lock()
	defer g.stopMu.Unlock()
	g.loadStopped()
	if len(g.stopped[key]) == 0 {
		return
	}
	for _, p := range g.stopped[key] {
		g.signal(p, syscall.SIGCONT)
	}
	delete(g.stopped, key)
	if err := g.saveStopped(); err != nil {
		log.Printf("stopped processes: %v", err)
	}
}

// ResumeStopped resumes everything a stop left: at a start, for what the last
// fyrd stopped (nothing remembers those jobs now), and at a shutdown.
func (g *JobGroups) ResumeStopped() {
	if !g.canStop() {
		return
	}
	g.stopMu.Lock()
	defer g.stopMu.Unlock()
	g.loadStopped()
	if len(g.stopped) == 0 {
		return
	}
	n := 0
	for _, ps := range g.stopped {
		for _, p := range ps {
			g.signal(p, syscall.SIGCONT)
			n++
		}
	}
	g.stopped = map[string][]int{}
	if err := g.saveStopped(); err != nil {
		log.Printf("stopped processes: %v", err)
	}
	log.Printf("stopped processes: resumed %d", n)
}

// stopGone reports that no process stopped under key is left.
func (g *JobGroups) stopGone(key string) bool {
	g.stopMu.Lock()
	defer g.stopMu.Unlock()
	g.loadStopped()
	t, ok := g.Trees.Table(time.Now())
	if !ok {
		return false
	}
	for _, p := range g.stopped[key] {
		if _, live := t[p]; live {
			return false
		}
	}
	return true
}

// stopJobOf stops thread's heavy job: the processes under its CLI whose
// command line holds the job's command, quoted or not, and their descendants.
func (g *JobGroups) stopJobOf(thread string) bool {
	g.mu.Lock()
	needle := bareCommand(g.needles[thread])
	g.mu.Unlock()
	if needle == "" {
		return false
	}
	return g.stop(stopJob+thread, thread, func(t procTable, kids map[int][]int, cli int) []int {
		var out []int
		for _, p := range under(kids, cli) {
			if p != cli && strings.Contains(bareCommand(t[p].args), needle) {
				out = append(out, under(kids, p)...)
			}
		}
		return out
	})
}

// stopRoots stops roots and their descendants.
func (g *JobGroups) stopRoots(thread string, roots []Root) bool {
	return g.stop(stopUndeclared+thread, thread, func(t procTable, kids map[int][]int, _ int) []int {
		var out []int
		for _, r := range roots {
			if _, ok := t[r.PID]; ok {
				out = append(out, under(kids, r.PID)...)
			}
		}
		return out
	})
}
