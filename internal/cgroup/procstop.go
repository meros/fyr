package cgroup

// V0.process-tree-hosts, decision 0029 section 0 (c): a host without cgroups pauses a job
// with SIGSTOP and resumes it with SIGCONT. A stop outlives fyrd, so the pids
// are stored before the first signal and the next start resumes them; SIGCONT
// to a pid that was reused does nothing to a running process.
//
// V0.guard-finish: ProcStops is the job control of such a host, a type of its
// own beside JobGroups (which is cgroups only); ProcMechanism serves the
// guard through it. fyrd picks one or the other when it wires the guard.

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/kvstore"
)

// stoppedKey is where the stopped pids are kept in the store's KV table.
const stoppedKey = "stopped-procs"

// stopPasses is how often a stop looks again for children forked meanwhile.
const stopPasses = 3

const (
	stopJob        = "job/"
	stopUndeclared = "undeclared/"
)

// ProcStops stops and resumes the slot holders' jobs and the threads'
// undeclared work on a host without the cgroup tree: each thread is its
// process tree under its CLI (Trees, CLIs), and a pause is SIGSTOP. Every
// method is safe on a nil *ProcStops, which stops nothing.
type ProcStops struct {
	Trees *TreeReader
	CLIs  func() map[string]int
	KV    kvstore.Table                           // keeps the stopped pids across a restart
	Kill  func(pid int, sig syscall.Signal) error // default syscall.Kill
	// Live says thread's episode of undeclared work goes on, as
	// JobGroups.Live; nil is always.
	Live func(thread string) bool

	stopMu  sync.Mutex // orders the stops and their KV writes; taken without mu
	stopped map[string][]int
	umu     sync.Mutex // orders a freeze of undeclared work and its episode's end

	mu      sync.Mutex
	frozen  map[string]bool
	ufrozen map[string]bool
	needles map[string]string // thread → its newest heavy command's text
	held    map[string]bool   // the holders at the last Tick
}

func (p *ProcStops) ready() bool {
	return p != nil && p.Trees != nil && p.CLIs != nil
}

func (p *ProcStops) signal(pid int, sig syscall.Signal) error {
	if p.Kill != nil {
		return p.Kill(pid, sig)
	}
	return syscall.Kill(pid, sig)
}

// Seen keeps the text of thread's newest heavy command: its job is the
// processes whose command line holds it.
func (p *ProcStops) Seen(thread, text string) {
	if p == nil || text == "" {
		return
	}
	p.mu.Lock()
	if p.needles == nil {
		p.needles = map[string]string{}
	}
	p.needles[thread] = text
	p.mu.Unlock()
}

// Tick resumes the jobs of the threads that no longer hold a slot. The
// sampler's Tick calls it, outside every lock.
func (p *ProcStops) Tick(held []string) {
	if !p.ready() {
		return
	}
	now := map[string]bool{}
	for _, h := range held {
		now[h] = true
	}
	p.mu.Lock()
	was := p.held
	p.held = now
	p.mu.Unlock()
	for h := range was {
		if !now[h] {
			p.Thaw(h)
			p.mu.Lock()
			delete(p.needles, h)
			p.mu.Unlock()
		}
	}
}

// Freeze stops thread's job (not its CLI); false when nothing was stopped.
func (p *ProcStops) Freeze(thread string) bool {
	if !p.ready() || !p.stopJobOf(thread) {
		return false
	}
	p.mu.Lock()
	if p.frozen == nil {
		p.frozen = map[string]bool{}
	}
	p.frozen[thread] = true
	p.mu.Unlock()
	return true
}

// Thaw lets thread's job run again.
func (p *ProcStops) Thaw(thread string) {
	if !p.ready() {
		return
	}
	p.cont(stopJob + thread)
	p.mu.Lock()
	delete(p.frozen, thread)
	p.mu.Unlock()
}

// FreezeUndeclared stops the roots and their descendants; false when nothing
// was stopped, or when thread's episode ended (Live false).
func (p *ProcStops) FreezeUndeclared(thread string, roots []Root) bool {
	if p == nil {
		return false
	}
	p.umu.Lock()
	defer p.umu.Unlock()
	if p.Live != nil && !p.Live(thread) {
		return false
	}
	if !p.ready() || !p.stopRoots(thread, roots) {
		return false
	}
	p.mu.Lock()
	if p.ufrozen == nil {
		p.ufrozen = map[string]bool{}
	}
	p.ufrozen[thread] = true
	p.mu.Unlock()
	return true
}

// ThawUndeclared lets thread's undeclared work run again.
func (p *ProcStops) ThawUndeclared(thread string) {
	if !p.ready() {
		return
	}
	p.cont(stopUndeclared + thread)
	p.mu.Lock()
	delete(p.ufrozen, thread)
	p.mu.Unlock()
}

// EndUndeclared resumes thread's undeclared work when its episode ends,
// ordered with FreezeUndeclared.
func (p *ProcStops) EndUndeclared(thread string) {
	if p == nil {
		return
	}
	p.umu.Lock()
	defer p.umu.Unlock()
	p.ThawUndeclared(thread)
}

// Frozen reports thread's job stopped by Freeze, or its undeclared work by
// FreezeUndeclared.
func (p *ProcStops) Frozen(thread string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frozen[thread] || p.ufrozen[thread]
}

// JobEmpty reports that no process Freeze stopped for thread is left.
func (p *ProcStops) JobEmpty(thread string) bool {
	return p.ready() && p.stopGone(stopJob+thread)
}

// UndeclaredEmpty reports that no process FreezeUndeclared stopped for
// thread is left.
func (p *ProcStops) UndeclaredEmpty(thread string) bool {
	return p.ready() && p.stopGone(stopUndeclared+thread)
}

// KillJob sends SIGKILL to each process the guard stopped for thread's job
// (or its undeclared work), and to the whole group of one that leads its own
// process group (V0.rerun-classes, decision 0036 (F)). The stop is cleared.
// It returns how many processes it killed; 0 when nothing was there.
func (p *ProcStops) KillJob(thread string, undeclared bool) int {
	if !p.ready() {
		return 0
	}
	key := stopJob + thread
	if undeclared {
		key = stopUndeclared + thread
	}
	p.stopMu.Lock()
	p.loadStopped()
	ps := p.stopped[key]
	delete(p.stopped, key)
	p.saveStopped()
	p.stopMu.Unlock()
	n := 0
	for _, pid := range ps {
		if pg, err := syscall.Getpgid(pid); err == nil && pg == pid {
			p.signal(-pid, syscall.SIGKILL) // it leads its group: the whole group
		}
		if p.signal(pid, syscall.SIGKILL) == nil {
			n++
		}
	}
	p.mu.Lock()
	if undeclared {
		delete(p.ufrozen, thread)
	} else {
		delete(p.frozen, thread)
	}
	p.mu.Unlock()
	return n
}

// loadStopped reads the stored pids once; call it with stopMu held.
func (p *ProcStops) loadStopped() {
	if p.stopped != nil {
		return
	}
	p.stopped = map[string][]int{}
	if p.KV == nil {
		return
	}
	if raw, _ := p.KV.GetKV(stoppedKey); raw != "" {
		if err := json.Unmarshal([]byte(raw), &p.stopped); err != nil {
			log.Printf("stopped processes: stored list unreadable: %v", err)
			p.stopped = map[string][]int{}
		}
	}
}

func (p *ProcStops) saveStopped() error {
	if p.KV == nil {
		return nil
	}
	b, err := json.Marshal(p.stopped)
	if err != nil {
		return err
	}
	return p.KV.SetKV(stoppedKey, string(b))
}

// stop sends SIGSTOP to what pick chooses under thread's CLI, never the CLI
// itself; it looks again for what they forked meanwhile. False when nothing
// was stopped.
func (p *ProcStops) stop(key, thread string, pick func(t procTable, kids map[int][]int, cli int) []int) bool {
	cli := p.CLIs()[thread]
	if cli <= 0 {
		return false
	}
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	p.loadStopped()
	done := map[int]bool{}
	for _, pid := range p.stopped[key] {
		done[pid] = true
	}
	stopped := false
	for pass := 0; pass < stopPasses; pass++ {
		t, ok := p.Trees.Table(time.Now())
		if !ok {
			break
		}
		kids := t.kids()
		mine := map[int]bool{}
		for _, pid := range under(kids, cli) {
			mine[pid] = true
		}
		var fresh []int
		for _, pid := range pick(t, kids, cli) {
			if mine[pid] && pid != cli && pid > 1 && pid != os.Getpid() && !done[pid] {
				done[pid] = true
				fresh = append(fresh, pid)
			}
		}
		if len(fresh) == 0 {
			break
		}
		p.stopped[key] = append(p.stopped[key], fresh...)
		if err := p.saveStopped(); err != nil {
			log.Printf("stopped processes: not stopping %s: %v", thread, err)
			p.stopped[key] = p.stopped[key][:len(p.stopped[key])-len(fresh)]
			break
		}
		for _, pid := range fresh {
			if p.signal(pid, syscall.SIGSTOP) == nil {
				stopped = true
			}
		}
	}
	return stopped
}

// cont resumes what stop stopped under key.
func (p *ProcStops) cont(key string) {
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	p.loadStopped()
	if len(p.stopped[key]) == 0 {
		return
	}
	for _, pid := range p.stopped[key] {
		p.signal(pid, syscall.SIGCONT)
	}
	delete(p.stopped, key)
	if err := p.saveStopped(); err != nil {
		log.Printf("stopped processes: %v", err)
	}
}

// ResumeStopped resumes everything a stop left: at a start, for what the last
// fyrd stopped (nothing remembers those jobs now), and at a shutdown.
func (p *ProcStops) ResumeStopped() {
	if !p.ready() {
		return
	}
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	p.loadStopped()
	if len(p.stopped) == 0 {
		return
	}
	n := 0
	for _, ps := range p.stopped {
		for _, pid := range ps {
			p.signal(pid, syscall.SIGCONT)
			n++
		}
	}
	p.stopped = map[string][]int{}
	if err := p.saveStopped(); err != nil {
		log.Printf("stopped processes: %v", err)
	}
	log.Printf("stopped processes: resumed %d", n)
}

// stopGone reports that no process stopped under key is left.
func (p *ProcStops) stopGone(key string) bool {
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	p.loadStopped()
	t, ok := p.Trees.Table(time.Now())
	if !ok {
		return false
	}
	for _, pid := range p.stopped[key] {
		if _, live := t[pid]; live {
			return false
		}
	}
	return true
}

// stopJobOf stops thread's heavy job: the processes under its CLI whose
// command line holds the job's command, quoted or not, and their descendants.
func (p *ProcStops) stopJobOf(thread string) bool {
	p.mu.Lock()
	needle := bareCommand(p.needles[thread])
	p.mu.Unlock()
	if needle == "" {
		return false
	}
	return p.stop(stopJob+thread, thread, func(t procTable, kids map[int][]int, cli int) []int {
		var out []int
		for _, pid := range under(kids, cli) {
			if pid != cli && strings.Contains(bareCommand(t[pid].args), needle) {
				out = append(out, under(kids, pid)...)
			}
		}
		return out
	})
}

// stopRoots stops roots and their descendants.
func (p *ProcStops) stopRoots(thread string, roots []Root) bool {
	return p.stop(stopUndeclared+thread, thread, func(t procTable, kids map[int][]int, _ int) []int {
		var out []int
		for _, r := range roots {
			if _, ok := t[r.PID]; ok {
				out = append(out, under(kids, r.PID)...)
			}
		}
		return out
	})
}
