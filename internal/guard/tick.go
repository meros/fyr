package guard

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// The memory guard's tick (V0.guard-tick): Tick makes every decision the
// guard takes on a tick, from its inputs and its state, and returns them with
// the next state. It freezes the work that grows most while memory is short,
// thaws and wakes what may run again, kills rerunnable work, takes turns
// among the parts of undeclared work, and tells the threads; the engine only
// applies the decisions (through Mechanism and its own records) and turns the
// notices into Inputs.
//
// Some decisions need the host's answer before the tick can go on: whether a
// freeze took, how many processes a kill found, which thread the fast flag
// picked, a reading taken after a freeze. Such a decision is an ask (Ask set).
// Tick stops at the first ask without an answer in Inputs.Answers and returns
// the decisions up to it; the caller applies them, adds the ask's answer and
// calls Tick again with the same state. Tick is deterministic, so the second
// call returns the same decisions first and goes on from the answer. Run does
// that loop.

// Kind is what a Decision does.
type Kind int

const (
	// Log writes Msg to the log; with a Key, once a minute while it repeats.
	Log Kind = iota
	// Record adds Event, with the Signals it was taken on, to the guard's
	// history (decision 0032).
	Record
	// Notify tells Thread's agent of a pause, resume, kill or retry: Args are
	// the slot-pause notice's (decision 0029).
	Notify
	// NotifyMemory tells Thread that memory is short and its job grew most
	// (Why, Grew); nothing is stopped (decision 0029 S2).
	NotifyMemory
	// Freeze freezes Thread's admitted job. Ask: OK, and Mem, the thread's
	// memory read after the freeze.
	Freeze
	// FreezeUndeclared freezes Thread's undeclared work. Ask: OK.
	FreezeUndeclared
	// Thaw thaws Thread's job; with Undeclared, its undeclared work.
	Thaw
	// Rotate lets PID of Thread's Units run and freezes the others (decision
	// 0032 slice 2). With Ask set the answer's OK says whether it took.
	Rotate
	// Kill kills Thread's job, or (Undeclared) its undeclared work. Ask: N,
	// the processes killed.
	Kill
	// FlagNow flags at once the thread whose work grew most
	// (V0.freeze-fast-class-name). Ask: Thread ("" for none) and Flags, the
	// flags read after.
	FlagNow
	// ReadSignals reads the host's signals now. Ask: Signals.
	ReadSignals
	// ReadGrowth reads how much each of Threads' jobs grew. Ask: Growth.
	ReadGrowth
	// ReadTurn reads Thread's work units and read-back counter now. Ask:
	// Units, Faults.
	ReadTurn
	// Raise raises the learned peak of Thread's admitted job's class
	// (V0.rerun-classes), before Release.
	Raise
	// Release gives Thread's admission back: its killed job is admitted again
	// when it reruns.
	Release
	// Thawed says Thread's job thawed At: its idle clock starts there
	// (V0.sim-fixes (1)).
	Thawed
	// RerunWait says Thread's job was killed for Entry and its rerun waits at
	// the gate (V0.rerun-classes).
	RerunWait
	// KeepKills keeps Kills, the rerunnable classes' kills of the last hour,
	// across a restart (V0.survive-update).
	KeepKills
	// ClearGate ends the gate's hold on new heavy commands (V0.guard-wake E1).
	// Ask: Threads, the threads whose commands it held, to be told to retry.
	ClearGate
)

// Decision is one thing the guard decides on a tick. Kind says which fields
// it reads.
type Decision struct {
	Kind       Kind
	Thread     string
	Undeclared bool
	// Ask: the tick waits for the answer (see Tick).
	Ask bool
	// Log: Key ("" logs every time) and Msg.
	Key, Msg string
	// Record.
	Event   resource.GuardEvent
	Signals resource.Signals
	// Notify.
	Args map[string]any
	// NotifyMemory.
	Why  string
	Grew uint64
	// Rotate.
	Units []resource.WorkUnit
	PID   int
	// RerunWait, Thawed.
	Entry string
	At    time.Time
	// ReadGrowth.
	Threads []string
	// KeepKills.
	Kills map[string][]time.Time
}

// Answer is the host's answer to an ask; Decision's kinds say which fields.
type Answer struct {
	OK      bool
	N       int
	Mem     uint64
	Thread  string
	Flags   []resource.Undeclared
	Signals resource.Signals
	Growth  map[string]uint64
	Units   []resource.WorkUnit
	Faults  uint64
	Threads []string
}

// View is what the tick reads of the host besides Inputs. The engine answers
// each question from one reading per tick, so the answers stay the same for
// the whole tick (and every call of Tick in it); a missing dependency answers
// the zero value.
type View interface {
	// Growth is how much a holder's memory grew since its grant.
	Growth(thread string) uint64
	// Mem is a thread's memory now.
	Mem(thread string) uint64
	// Service says thread's heavy work is a service, never frozen (decision
	// 0036 (D)).
	Service(thread string) bool
	// Empty says thread's frozen job (undeclared: work) cgroup holds no
	// process any more.
	Empty(thread string, undeclared bool) bool
	// RootsMem is the memory of the roots of thread's episode of undeclared
	// work; false when unread.
	RootsMem(thread string) (uint64, bool)
	// Units are the parts of thread's undeclared work, nil when the host
	// cannot take turns.
	Units(thread string) []resource.WorkUnit
	// Faults is what thread has read back so far, in bytes.
	Faults(thread string) uint64
	// FrozenUsec is the cgroup's frozen_usec, false where the kernel has none.
	FrozenUsec(thread string, undeclared bool) (uint64, bool)
	// Wall is the learned wall time (p90) of thread's admitted job's class.
	Wall(thread string) (time.Duration, bool)
	// Class is thread's newest heavy class at the gate.
	Class(thread string) string
	// Rerunnable is the owner's entry a class or command matches, "" for none
	// or when this host cannot kill.
	Rerunnable(class, command string) string
	// Exists says the engine knows thread.
	Exists(thread string) bool
}

// Can says which of the host's controls are wired; the guard skips what it
// cannot do.
type Can struct {
	Guard      bool // freeze, thaw and the signals: the tick's freezes at all
	Undeclared bool // freeze and thaw undeclared work
	FlagNow    bool
	Rerun      bool // the owner's rerunnable classes are read
	Kill       bool // and a job can be killed
	Memory     bool // the signals and the jobs' growth: the memory notice
}

// Inputs are what one tick reads.
type Inputs struct {
	Now time.Time
	// Signals are the host's signals at the start of the tick; Trig is the
	// PSI trigger's last firing (V0.freeze-fast-class-name).
	Signals resource.Signals
	Trig    time.Time
	Policy  resource.Policy
	// Held are the pool's holders; Flags the threads that run undeclared
	// heavy work; Running the threads whose agent runs.
	Held    []resource.Holder
	Flags   []resource.Undeclared
	Running []string
	Can     Can
	View    View
	// Answers are the answers to the tick's asks so far, in order.
	Answers []Answer
}

// State is what the guard keeps from one tick to the next.
type State struct {
	// Frozen is what the guard holds frozen now, the newest last.
	Frozen []Frozen
	// FrozeHolds are the holds frozen once already (owner → since).
	FrozeHolds map[string]time.Time
	// UndFroze is, per thread, what the guard froze of its episode.
	UndFroze map[string]UndFreeze
	// Short: memory was short at the last tick, since ShortSince.
	Short      bool
	ShortSince time.Time
	// V0.guard-wake (decision 0036 E3): Cap is the agents' memory when the
	// stall first went critical in this episode; OKSince since when memory is
	// fine (zero: it is not); LastWake the last wake.
	Cap      uint64
	OKSince  time.Time
	LastWake time.Time
	// RerunKills are each rerunnable class's kills (the last hour).
	RerunKills map[string][]time.Time
	// MemTold: the memory notice told a thread in this episode.
	MemTold bool
	// V0.freeze-cycle-cap: each thread's thaws in the last ThawWindow, jobs
	// and undeclared work alike.
	Thaws map[string][]time.Time
}

// clone is s with its own maps and slices, so a tick never writes the
// caller's.
func (s State) clone() State {
	s.Frozen = slices.Clone(s.Frozen)
	s.FrozeHolds = maps.Clone(s.FrozeHolds)
	if s.FrozeHolds == nil {
		s.FrozeHolds = map[string]time.Time{}
	}
	s.UndFroze = maps.Clone(s.UndFroze)
	if s.UndFroze == nil {
		s.UndFroze = map[string]UndFreeze{}
	}
	s.RerunKills = maps.Clone(s.RerunKills)
	s.Thaws = maps.Clone(s.Thaws)
	if s.Thaws == nil {
		s.Thaws = map[string][]time.Time{}
	}
	return s
}

// stop ends a tick at an ask without an answer.
type stop struct{}

// tick is one call of Tick.
type tick struct {
	in    Inputs
	st    State
	out   []Decision
	asked int
	flags []resource.Undeclared
}

// Tick decides one tick of the guard from in and st, and returns the
// decisions in the order they apply and the next state. When it asked more
// than in.Answers answer (Waits), the tick waits for the last decision's
// answer: next is st unchanged, and the caller calls Tick again with the
// answer added to in.Answers.
func Tick(in Inputs, st State) (ds []Decision, next State) {
	t := &tick{in: in, st: st.clone(), flags: in.Flags}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(stop); !ok {
				panic(r)
			}
			ds, next = t.out, st
		}
	}()
	t.freezeGuard()
	t.memoryGuard()
	return t.out, t.st
}

// Run runs one tick to its end: it applies each decision with apply, in
// order and once, answers the asks with what apply returns, and returns the
// next state.
func Run(in Inputs, st State, apply func(Decision) Answer) State {
	done := 0
	for {
		ds, next := Tick(in, st)
		waits := Waits(in, ds)
		for _, d := range ds[done:] {
			a := apply(d)
			if d.Ask {
				in.Answers = append(in.Answers, a)
			}
		}
		done = len(ds)
		if !waits {
			return next
		}
	}
}

// Waits says the tick that returned ds waits for an answer: it asked more
// than in.Answers answer.
func Waits(in Inputs, ds []Decision) bool {
	n := 0
	for _, d := range ds {
		if d.Ask {
			n++
		}
	}
	return n > len(in.Answers)
}

func (t *tick) emit(d Decision) { t.out = append(t.out, d) }

// ask emits d as an ask and returns its answer, or stops the tick.
func (t *tick) ask(d Decision) Answer {
	d.Ask = true
	t.emit(d)
	if t.asked >= len(t.in.Answers) {
		panic(stop{})
	}
	a := t.in.Answers[t.asked]
	t.asked++
	return a
}

func (t *tick) log(format string, a ...any) {
	t.emit(Decision{Kind: Log, Msg: fmt.Sprintf(format, a...)})
}

// once logs a decision that repeats every tick once a minute (guardOnce).
func (t *tick) once(key, msg string) { t.emit(Decision{Kind: Log, Key: key, Msg: msg}) }

func (t *tick) record(ev resource.GuardEvent, sig resource.Signals) {
	t.emit(Decision{Kind: Record, Event: ev, Signals: sig})
}

func (t *tick) notify(thread string, args map[string]any) {
	t.emit(Decision{Kind: Notify, Thread: thread, Args: args})
}

// freezeGuard freezes undeclared work, else the slotted job that grew most
// (freezeVictim), while memory is short: one at first, one more each
// AddFreezeAfter that memory stays short, at most MaxFrozen (V0.guard-wake:
// the running threads less one), never the oldest running job or a service.
// Each freeze thaws when its work or hold ended, after FreezeMax unless the
// stall is critical (V0.oomd-guard), or by the wake rule (wakeable).
func (t *tick) freezeGuard() {
	if !t.in.Can.Guard {
		return
	}
	now, st, v := t.in.Now, &t.st, t.in.View
	// V0.freeze-fast-class-name: a fresh trigger is a critical stall (the
	// sampler says so too; this holds whatever the signals are).
	sig := t.in.Signals.Fresh(t.in.Trig, now)
	// V0.oomd-guard (decision 0036 E2): no timed thaw into a critical stall,
	// the host's or the agents'. V0.guard-agents-stall (decision 0036 B, act
	// before oomd): either makes memory short, and so does the unit's stall
	// near oomd's limit; the host's alone used to.
	mem := memoryState(sig, t.in.Policy)
	critical, nearOomd, short := mem.critical, mem.nearOomd, mem.short
	frozen := slices.Clone(st.Frozen)
	// V0.freeze-fast-class-name: the trigger fired within TriggerFresh.
	fast := sig.Triggered
	wasShort := st.Short
	st.Short = short
	if short && !wasShort {
		st.ShortSince = now
	}
	// V0.guard-wake (decision 0036 E3): the cap is what the agents held when
	// the stall first went critical in this episode; the episode ends when
	// memory is not short and nothing is frozen.
	newCap := false
	if critical && st.Cap == 0 && sig.AgentsMem > 0 {
		st.Cap, newCap = sig.AgentsMem, true
	}
	if !short && len(frozen) == 0 {
		st.Cap = 0
	}
	capNow := st.Cap
	// With a cap the wake waits for the stall alone (below elevated); without
	// one, also for the headroom (reserve + 5 %), as before.
	fine := wakeFine(mem, sig, t.in.Policy, capNow)
	switch {
	case !fine:
		st.OKSince = time.Time{}
	case st.OKSince.IsZero():
		st.OKSince = now
	}
	okSince, lastWake := st.OKSince, st.LastWake
	var retry []string
	if !short && wasShort {
		retry = t.ask(Decision{Kind: ClearGate}).Threads
	}
	if newCap {
		t.log("guard: wake cap agents=%s: the agents' memory when the stall first went critical in this episode %s", resource.GB(float64(capNow)), SignalsLine(sig))
	}
	if short != wasShort {
		state := "recovered"
		if short {
			state = "short"
		}
		t.record(resource.GuardEvent{At: now, Kind: state}, sig)
		t.log("guard: memory %s: %s flagged=%d held=%d running=%d frozen=%q", state, SignalsLine(sig), len(t.flags), len(t.in.Held), len(t.in.Running), FrozenOwners(frozen))
	}
	slices.Sort(retry)
	for _, th := range retry { // V0.guard-wake (E1): the commands the gate held
		t.notify(th, map[string]any{"retry": true})
	}
	var keep []Frozen
	var thaws []thawing
	var pastMax []Frozen // newly held past FreezeMax: told once
	for _, f := range frozen {
		var flagged []resource.Undeclared
		if f.Undeclared {
			flagged = t.flags
		}
		holding := stillHolding(f, flagged, t.in.Held)
		// A freeze whose cgroup holds no process any more (the work ended or
		// was killed) holds nothing; release it at once instead of letting it
		// block every other freeze for ten minutes. V0.guard-tidy: a job's too.
		emptied := v.Empty(f.Owner, f.Undeclared)
		// V0.guard-tidy: past FreezeMax only the agents' own critical stall
		// holds the work (the host's can come from the desktop, which the
		// frozen work does not feed), and not past FreezeCeiling unless the
		// work is rerunnable: then V0.rerun-classes's kill ends it.
		vd := judgeFrozen(mem, frozenFacts{holding: holding, emptied: emptied, age: now.Sub(f.At), rerunnable: now.Sub(f.At) >= FreezeCeiling && t.rerunEntry(f) != ""},
			func() (bool, string) { return t.roomToThaw(f, sig) })
		switch vd.why {
		case whyEnded:
			t.once("ended:"+f.Owner, fmt.Sprintf("keeping thread=%s frozen: its hold or episode ended but its work is still there, and memory stalls critically (%s)", f.Owner, SignalsLine(sig)))
		case whyNoRoom:
			t.once("pastmax:"+f.Owner, fmt.Sprintf("keeping thread=%s frozen past the %s limit: %s; it thaws when it fits, or at %s", f.Owner, FreezeMax, vd.room, FreezeCeiling))
		case whyCritical:
			t.once("pastmax:"+f.Owner, fmt.Sprintf("keeping thread=%s frozen past the %s limit: the agents' memory stalls critically (%s); it thaws when the stall is below critical, or at %s", f.Owner, FreezeMax, SignalsLine(sig), FreezeCeiling))
		}
		switch vd.act {
		case actThaw:
			thaws = append(thaws, thawing{f: f, holding: holding, emptied: emptied, ceiling: vd.ceiling})
			continue
		case actKeepPastMax:
			if !f.PastMax {
				f.PastMax = true
				pastMax = append(pastMax, f)
			}
		}
		keep = append(keep, f)
	}
	// V0.rerun-classes: a rerunnable freeze that could not wake within
	// RerunAfter is killed and run again.
	var killed int
	if t.in.Can.Rerun && len(keep) > 0 {
		var still []Frozen
		for _, f := range keep {
			if now.Sub(f.At) >= RerunAfter {
				if entry := t.rerunEntry(f); entry != "" && t.rerunKill(f, entry, fmt.Sprintf("frozen %s and could not wake", RerunAfter), sig) {
					killed++
					continue
				}
			}
			still = append(still, f)
		}
		keep = still
	}
	// V0.guard-wake (decision 0036 E3, Buck2's rule): the oldest freeze wakes
	// first, at most one each WakeEvery, once the stall has been below
	// elevated for ThawAfterOK and it fits under the cap.
	// V0.sim-fixes (3): when the frozen work is all the heavy work left (the
	// rest ended), the oldest freeze wakes at once unless memory stalls
	// critically: nothing runs that it could take memory from.
	if wakesAlone(len(keep), len(thaws), mem, now.Sub(lastWake), func() int { return len(RunningHeavy(t.in.Held, t.flags, frozen)) }) {
		thaws = append(thaws, thawing{f: keep[0], holding: true, woke: "no other heavy work runs"})
		keep = keep[1:]
		st.LastWake = now
	}
	if wakeDue(len(keep), len(thaws), okSince, lastWake, now) {
		f := keep[0]
		resident := v.Mem(f.Owner) // V0.wake-once: what of the agents' memory is the frozen thread's own
		w := f
		if f.Active != 0 {
			w.Mem, resident = t.frozenParts(f)
		}
		if ok, why := wakeable(w, sig, capNow, resident); ok {
			keep = keep[1:]
			thaws = append(thaws, thawing{f: f, holding: true, woke: why})
			st.LastWake = now
		} else {
			t.once("wake:"+f.Owner, fmt.Sprintf("not waking thread=%s yet: %s", f.Owner, why))
		}
	}
	for i := range keep {
		keep[i] = t.nextTurn(keep[i])
	}
	st.Frozen = keep
	for _, th := range thaws {
		st.Thaws[th.f.Owner] = append(slices.DeleteFunc(slices.Clone(st.Thaws[th.f.Owner]), func(at time.Time) bool { return now.Sub(at) >= ThawWindow }), now)
		if !th.f.Undeclared {
			t.emit(Decision{Kind: Thawed, Thread: th.f.Owner, At: now})
		}
		if th.f.Undeclared {
			u := st.UndFroze[th.f.Owner]
			u.ThawedAt = now
			st.UndFroze[th.f.Owner] = u
		}
	}
	for _, th := range thaws {
		f := th.f
		reason, why := thawWords(th, now)
		kind, work := "job", "job"
		if f.Undeclared {
			kind, work = "undeclared work", "undeclared"
		}
		held := t.frozenFor(f, now) // read before the thaw: the cgroup may go with it
		t.record(resource.GuardEvent{At: now, Kind: "thaw", Work: work, Thread: f.Owner, Reason: why}, sig)
		t.log("guard: thawed %s thread=%s after=%s frozen=%s reason=%q %s still_frozen=%d", kind, f.Owner, now.Sub(f.At).Round(time.Second), held.Round(time.Second), reason, SignalsLine(sig), len(keep))
		t.emit(Decision{Kind: Thaw, Thread: f.Owner, Undeclared: f.Undeclared})
		if th.holding {
			d := map[string]any{"resumed": true, "frozen": frozenWords(held)}
			if f.Undeclared {
				d["work"] = true
			}
			t.notify(f.Owner, d)
		}
	}
	for _, f := range pastMax {
		// V0.guard-tidy (decision 0032 "What the log gives"): the owner's
		// history shows the hold too.
		work := "job"
		if f.Undeclared {
			work = "undeclared"
		}
		t.record(resource.GuardEvent{At: now, Kind: "kept", Work: work, Thread: f.Owner}, sig)
		t.notify(f.Owner, map[string]any{"waits": true, "work": f.Undeclared})
	}
	switch freezeGate(mem, Stalls(sig), len(thaws)+killed) {
	case gateQuiet:
		return // a thaw or a kill this tick: the next tick sees what it did
	case gateNoStall:
		t.once("nostall", fmt.Sprintf("not freezing on the headroom alone: memory does not stall (%s); new heavy commands wait at the gate", SignalsLine(sig)))
		return
	}
	// With one thread running (n is 0) the freezes below say themselves why
	// they skip it.
	// V0.guard-agents-stall: near oomd's limit the next freeze does not wait
	// AddFreezeAfter; oomd kills after its duration (20 s), sooner than that.
	n := MaxFrozen(t.in.Held, t.flags)
	var lastFrozen time.Time
	if len(keep) > 0 {
		lastFrozen = keep[len(keep)-1].At
	}
	// V0.freeze-cap-holds-memory: only a freeze that holds memory fills a slot.
	// A frozen esbuild of 0.0 GB, or work taking turns (a part runs), frees
	// nothing, yet it used to stop the freeze of the thread that held 7 GB.
	if block, say := freezeRoom(len(keep), HoldingFreezes(keep), n, lastFrozen, now, nearOomd); block {
		if say {
			t.once("maxfrozen", fmt.Sprintf("not freezing more: %d frozen that hold memory, at most %d (threads with heavy work less one)", HoldingFreezes(keep), n))
		}
		return
	}
	admitted := len(t.in.Held) > 0
	if t.freezeUndeclared(sig, admitted, RunningHeavy(t.in.Held, t.flags, st.Frozen)) {
		return
	}
	// V0.freeze-fast-class-name: on 20 Nov the PSI trigger fired at 07:32:12
	// and the work was frozen at 07:33:01, as the desktop froze. Within
	// TriggerFresh of the trigger the work that grew most is flagged now, not
	// a window (45 s) later, and freezeUndeclared's rules (services, the share
	// it holds, MaxFreezes, someone gains) decide as for any flag.
	if fast && t.in.Can.FlagNow {
		if a := t.ask(Decision{Kind: FlagNow}); a.Thread != "" {
			t.flags = a.Flags
			why := "the PSI trigger fired"
			if sig.Provider != "" && sig.Provider != "cgroups" {
				why = "memory stalled critically"
			}
			t.log("guard: flagged thread=%s without the window: %s %s ago %s", a.Thread, why, now.Sub(t.in.Trig).Round(time.Second), SignalsLine(sig))
			// V0.sim-fixes (3): the early flag is heavy work too, and the
			// only heavy work that runs is still never frozen whole.
			if t.freezeUndeclared(sig, admitted, RunningHeavy(t.in.Held, t.flags, st.Frozen)) {
				return
			}
		}
	}
	t.freezeJob(sig)
}

// freezeJob freezes the job that grew most, while two or more run: one job
// always runs.
func (t *tick) freezeJob(sig resource.Signals) {
	now, st, v := t.in.Now, &t.st, t.in.View
	var running []resource.Holder
	for _, h := range t.in.Held {
		if !IsFrozen(st.Frozen, h.Owner) {
			running = append(running, h)
		}
	}
	if len(running) < 2 {
		return
	}
	victim, ok := t.freezeVictim(running)
	if !ok {
		return
	}
	usec, hasUsec := v.FrozenUsec(victim.Owner, false)
	a := t.ask(Decision{Kind: Freeze, Thread: victim.Owner})
	if !a.OK {
		return
	}
	why := Why(sig, t.in.Policy)
	fj := Frozen{Owner: victim.Owner, Since: victim.Since.Format(time.RFC3339Nano), At: now, Usec: usec, HasUsec: hasUsec, Mem: a.Mem}
	fj.Class = v.Class(victim.Owner)
	st.FrozeHolds[victim.Owner] = victim.Since
	// V0.rerun-classes: a short rerunnable job is killed instead: running it
	// again later costs less than holding its memory frozen.
	if entry := t.rerunEntry(fj); entry != "" {
		if wall, short := t.shortJob(victim.Owner); short && t.rerunKill(fj, entry, fmt.Sprintf("would be frozen; it runs %s", wall.Round(time.Second)), sig) {
			return
		}
	}
	st.Frozen = append(st.Frozen, fj)
	// V0.guard-tidy: the fields of `froze undeclared work` (decision 0032
	// "What the log gives for tuning"). A hold is frozen once (FrozeHolds), so
	// its freeze is 1/1; its episode is the hold, its command its class.
	cmd, cores := fj.Class, 0.0
	if cmd == "" {
		cmd = "unknown command"
	}
	if victim.Need != nil {
		cores = victim.Need.CPU
	}
	t.log("guard: froze job thread=%s freeze=1/1 episode_age=%s cmd=%q why=%q mem=%s cores=%.1f %s held=%d running=%d frozen_now=%d",
		victim.Owner, now.Sub(victim.Since).Round(time.Second), cmd, why, resource.GB(float64(fj.Mem)), cores, SignalsLine(sig), len(t.in.Held), len(t.in.Running), len(st.Frozen))
	t.record(resource.GuardEvent{At: now, Kind: "freeze", Work: "job", Thread: victim.Owner}, sig)
	t.notify(victim.Owner, map[string]any{"paused": why})
}

// cycled says thread was thawed MaxThawsPerHour times within ThawWindow
// (V0.freeze-cycle-cap): it is let run, not frozen a fourth time.
func (t *tick) cycled(thread string) bool {
	n := 0
	for _, at := range t.st.Thaws[thread] {
		if t.in.Now.Sub(at) < ThawWindow {
			n++
		}
	}
	return n >= MaxThawsPerHour
}

// freezeVictim is the job to freeze: of the holds not frozen yet, the one whose
// memory grew most (V0.load-tuning; freezing a job that uses none frees nothing
// and stops nothing), the newest of equals. V0.guard-wake (decision 0036 E4,
// Buck2's "forward progress"): never the oldest hold, which always runs, and
// never a service.
func (t *tick) freezeVictim(held []resource.Holder) (resource.Holder, bool) {
	var best resource.Holder
	var bestGrew uint64
	found := false
	oldest := held[0]
	for _, h := range held[1:] {
		if h.Since.Before(oldest.Since) {
			oldest = h
		}
	}
	for _, h := range held {
		done := t.st.FrozeHolds[h.Owner].Equal(h.Since)
		if h.Priority || done || t.cycled(h.Owner) || h.Owner == oldest.Owner || t.in.View.Service(h.Owner) {
			continue
		}
		grew := t.in.View.Growth(h.Owner)
		if !found || grew > bestGrew || (grew == bestGrew && h.Since.After(best.Since)) {
			best, bestGrew, found = h, grew, true
		}
	}
	return best, found
}

// freezeUndeclared freezes the newest flagged thread whose memory is over
// (V0.164 section 7), before any job that asked; not one already frozen
// (V0.multi-freeze: freezeGuard decides when another may be).
// Decision 0032 slice 1 changes two once-only rules of V0.164 that left a long
// episode unprotected: the work is frozen when anyone else would gain from it
// (an admitted job, another flagged thread, or any other thread that runs:
// its agent starts slowly or not at all in a thrashing cgroup), not only when
// a job or a second flag exists; and an episode is frozen again after a thaw,
// RefreezeAfter later and at most MaxFreezes times, so the freezes work as time
// slices of the long episode. It reports whether it froze one.
func (t *tick) freezeUndeclared(sig resource.Signals, admitted bool, heavy map[string]time.Time) bool {
	if !t.in.Can.Undeclared {
		return false
	}
	now, st, v := t.in.Now, &t.st, t.in.View
	fl := t.flags
	if len(fl) == 0 {
		return false
	}
	others := func(thread string) int {
		n := 0
		for _, id := range t.in.Running {
			if id != thread {
				n++
			}
		}
		return n
	}
	var pick *resource.Undeclared
	var picked UndFreeze
	var skips [][2]string // key, line
	svc := map[string]bool{}
	held := map[string]uint64{}
	turnable := map[string]bool{} // two or more parts: one keeps running
	for _, f := range fl {
		svc[f.Thread] = v.Service(f.Thread)
		if f.MemOver {
			turnable[f.Thread] = len(v.Units(f.Thread)) >= 2
		}
		// V0.guard-tidy: what the episode's processes hold now. The kernel does
		// not move memory with a process, so a thread's flagged memory can be
		// charged where its work no longer is (cli/), and freezing the work
		// frees none of it.
		if f.MemOver {
			if m, ok := v.RootsMem(f.Thread); ok {
				held[f.Thread] = m + 1 // 0: not read
			}
		}
	}
	for i := range fl {
		f := &fl[i]
		if !f.MemOver || !v.Exists(f.Thread) || IsFrozen(st.Frozen, f.Thread) {
			continue
		}
		if svc[f.Thread] {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: its work is a service", f.Thread)})
			continue
		}
		if m := held[f.Thread]; m > 0 && float64(m-1) < WorkMemShare*float64(f.Mem) {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: the work holds little of the thread's memory (its processes %s of %s)", f.Thread, resource.GB(float64(m-1)), resource.GB(float64(f.Mem)))})
			continue
		}
		if t.cycled(f.Thread) {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: thawed %d times in the last %s, letting it run", f.Thread, MaxThawsPerHour, ThawWindow)})
			continue
		}
		prev := st.UndFroze[f.Thread]
		if prev.Episode.Equal(f.Since) {
			if prev.N >= MaxFreezes {
				skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: frozen %d times in this episode, letting it run", f.Thread, prev.N)})
				continue
			}
			if !prev.ThawedAt.IsZero() && now.Sub(prev.ThawedAt) < RefreezeAfter {
				continue // the slice has not run long enough yet; says nothing
			}
		}
		if !admitted && len(fl) < 2 && others(f.Thread) == 0 {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: nobody else would gain (no job, no other flag, no other thread running)", f.Thread)})
			continue
		}
		// V0.sim-fixes (3): the last heavy work that runs is never frozen
		// whole (one always runs, decision 0029 section 4, Buck2's rule); work
		// of two or more parts takes turns, so one of its parts still runs. An
		// agent with no heavy work is no reason to stop all of it: the frozen
		// memory stayed charged, and only the 10 min thaw ran the work again.
		if !turnable[f.Thread] && len(heavy) < 2 {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: its work is the only heavy work that runs; one always runs", f.Thread)})
			continue
		}
		if pick == nil || f.Since.After(pick.Since) {
			pick, picked = f, prev
		}
	}
	for _, k := range skips {
		t.once("skip:"+k[0], k[1])
	}
	if pick == nil {
		return false
	}
	usec, hasUsec := v.FrozenUsec(pick.Thread, true)
	first, units, turns := t.startTurns(pick.Thread)
	if !turns && !t.ask(Decision{Kind: FreezeUndeclared, Thread: pick.Thread}).OK {
		t.once("skip:"+pick.Thread, fmt.Sprintf("could not freeze thread=%s: no process to move or the cgroup refused", pick.Thread))
		return false
	}
	why := Why(sig, t.in.Policy)
	n := 1
	if picked.Episode.Equal(pick.Since) {
		n = picked.N + 1
	}
	fj := Frozen{Owner: pick.Thread, At: now, Undeclared: true, Episode: pick.Since, Mem: pick.Mem, Usec: usec, HasUsec: hasUsec, Cmd: pick.Command}
	if turns {
		r := t.ask(Decision{Kind: ReadTurn, Thread: pick.Thread}) // read after the freeze
		fj.Active, fj.TurnAt, fj.Turn, fj.Faults = first.PID, now, TurnMin, r.Faults
		fj.Parts = map[int]uint64{}
		for _, u := range r.Units {
			if u.PID != first.PID {
				fj.Parts[u.PID] = u.RSS
			}
		}
	}
	st.Frozen = append(st.Frozen, fj)
	st.UndFroze[pick.Thread] = UndFreeze{Episode: pick.Since, N: n}
	if turns {
		t.log("guard: taking turns thread=%s units=%d first=%d cmd=%q mem=%s", pick.Thread, units, first.PID, first.Command, resource.GB(float64(first.RSS)))
	}
	cmd := pick.Command
	if cmd == "" {
		cmd = "unknown command"
	}
	t.log("guard: froze undeclared work thread=%s freeze=%d/%d episode_age=%s cmd=%q mem=%s cores=%.1f %s flagged=%d admitted=%t others_running=%d frozen_now=%d",
		pick.Thread, n, MaxFreezes, now.Sub(pick.Since).Round(time.Second), cmd, resource.GB(float64(pick.Mem)), pick.CPU, SignalsLine(sig), len(fl), admitted, others(pick.Thread), len(st.Frozen))
	t.record(resource.GuardEvent{At: now, Kind: "freeze", Work: "undeclared", Thread: pick.Thread, N: n}, sig)
	t.notify(pick.Thread, map[string]any{"paused": why, "work": cmd})
	return true
}

// startTurns freezes all of thread's units but the first that takes turns;
// false when the work has fewer than two, or the cgroups refused, and is
// frozen whole instead.
func (t *tick) startTurns(thread string) (resource.WorkUnit, int, bool) {
	units := t.in.View.Units(thread)
	if len(units) < 2 {
		return resource.WorkUnit{}, 0, false
	}
	first := turnTakers(units)[0]
	if !t.ask(Decision{Kind: Rotate, Thread: thread, Units: units, PID: first.PID}).OK {
		return resource.WorkUnit{}, 0, false
	}
	return first, len(units), true
}

// nextTurn runs at each tick of a freeze that takes turns. Every tick it
// applies the turn again, so a part started since is frozen too; when the turn
// is over, or its part ended, the next part in pid order gets one, as long as
// the swap-in rate measured during this turn says.
func (t *tick) nextTurn(f Frozen) Frozen {
	if f.Active == 0 {
		return f
	}
	now := t.in.Now
	units := t.in.View.Units(f.Owner)
	if len(units) == 0 {
		return f
	}
	at := -1
	for i, u := range units {
		if u.PID == f.Active {
			at = i
		}
	}
	over := now.Sub(f.TurnAt) >= f.Turn
	if at >= 0 && !over {
		t.emit(Decision{Kind: Rotate, Thread: f.Owner, Units: units, PID: f.Active})
		return f
	}
	takers := turnTakers(units)
	next := takers[0]
	for _, u := range takers {
		if u.PID > f.Active {
			next = u
			break
		}
	}
	faults := t.in.View.Faults(f.Owner)
	rate := 0.0
	if s := now.Sub(f.TurnAt).Seconds(); s > 0 && faults > f.Faults {
		rate = float64(faults-f.Faults) / s
	}
	turn := turnLength(next.RSS, rate)
	if !t.ask(Decision{Kind: Rotate, Thread: f.Owner, Units: units, PID: next.PID}).OK && len(units) > 1 {
		t.once("turn:"+f.Owner, "could not move the turn in thread="+f.Owner+": the cgroup refused")
		return f
	}
	// V0.sim-fixes (4): each frozen part's memory at its freeze, for the wake
	// rule; a part that ended is gone from it.
	parts := map[int]uint64{}
	for _, u := range units {
		switch {
		case u.PID == next.PID:
		case u.PID == f.Active:
			parts[u.PID] = u.RSS
		default:
			parts[u.PID] = max(f.Parts[u.PID], u.RSS)
		}
	}
	f.Parts = parts
	if next.PID != f.Active {
		t.log("guard: turn thread=%s pid=%d cmd=%q mem=%s turn=%s units=%d read_back=%s/s", f.Owner, next.PID, next.Command,
			resource.GB(float64(next.RSS)), turn.Round(time.Second), len(units), resource.GB(rate))
	}
	f.Active, f.TurnAt, f.Turn, f.Faults = next.PID, now, turn, faults
	return f
}

// frozenParts is, for work taking turns, the frozen parts' memory at their
// freeze and their memory now (V0.sim-fixes (4)). The thread's memory at the
// freeze (f.Mem) counted the part whose turn it is, which runs and is in the
// agents' memory already, and the parts that ended since: with one part left
// the work could never wake ("agents=0.8 GB + its 11.2 GB >= cap=12.0 GB").
func (t *tick) frozenParts(f Frozen) (atFreeze, now uint64) {
	for _, u := range t.in.View.Units(f.Owner) {
		if u.PID == f.Active {
			continue
		}
		at, ok := f.Parts[u.PID]
		if !ok {
			at = u.RSS // started since: frozen at the last turn's apply
		}
		atFreeze += max(at, u.RSS)
		now += u.RSS
	}
	return atFreeze, now
}

// roomToThaw says whether f's work fits back above the reserve: what of it is
// out of RAM now (its memory at the freeze less the thread's memory now) comes
// back with the thaw (V0.sim-fixes (3)). Without a headroom, a budget or f's
// memory it fits.
func (t *tick) roomToThaw(f Frozen, sig resource.Signals) (bool, string) {
	if sig.Headroom == nil || sig.Memory == 0 || f.Mem == 0 {
		return true, ""
	}
	resident := t.in.View.Mem(f.Owner)
	back := float64(f.Mem - min(resident, f.Mem))
	room := (*sig.Headroom - sig.Reserve(t.in.Policy)) * float64(sig.Memory)
	if back <= room {
		return true, ""
	}
	return false, fmt.Sprintf("its %s out of RAM would come back, %s free above the reserve", resource.GB(back), resource.GB(max(0, room)))
}

// frozenFor is how long f was frozen (V0.guard-wake, decision 0036 E6): the
// cgroup's frozen_usec since the freeze where the kernel has it, else the
// wall time since the freeze.
func (t *tick) frozenFor(f Frozen, now time.Time) time.Duration {
	wall := now.Sub(f.At)
	if !f.HasUsec || f.Active != 0 { // taking turns: the parts thaw in turn
		return wall
	}
	u, ok := t.in.View.FrozenUsec(f.Owner, f.Undeclared)
	if !ok || u <= f.Usec {
		return wall
	}
	return time.Duration(u-f.Usec) * time.Microsecond
}

// rerunEntry is the owner's entry that makes f's work rerunnable now, "" when
// it is not: no entry matches, it is a service, or its class is capped.
func (t *tick) rerunEntry(f Frozen) string {
	if !t.in.Can.Kill || t.in.View.Service(f.Owner) {
		return ""
	}
	entry := t.in.View.Rerunnable(f.Class, f.Cmd)
	if entry == "" {
		return ""
	}
	if ks := recent(t.st.RerunKills[entry], t.in.Now); len(ks) >= RerunCap {
		until := ks[len(ks)-RerunCap].Add(RerunCapFor)
		t.once("rerun-cap:"+entry, fmt.Sprintf("not killing class=%q: killed %d times in an hour; frozen instead until %s", entry, RerunCap, until.Format("15:04")))
		return ""
	}
	return entry
}

// shortJob says thread's admitted job usually ends within RerunShort, so a
// rerun costs less than a freeze.
func (t *tick) shortJob(thread string) (time.Duration, bool) {
	w, ok := t.in.View.Wall(thread)
	return w, ok && w > 0 && w < RerunShort
}

// rerunKill kills f's work for entry, says why, and arranges its rerun; false
// when nothing was killed (the freeze then stays).
func (t *tick) rerunKill(f Frozen, entry, why string, sig resource.Signals) bool {
	now, st := t.in.Now, &t.st
	n := t.ask(Decision{Kind: Kill, Thread: f.Owner, Undeclared: f.Undeclared}).N
	if n == 0 {
		t.once("kill:"+f.Owner, fmt.Sprintf("could not kill thread=%s class=%q: no process left in its cgroup", f.Owner, entry))
		return false
	}
	kind, work := "job", "job"
	if f.Undeclared {
		kind, work = "undeclared work", "undeclared"
	}
	if st.RerunKills == nil {
		st.RerunKills = map[string][]time.Time{}
	}
	ks := append(recent(st.RerunKills[entry], now), now)
	st.RerunKills[entry] = ks
	kept := map[string][]time.Time{}
	for e, ks := range st.RerunKills {
		if ks = recent(ks, now); len(ks) > 0 {
			kept[e] = ks
		} else {
			delete(st.RerunKills, e)
		}
	}
	t.emit(Decision{Kind: KeepKills, Kills: kept})
	t.emit(Decision{Kind: RerunWait, Thread: f.Owner, Entry: entry})
	if f.Undeclared {
		u := st.UndFroze[f.Owner]
		u.ThawedAt = now
		st.UndFroze[f.Owner] = u
	}
	t.log("guard: killed %s thread=%s class=%q why=%q procs=%d mem=%s frozen_for=%s kills_this_hour=%d %s", kind, f.Owner, entry, why, n, resource.GB(float64(f.Mem)), now.Sub(f.At).Round(time.Second), len(ks), SignalsLine(sig))
	t.record(resource.GuardEvent{At: now, Kind: "kill", Work: work, Thread: f.Owner, Class: entry}, sig)
	if len(ks) == RerunCap {
		t.log("guard: class=%q killed %d times in an hour: frozen instead of killed until %s", entry, RerunCap, ks[0].Add(RerunCapFor).Format("15:04"))
		t.record(resource.GuardEvent{At: now, Kind: "capped", Class: entry}, sig)
	}
	if !f.Undeclared {
		t.emit(Decision{Kind: Raise, Thread: f.Owner})   // while it still holds the admission: its class is known
		t.emit(Decision{Kind: Release, Thread: f.Owner}) // the rerun is admitted again
	}
	t.notify(f.Owner, map[string]any{"killed": entry, "work": f.Undeclared})
	return true
}

// memoryGuard tells the holder whose job grew most, once per episode, when
// memory is short (decision 0029 S2); nothing is stopped. It reads the
// signals and the growth after the freezes above.
func (t *tick) memoryGuard() {
	if !t.in.Can.Memory {
		return
	}
	sig := t.ask(Decision{Kind: ReadSignals}).Signals
	why := ""
	if sig.Provider != "" && sig.Headroom != nil && *sig.Headroom < sig.Reserve(t.in.Policy) {
		why = sig.MemoryWhy()
	} else if crit, w := sig.Critical(); crit { // V0.guard-agents-stall: the agents' stall too
		why = w
	}
	was := t.st.MemTold
	t.st.MemTold = why != ""
	if why == "" || was {
		return
	}
	var ths []string
	for _, h := range t.in.Held {
		ths = append(ths, h.Owner)
	}
	grew := t.ask(Decision{Kind: ReadGrowth, Threads: ths}).Growth
	owner, most := "", uint64(0)
	for _, h := range t.in.Held {
		if g := grew[h.Owner]; !h.Priority && g > most {
			owner, most = h.Owner, g
		}
	}
	if owner == "" {
		return
	}
	// V0.guard-tidy: a thread the guard froze heard why in the pause notice;
	// this one says "nothing is stopped", so it would contradict it.
	if IsFrozen(t.st.Frozen, owner) {
		return
	}
	t.emit(Decision{Kind: NotifyMemory, Thread: owner, Why: why, Grew: most})
}
