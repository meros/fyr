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
	"slices"
	"sort"
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
	needles map[string]string       // thread → its newest heavy command's text
	held    map[string]bool         // the holders at the last Tick
	units   map[string]map[int]bool // thread → unit pids pinned while taking turns
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
	delete(p.units, thread)
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

// Holds reports whether pid is in thread's job (stopped, or what Freeze would
// stop) or its stopped undeclared work: decision 0036 (D), the guard passes
// over a thread whose job holds one of its services.
func (p *ProcStops) Holds(thread string, pid int) bool {
	if !p.ready() || pid <= 0 {
		return false
	}
	p.stopMu.Lock()
	p.loadStopped()
	held := slices.Contains(p.stopped[stopJob+thread], pid) || slices.Contains(p.stopped[stopUndeclared+thread], pid)
	p.stopMu.Unlock()
	if held {
		return true
	}
	p.mu.Lock()
	needle := bareCommand(p.needles[thread])
	p.mu.Unlock()
	if needle == "" {
		return false
	}
	cli := p.CLIs()[thread]
	if cli <= 0 {
		return false
	}
	t, ok := p.Trees.Table(time.Now())
	if !ok {
		return false
	}
	return slices.Contains(jobPids(t, t.kids(), cli, needle), pid)
}

// Units are the parts of thread's undeclared work under roots, in pid order
// (decision 0032 slice 2 on hosts without cgroups).
func (p *ProcStops) Units(thread string, roots []Root) []Unit {
	if !p.ready() || len(roots) == 0 {
		return nil
	}
	cli := p.CLIs()[thread]
	if cli <= 0 {
		return nil
	}
	t, ok := p.Trees.Table(time.Now())
	if !ok {
		return nil
	}
	kids := t.kids()
	f := &family{children: map[int][]int{}, here: map[int]bool{}, rss: map[int]uint64{}, memo: map[int]uint64{}}
	for _, pid := range under(kids, cli) {
		if pid == cli {
			continue
		}
		row := t[pid]
		f.here[pid] = true
		f.rss[pid] = row.rss
		f.children[row.ppid] = append(f.children[row.ppid], pid)
	}
	for _, c := range f.children {
		sort.Ints(c)
	}
	p.mu.Lock()
	pinnedSet := map[int]bool{}
	for pid := range p.units[thread] {
		pinnedSet[pid] = true
	}
	p.mu.Unlock()
	pinned := func(pid int) bool { return pinnedSet[pid] }
	var out []Unit
	for _, r := range roots {
		if !f.here[r.PID] {
			continue
		}
		for _, pid := range f.split(r.PID, pinned) {
			out = append(out, Unit{PID: pid, Cmdline: t[pid].args, RSS: f.subtree(pid)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// RotateUndeclared lets the unit with pid active run and stops the others with
// SIGSTOP, storing the stopped pids before signalling so a restart resumes them.
// Inactive units are stopped before active is resumed, so two units never run
// together. False when thread's episode ended (Live false) or nothing is stopped.
func (p *ProcStops) RotateUndeclared(thread string, units []Unit, active int) bool {
	if p == nil || len(units) == 0 {
		return false
	}
	p.umu.Lock()
	defer p.umu.Unlock()
	if p.Live != nil && !p.Live(thread) {
		return false
	}
	if !p.ready() {
		return false
	}
	cli := p.CLIs()[thread]
	if cli <= 0 {
		return false
	}
	t, ok := p.Trees.Table(time.Now())
	if !ok {
		return false
	}
	kids := t.kids()
	mine := map[int]bool{}
	for _, pid := range under(kids, cli) {
		if pid != cli && pid > 1 && pid != os.Getpid() {
			mine[pid] = true
		}
	}
	wantStop := map[int]bool{}
	var ordered []int
	for _, u := range units {
		if u.PID == active || !mine[u.PID] {
			continue
		}
		for _, pid := range under(kids, u.PID) {
			if mine[pid] && !wantStop[pid] {
				wantStop[pid] = true
				ordered = append(ordered, pid)
			}
		}
	}
	key := stopUndeclared + thread
	p.stopMu.Lock()
	p.loadStopped()
	was := map[int]bool{}
	for _, pid := range p.stopped[key] {
		was[pid] = true
	}
	var fresh []int
	for _, pid := range ordered {
		if !was[pid] {
			fresh = append(fresh, pid)
		}
	}
	var resume []int
	for _, pid := range p.stopped[key] {
		if !wantStop[pid] {
			resume = append(resume, pid)
		}
	}
	if len(fresh) > 0 {
		prev := append([]int(nil), p.stopped[key]...)
		p.stopped[key] = append(p.stopped[key], fresh...)
		if err := p.saveStopped(); err != nil {
			log.Printf("stopped processes: not rotating %s: %v", thread, err)
			p.stopped[key] = prev
			p.stopMu.Unlock()
			return false
		}
	}
	stoppedSet := map[int]bool{}
	for _, pid := range ordered {
		if was[pid] {
			stoppedSet[pid] = true
		}
	}
	for _, pid := range fresh {
		if p.signal(pid, syscall.SIGSTOP) == nil {
			stoppedSet[pid] = true
		}
	}
	for _, pid := range resume {
		p.signal(pid, syscall.SIGCONT)
	}
	var kept []int
	for _, pid := range ordered {
		if stoppedSet[pid] {
			kept = append(kept, pid)
		}
	}
	if len(kept) == 0 {
		delete(p.stopped, key)
	} else {
		p.stopped[key] = kept
	}
	if err := p.saveStopped(); err != nil {
		log.Printf("stopped processes: %v", err)
	}
	p.stopMu.Unlock()
	if len(kept) == 0 {
		return false
	}
	pinned := map[int]bool{}
	for _, u := range units {
		pinned[u.PID] = true
	}
	p.mu.Lock()
	if p.ufrozen == nil {
		p.ufrozen = map[string]bool{}
	}
	if p.units == nil {
		p.units = map[string]map[int]bool{}
	}
	p.ufrozen[thread] = true
	p.units[thread] = pinned
	p.mu.Unlock()
	return true
}

// KillJob sends SIGKILL to each process the guard stopped for thread's job
// (or its undeclared work, including the active part if taking turns), and to
// the whole group of one that leads its own process group (V0.rerun-classes,
// decision 0036 (F)). The stop is cleared. It returns how many processes it
// killed; 0 when nothing was there.
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
	p.mu.Lock()
	var unitPIDs []int
	if undeclared {
		for pid := range p.units[thread] {
			unitPIDs = append(unitPIDs, pid)
		}
		delete(p.units, thread)
		delete(p.ufrozen, thread)
	} else {
		delete(p.frozen, thread)
	}
	p.mu.Unlock()
	if len(unitPIDs) > 0 {
		if cli := p.CLIs()[thread]; cli > 0 {
			if t, ok := p.Trees.Table(time.Now()); ok {
				kids := t.kids()
				mine := map[int]bool{}
				for _, pid := range under(kids, cli) {
					if pid != cli && pid > 1 && pid != os.Getpid() {
						mine[pid] = true
					}
				}
				seen := map[int]bool{}
				for _, pid := range ps {
					seen[pid] = true
				}
				for _, up := range unitPIDs {
					if !mine[up] {
						continue
					}
					for _, pid := range under(kids, up) {
						if mine[pid] && !seen[pid] {
							seen[pid] = true
							ps = append(ps, pid)
						}
					}
				}
			}
		}
	}
	n := 0
	for _, pid := range ps {
		if pg, err := syscall.Getpgid(pid); err == nil && pg == pid {
			p.signal(-pid, syscall.SIGKILL) // it leads its group: the whole group
		}
		if p.signal(pid, syscall.SIGKILL) == nil {
			n++
		}
	}
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
		base := len(p.stopped[key])
		p.stopped[key] = append(p.stopped[key], fresh...)
		if err := p.saveStopped(); err != nil {
			log.Printf("stopped processes: not stopping %s: %v", thread, err)
			p.stopped[key] = p.stopped[key][:base]
			break
		}
		var okPIDs []int
		for _, pid := range fresh {
			if p.signal(pid, syscall.SIGSTOP) == nil {
				stopped = true
				okPIDs = append(okPIDs, pid)
			}
		}
		if len(okPIDs) != len(fresh) {
			p.stopped[key] = append(p.stopped[key][:base], okPIDs...)
			if len(p.stopped[key]) == 0 {
				delete(p.stopped, key)
			}
			p.saveStopped()
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

// jobPids are the processes under cli whose command line holds needle (already
// normalized with bareCommand), and their descendants.
func jobPids(t procTable, kids map[int][]int, cli int, needle string) []int {
	var out []int
	for _, pid := range under(kids, cli) {
		if pid != cli && strings.Contains(bareCommand(t[pid].args), needle) {
			out = append(out, under(kids, pid)...)
		}
	}
	return out
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
		return jobPids(t, kids, cli, needle)
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
