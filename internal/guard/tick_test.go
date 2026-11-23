package guard

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// V0.guard-tick: Tick's decisions on their own, with a fake host. The engine
// tests (and the load simulation) cover the same rules end to end.

type fakeView struct {
	growth  map[string]uint64
	mem     map[string]uint64
	service map[string]bool
	empty   map[string]bool
	units   map[string][]resource.WorkUnit
	rerun   string // the entry every class matches
	wall    time.Duration
	unknown map[string]bool
}

func (v *fakeView) Growth(th string) uint64                { return v.growth[th] }
func (v *fakeView) Mem(th string) uint64                   { return v.mem[th] }
func (v *fakeView) Service(th string) bool                 { return v.service[th] }
func (v *fakeView) Empty(th string, _ bool) bool           { return v.empty[th] }
func (v *fakeView) RootsMem(string) (uint64, bool)         { return 0, false }
func (v *fakeView) Units(th string) []resource.WorkUnit    { return v.units[th] }
func (v *fakeView) Faults(string) uint64                   { return 0 }
func (v *fakeView) FrozenUsec(string, bool) (uint64, bool) { return 0, false }
func (v *fakeView) Wall(string) (time.Duration, bool)      { return v.wall, v.wall > 0 }
func (v *fakeView) Class(string) string                    { return "make test" }
func (v *fakeView) Rerunnable(string, string) string       { return v.rerun }
func (v *fakeView) Exists(th string) bool                  { return !v.unknown[th] }

var t0 = time.Date(2026, 11, 20, 7, 0, 0, 0, time.UTC)

// critical is a host whose agents' memory stalls critically with 4 % free.
func critical() resource.Signals {
	return resource.Signals{Provider: "cgroups", Headroom: f(0.04), Stall: resource.StallElevated, AgentsStall: resource.StallCritical, CPU: 8, Memory: 16e9}
}

func calm() resource.Signals {
	return resource.Signals{Provider: "cgroups", Headroom: f(0.6), Stall: resource.StallOK, AgentsStall: resource.StallOK, CPU: 8, Memory: 16e9}
}

func inputs(v *fakeView, sig resource.Signals, held ...resource.Holder) Inputs {
	return Inputs{Now: t0, Signals: sig, Policy: resource.DefaultPolicy, Held: held, View: v,
		Can: Can{Guard: true, Undeclared: true, Rerun: true, Kill: true, Memory: true}}
}

// host answers the asks: ok for each freeze, kill for each kill, and the
// signals sig.
type host struct {
	ok      bool
	kill    int
	sig     resource.Signals
	gate    []string
	applied []Decision
}

func (h *host) apply(d Decision) Answer {
	h.applied = append(h.applied, d)
	switch d.Kind {
	case Freeze, FreezeUndeclared, Rotate:
		return Answer{OK: h.ok, Mem: 2e9}
	case Kill:
		return Answer{N: h.kill}
	case ReadSignals:
		return Answer{Signals: h.sig}
	case ReadGrowth:
		return Answer{Growth: map[string]uint64{}}
	case ClearGate:
		return Answer{Threads: h.gate}
	}
	return Answer{}
}

func (h *host) kinds() []Kind {
	var out []Kind
	for _, d := range h.applied {
		if d.Kind != Log && d.Kind != Record {
			out = append(out, d.Kind)
		}
	}
	return out
}

func (h *host) notices(thread string) []map[string]any {
	var out []map[string]any
	for _, d := range h.applied {
		if d.Kind == Notify && d.Thread == thread {
			out = append(out, d.Args)
		}
	}
	return out
}

func hold(owner string, ago time.Duration) resource.Holder {
	return resource.Holder{Owner: owner, Kind: resource.Test, Since: t0.Add(-ago)}
}

// Tick stops at its first ask without an answer and leaves the state alone;
// given the answer it returns the same decisions first and goes on.
func TestTickWaitsForItsAsk(t *testing.T) {
	v := &fakeView{growth: map[string]uint64{"a": 1e9, "b": 3e9}}
	in := inputs(v, critical(), hold("a", time.Hour), hold("b", time.Minute))
	ds, next := Tick(in, State{})
	if !Waits(in, ds) || ds[len(ds)-1].Kind != Freeze || ds[len(ds)-1].Thread != "b" {
		t.Fatalf("not waiting at the freeze of b: %+v", ds)
	}
	if next.Short || len(next.Frozen) != 0 {
		t.Fatalf("a waiting tick changed the state: %+v", next)
	}
	in.Answers = []Answer{{OK: true, Mem: 2e9}}
	ds2, next := Tick(in, State{})
	if !reflect.DeepEqual(ds2[:len(ds)], ds) {
		t.Fatal("the answered tick did not repeat its first decisions")
	}
	if !Waits(in, ds2) || ds2[len(ds2)-1].Kind != ReadSignals { // the memory notice reads after the freezes
		t.Fatalf("want the memory notice's read next: %+v", ds2[len(ds2)-1])
	}
	_ = next
}

// The job that grew most is frozen, never the oldest hold, and its thread is
// told; a refused freeze changes nothing.
func TestTickFreezesTheJobThatGrewMost(t *testing.T) {
	v := &fakeView{growth: map[string]uint64{"a": 9e9, "b": 1e9, "c": 3e9}}
	in := inputs(v, critical(), hold("a", time.Hour), hold("b", 2*time.Minute), hold("c", time.Minute))
	h := &host{ok: true, sig: critical()}
	st := Run(in, State{}, h.apply)
	if len(st.Frozen) != 1 || st.Frozen[0].Owner != "c" || st.Frozen[0].Mem != 2e9 || st.Frozen[0].Class != "make test" {
		t.Fatalf("frozen %+v", st.Frozen)
	}
	if !st.Short || !st.FrozeHolds["c"].Equal(t0.Add(-time.Minute)) {
		t.Fatalf("state %+v", st)
	}
	if n := h.notices("c"); len(n) != 1 || n[0]["paused"] == nil {
		t.Fatalf("c was not told: %+v", n)
	}
	h = &host{ok: false, sig: critical()}
	st = Run(in, State{}, h.apply)
	if len(st.Frozen) != 0 || len(st.FrozeHolds) != 0 || len(h.notices("c")) != 0 {
		t.Fatalf("a refused freeze left %+v, told %+v", st, h.notices("c"))
	}
}

// A service and a priority hold are never frozen; with one job left that
// runs, nothing is.
func TestTickNeverFreezesAServiceOrTheLastJob(t *testing.T) {
	v := &fakeView{growth: map[string]uint64{"b": 3e9}, service: map[string]bool{"b": true}}
	in := inputs(v, critical(), hold("a", time.Hour), hold("b", time.Minute))
	h := &host{ok: true, sig: critical()}
	if st := Run(in, State{}, h.apply); len(st.Frozen) != 0 || slices.Contains(h.kinds(), Freeze) {
		t.Fatalf("froze a service: %+v", st.Frozen)
	}
	in = inputs(&fakeView{}, critical(), hold("a", time.Hour))
	h = &host{ok: true, sig: critical()}
	if st := Run(in, State{}, h.apply); len(st.Frozen) != 0 {
		t.Fatalf("froze the only job: %+v", st.Frozen)
	}
}

// Undeclared work over its memory goes before any job (V0.164), and is
// counted per episode (decision 0032).
func TestTickFreezesUndeclaredWorkFirst(t *testing.T) {
	v := &fakeView{growth: map[string]uint64{"b": 3e9}}
	in := inputs(v, critical(), hold("a", time.Hour), hold("b", time.Minute))
	in.Flags = []resource.Undeclared{{Thread: "u", Since: t0.Add(-time.Minute), MemOver: true, Mem: 4e9, Command: "cargo build"}}
	h := &host{ok: true, sig: critical()}
	st := Run(in, State{}, h.apply)
	if len(st.Frozen) != 1 || !st.Frozen[0].Undeclared || st.Frozen[0].Owner != "u" || st.Frozen[0].Cmd != "cargo build" {
		t.Fatalf("frozen %+v", st.Frozen)
	}
	if u := st.UndFroze["u"]; u.N != 1 || !u.Episode.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("episode %+v", u)
	}
	if slices.Contains(h.kinds(), Freeze) {
		t.Fatal("a job was frozen beside the undeclared work in one tick")
	}
}

// A frozen job thaws at FreezeMax when memory is calm and it fits, and its
// idle clock starts at the thaw.
func TestTickThawsAtTheLimit(t *testing.T) {
	v := &fakeView{}
	b := hold("b", time.Hour)
	in := inputs(v, calm(), hold("a", 2*time.Hour), b)
	st := State{Frozen: []Frozen{{Owner: "b", Since: b.Since.Format(time.RFC3339Nano), At: t0.Add(-FreezeMax)}}, Short: true}
	h := &host{sig: calm()}
	st = Run(in, st, h.apply)
	if len(st.Frozen) != 0 {
		t.Fatalf("still frozen %+v", st.Frozen)
	}
	want := []Kind{ClearGate, Thawed, Thaw, Notify, ReadSignals}
	if got := h.kinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions %v want %v", got, want)
	}
	if n := h.notices("b"); n[0]["resumed"] != true {
		t.Fatalf("notice %+v", n)
	}
}

// While the agents' memory stalls critically a job past FreezeMax stays
// frozen and its thread is told once.
func TestTickKeepsPastTheLimitInACriticalStall(t *testing.T) {
	b := hold("b", time.Hour)
	in := inputs(&fakeView{}, critical(), hold("a", 2*time.Hour), b)
	st := State{Frozen: []Frozen{{Owner: "b", Since: b.Since.Format(time.RFC3339Nano), At: t0.Add(-FreezeMax)}}, Short: true}
	h := &host{sig: critical()}
	st = Run(in, st, h.apply)
	if len(st.Frozen) != 1 || !st.Frozen[0].PastMax {
		t.Fatalf("frozen %+v", st.Frozen)
	}
	if n := h.notices("b"); len(n) != 1 || n[0]["waits"] != true {
		t.Fatalf("told %+v", n)
	}
	h = &host{sig: critical()}
	Run(in, st, h.apply)
	if n := h.notices("b"); len(n) != 0 {
		t.Fatalf("told again %+v", n)
	}
}

// A rerunnable freeze that could not wake in RerunAfter is killed and its
// rerun waits at the gate; a kill that found nothing keeps the freeze.
func TestTickKillsARerunnableFreeze(t *testing.T) {
	b := hold("b", time.Hour)
	v := &fakeView{rerun: "make test"}
	in := inputs(v, critical(), hold("a", 2*time.Hour), b)
	st0 := State{Frozen: []Frozen{{Owner: "b", Since: b.Since.Format(time.RFC3339Nano), At: t0.Add(-RerunAfter), Class: "make test"}}, Short: true}
	h := &host{kill: 3, sig: critical()}
	st := Run(in, st0, h.apply)
	if len(st.Frozen) != 0 || len(st.RerunKills["make test"]) != 1 {
		t.Fatalf("state %+v", st)
	}
	for _, k := range []Kind{Kill, KeepKills, RerunWait, Raise, Release} {
		if !slices.Contains(h.kinds(), k) {
			t.Fatalf("no %v in %v", k, h.kinds())
		}
	}
	if n := h.notices("b"); n[0]["killed"] != "make test" {
		t.Fatalf("notice %+v", n)
	}
	h = &host{kill: 0, sig: critical()}
	if st := Run(in, st0, h.apply); len(st.Frozen) != 1 || len(st.RerunKills) != 0 || slices.Contains(h.kinds(), Release) {
		t.Fatalf("a kill of nothing changed %+v", st)
	}
	// RerunCap kills in the hour: frozen instead.
	st0.RerunKills = map[string][]time.Time{"make test": {t0.Add(-3 * time.Minute), t0.Add(-2 * time.Minute), t0.Add(-time.Minute)}}
	h = &host{kill: 3, sig: critical()}
	if Run(in, st0, h.apply); slices.Contains(h.kinds(), Kill) {
		t.Fatal("a capped class was killed")
	}
}

// When memory recovers the gate's held commands are told to retry, in
// order, and the memory notice says nothing.
func TestTickRetriesTheGateOnRecovery(t *testing.T) {
	in := inputs(&fakeView{}, calm())
	h := &host{sig: calm(), gate: []string{"t2", "t1"}}
	st := Run(in, State{Short: true}, h.apply)
	if st.Short {
		t.Fatal("still short")
	}
	var told []string
	for _, d := range h.applied {
		if d.Kind == Notify && d.Args["retry"] == true {
			told = append(told, d.Thread)
		}
	}
	if !slices.Equal(told, []string{"t1", "t2"}) {
		t.Fatalf("retry %v", told)
	}
}

// The memory notice tells the holder whose job grew most once per episode,
// from the signals and growth read after the freezes.
func TestTickMemoryNoticeOncePerEpisode(t *testing.T) {
	in := inputs(&fakeView{}, calm(), hold("a", time.Hour), hold("b", time.Minute))
	in.Can.Guard = false
	short := calm()
	short.Headroom = f(0.05)
	apply := func(d Decision) Answer {
		switch d.Kind {
		case ReadSignals:
			return Answer{Signals: short}
		case ReadGrowth:
			return Answer{Growth: map[string]uint64{"a": 1e9, "b": 2e9}}
		}
		return Answer{}
	}
	var told []Decision
	st := Run(in, State{}, func(d Decision) Answer {
		if d.Kind == NotifyMemory {
			told = append(told, d)
		}
		return apply(d)
	})
	if len(told) != 1 || told[0].Thread != "b" || told[0].Grew != 2e9 || !st.MemTold {
		t.Fatalf("told %+v", told)
	}
	told = nil
	Run(in, st, func(d Decision) Answer {
		if d.Kind == NotifyMemory {
			told = append(told, d)
		}
		return apply(d)
	})
	if len(told) != 0 {
		t.Fatal("told twice in one episode")
	}
}

// Tick never writes the state it is given.
func TestTickLeavesItsInputAlone(t *testing.T) {
	v := &fakeView{growth: map[string]uint64{"b": 3e9}}
	in := inputs(v, critical(), hold("a", time.Hour), hold("b", time.Minute))
	st := State{FrozeHolds: map[string]time.Time{}, UndFroze: map[string]UndFreeze{}}
	h := &host{ok: true, sig: critical()}
	Run(in, st, h.apply)
	if len(st.FrozeHolds) != 0 || len(st.Frozen) != 0 {
		t.Fatalf("the caller's state changed: %+v", st)
	}
}
