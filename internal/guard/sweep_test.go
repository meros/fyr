package guard

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// The idle sweep on its own (V0.guard-finish): what goes back, what stays,
// and the idle clocks it leaves.
func TestSweep(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := t0.Add(time.Hour)
	held := func(owners ...string) []resource.Holder {
		var out []resource.Holder
		for _, o := range owners {
			out = append(out, resource.Holder{Owner: o, Since: t0})
		}
		return out
	}
	cases := []struct {
		name     string
		in       SweepIn
		drop     []string
		idle     map[string]time.Time
		unchange bool
	}{
		{name: "a gone thread's hold and place go back",
			in:   SweepIn{Held: held("a"), Waiting: []resource.Holder{{Owner: "w"}}, Threads: map[string]SweepThread{}},
			drop: []string{"a", "w"}, idle: map[string]time.Time{}},
		{name: "the self-deploy releases its own",
			in:   SweepIn{Held: []resource.Holder{{Owner: "d", Priority: true}}, Waiting: []resource.Holder{{Owner: "p", Priority: true}}},
			idle: map[string]time.Time{}},
		{name: "a frozen hold is not swept and its clock stops",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {}}, Frozen: []Frozen{{Owner: "a", Since: "x"}},
				Idle: map[string]time.Time{"a": t0}},
			idle: map[string]time.Time{}},
		{name: "a running thread with a live call keeps its hold until the call is stale",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {Running: true}},
				Active: map[string]time.Time{"a": now.Add(-HeavyCallStale + time.Minute)}, Calls: map[string]int{"a": 1}},
			idle: map[string]time.Time{}},
		{name: "a stale call goes back",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {Running: true}},
				Active: map[string]time.Time{"a": now.Add(-HeavyCallStale)}, Calls: map[string]int{"a": 1}},
			drop: []string{"a"}, idle: map[string]time.Time{}},
		{name: "a background job that runs admitted work stays",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {Running: true, Admitted: true}},
				Active: map[string]time.Time{"a": t0}, Background: map[string]bool{"a": true}},
			idle: map[string]time.Time{}},
		{name: "a running thread idle for SlotIdle goes back",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {Running: true}},
				Active: map[string]time.Time{"a": now.Add(-SlotIdle)}},
			drop: []string{"a"}, idle: map[string]time.Time{}},
		{name: "a thaw restarts the running thread's clock",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {Running: true}},
				Active: map[string]time.Time{"a": t0}, Thawed: map[string]time.Time{"a": now.Add(-time.Minute)}},
			idle: map[string]time.Time{}},
		{name: "admitted work after the run keeps no clock",
			in:   SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {Admitted: true}}, Idle: map[string]time.Time{"a": t0}},
			idle: map[string]time.Time{}},
		{name: "an idle thread's clock starts",
			in:   SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {}}},
			idle: map[string]time.Time{"a": now}},
		{name: "an idle thread goes back after SlotIdle",
			in:   SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {}}, Idle: map[string]time.Time{"a": now.Add(-SlotIdle)}},
			drop: []string{"a"}, idle: map[string]time.Time{"a": now.Add(-SlotIdle)}},
		{name: "a thaw moves an idle thread's clock to the thaw",
			in: SweepIn{Held: held("a"), Threads: map[string]SweepThread{"a": {}}, Idle: map[string]time.Time{"a": t0},
				Thawed: map[string]time.Time{"a": now.Add(-time.Minute)}},
			idle: map[string]time.Time{"a": now.Add(-time.Minute)}},
	}
	for _, c := range cases {
		c.in.Now = now
		before := maps.Clone(c.in.Idle)
		drop, idle := Sweep(c.in)
		if !slices.Equal(drop, c.drop) {
			t.Errorf("%s: dropped %v, want %v", c.name, drop, c.drop)
		}
		if !maps.Equal(idle, c.idle) {
			t.Errorf("%s: idle %v, want %v", c.name, idle, c.idle)
		}
		if !maps.Equal(c.in.Idle, before) {
			t.Errorf("%s: the caller's idle clocks changed: %v", c.name, c.in.Idle)
		}
	}
}

func TestHeavierThanPredicted(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	held := []resource.Holder{{Owner: "told", Since: t0}, {Owner: "new", Since: t0}, {Owner: "again", Since: t0.Add(time.Minute)},
		{Owner: "light", Since: t0}, {Owner: "deploy", Since: t0, Priority: true}}
	told := map[string]time.Time{"told": t0, "again": t0}
	var asked []string
	got := HeavierThanPredicted(held, told, func(o string) Over {
		asked = append(asked, o)
		return Over{Class: "c:" + o, Live: 30, Predicted: 10, Over: o != "light"}
	})
	if want := []string{"new", "again", "light"}; !slices.Equal(asked, want) {
		t.Errorf("measured %v, want %v (a holder told for its hold, and the self-deploy, are not)", asked, want)
	}
	if len(got) != 2 || got[0].Thread != "new" || got[1].Thread != "again" || got[0].Class != "c:new" || got[0].Live != 30 {
		t.Errorf("notices %+v", got)
	}
}

func TestGateHolds(t *testing.T) {
	for _, c := range []struct {
		short, holds, priority, soft, want bool
	}{
		{short: false, holds: true, want: false},
		{short: true, holds: true, want: true},
		{short: true, holds: true, priority: true, want: false},
		{short: true, soft: true, want: true},
		{short: true, want: false}, // the pool queues it
	} {
		if got := GateHolds(c.short, c.holds, c.priority, c.soft); got != c.want {
			t.Errorf("%+v: %t", c, got)
		}
	}
}

func TestBudget(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	full := resource.Factors{CPU: 1, Mem: 1}
	f := func(v float64) *float64 { return &v }
	short := resource.Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.05), Stall: resource.StallOK}
	a := &AIMD{N: 4}
	var m BudgetMove
	var at time.Time
	for i := 0; i < 5 && !m.Set; i++ { // the cut waits for AIMDHold
		at = t0.Add(time.Duration(30*i) * time.Second)
		m = Budget(a, BudgetIn{Now: at, Old: full, Signals: short, Snapshot: resource.Snapshot{Waiting: []resource.Holder{{Owner: "w"}}, Bound: resource.ResMem}})
	}
	if !m.Set || m.F.Mem >= 1 || !strings.Contains(m.Log, "slots: budget cpu=1.000->1.000 mem=1.000->") {
		t.Fatalf("a short host cuts the memory budget and logs it: %+v", m)
	}
	cut, t1 := m.F, at.Add(time.Second)
	one := 1.0
	m = Budget(a, BudgetIn{Now: t1, Old: cut, Signals: short, Snapshot: resource.Snapshot{Budget: &one, BudgetWhy: m.Why}})
	if m.Set || m.Log != "" {
		t.Errorf("within AIMDEvery nothing moves: %+v", m)
	}
	m = Budget(a, BudgetIn{Now: t1, Old: cut, Signals: short, Snapshot: resource.Snapshot{Budget: &one, BudgetWhy: "older"}})
	if !m.Set || m.Log != "" {
		t.Errorf("a new reason is set without a log line: %+v", m)
	}
}
