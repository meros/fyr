package guard

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

func noRoom() (bool, string) { return false, "would not fit" }
func room() (bool, string)   { return true, "" }

func TestJudgeFrozen(t *testing.T) {
	calm := guardMemory{calm: true}
	crit := guardMemory{critical: true}
	agents := guardMemory{critical: true, agentsCritical: true}
	for _, c := range []struct {
		name string
		m    guardMemory
		f    frozenFacts
		room func() (bool, string)
		want frozenVerdict
	}{
		{"holding and calm keeps", calm, frozenFacts{holding: true, age: time.Minute}, room, frozenVerdict{act: actKeep}},
		{"ended hold in a critical stall is kept", crit, frozenFacts{age: time.Minute}, room, frozenVerdict{act: actKeep, why: whyEnded}},
		{"ended hold at the ceiling thaws", agents, frozenFacts{age: FreezeCeiling}, room, frozenVerdict{act: actThaw, ceiling: true}},
		{"ended hold at the ceiling is kept when rerunnable", agents, frozenFacts{age: FreezeCeiling, rerunnable: true}, room, frozenVerdict{act: actKeep, why: whyEnded}},
		{"ended hold in a calm stall thaws", calm, frozenFacts{age: time.Minute}, room, frozenVerdict{act: actThaw}},
		{"emptied thaws at once", crit, frozenFacts{holding: true, emptied: true, age: time.Second}, room, frozenVerdict{act: actThaw}},
		{"timed thaw when it fits", calm, frozenFacts{holding: true, age: FreezeMax}, room, frozenVerdict{act: actThaw}},
		{"timed thaw waits without room", calm, frozenFacts{holding: true, age: FreezeMax}, noRoom, frozenVerdict{act: actKeep, why: whyNoRoom, room: "would not fit"}},
		{"past max in the agents' stall is told once", agents, frozenFacts{holding: true, age: FreezeMax}, room, frozenVerdict{act: actKeepPastMax, why: whyCritical}},
		{"ceiling in the agents' stall thaws", agents, frozenFacts{holding: true, age: FreezeCeiling}, room, frozenVerdict{act: actThaw, ceiling: true}},
	} {
		if got := judgeFrozen(c.m, c.f, c.room); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestMemoryState(t *testing.T) {
	if m := memoryState(resource.Signals{}, resource.Policy{}); m.short || !m.calm {
		t.Fatalf("no signals must be calm and not short: %+v", m)
	}
	m := memoryState(resource.Signals{Provider: "x", AgentsStall: resource.StallCritical}, resource.Policy{})
	if !m.agentsCritical || !m.critical || !m.short || m.calm {
		t.Fatalf("agents' critical stall: %+v", m)
	}
}

func TestThawWords(t *testing.T) {
	now := time.Now()
	f := Frozen{At: now.Add(-FreezeMax - time.Second)}
	for _, c := range []struct {
		t        thawing
		why, rsn string
	}{
		{thawing{f: Frozen{At: now}, holding: true}, "memory", "memory fine for 30 s"},
		{thawing{f: f, emptied: true}, "gone", "its processes are gone"},
		{thawing{f: f}, "ended", "episode or hold ended"},
		{thawing{f: f, holding: true, woke: "because"}, "memory", "because"},
		{thawing{f: f, holding: true}, "limit", "10 min limit"},
	} {
		if r, w := thawWords(c.t, now); r != c.rsn || w != c.why {
			t.Errorf("%+v: got %q %q want %q %q", c.t, r, w, c.rsn, c.why)
		}
	}
	f.PastMax = true
	if r, _ := thawWords(thawing{f: f, holding: true}, now); r != "10 min limit, stall no longer critical" {
		t.Errorf("past max: %q", r)
	}
	if _, w := thawWords(thawing{f: f, holding: true, ceiling: true}, now); w != "ceiling" {
		t.Errorf("ceiling: %q", w)
	}
}

func TestStillHolding(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	job := Frozen{Owner: "t1", Since: at.Format(time.RFC3339Nano)}
	und := Frozen{Owner: "t1", Undeclared: true, Episode: at}
	for _, c := range []struct {
		name    string
		f       Frozen
		flagged []resource.Undeclared
		held    []resource.Holder
		want    bool
	}{
		{"job still held", job, nil, []resource.Holder{{Owner: "t1", Since: at}}, true},
		{"job released", job, nil, []resource.Holder{{Owner: "t2", Since: at}}, false},
		{"job held under a newer hold", job, nil, []resource.Holder{{Owner: "t1", Since: at.Add(time.Second)}}, false},
		{"undeclared still flagged", und, []resource.Undeclared{{Thread: "t1", Since: at}}, nil, true},
		{"undeclared flagged with a newer start", und, []resource.Undeclared{{Thread: "t1", Since: at.Add(time.Second)}}, nil, false},
		{"undeclared no longer flagged", und, nil, nil, false},
	} {
		if got := stillHolding(c.f, c.flagged, c.held); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestWakeRules(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	calm, crit := guardMemory{calm: true}, guardMemory{critical: true}
	heavy := func(n int) func() int { return func() int { return n } }
	if !wakesAlone(1, 0, calm, WakeEvery, heavy(0)) {
		t.Error("alone, calm, WakeEvery since the last wake: wakes")
	}
	for name, got := range map[string]bool{
		"nothing kept":     wakesAlone(0, 0, calm, WakeEvery, heavy(0)),
		"a thaw this tick": wakesAlone(1, 1, calm, WakeEvery, heavy(0)),
		"critical":         wakesAlone(1, 0, crit, WakeEvery, heavy(0)),
		"near oomd":        wakesAlone(1, 0, guardMemory{nearOomd: true}, WakeEvery, heavy(0)),
		"other heavy runs": wakesAlone(1, 0, calm, WakeEvery, heavy(1)),
		"woke just now":    wakesAlone(1, 0, calm, WakeEvery-time.Second, heavy(0)),
	} {
		if got {
			t.Errorf("wakesAlone %s", name)
		}
	}
	ok := now.Add(-ThawAfterOK)
	last := now.Add(-WakeEvery)
	if !wakeDue(1, 0, ok, last, now) {
		t.Error("wakeDue at the thresholds")
	}
	for name, got := range map[string]bool{
		"nothing kept":    wakeDue(0, 0, ok, last, now),
		"a thaw":          wakeDue(1, 1, ok, last, now),
		"clock not run":   wakeDue(1, 0, time.Time{}, last, now),
		"clock too young": wakeDue(1, 0, ok.Add(time.Second), last, now),
		"woke just now":   wakeDue(1, 0, ok, last.Add(time.Second), now),
	} {
		if got {
			t.Errorf("wakeDue %s", name)
		}
	}
}

func TestFreezeGateAndRoom(t *testing.T) {
	short := guardMemory{short: true}
	for _, c := range []struct {
		name  string
		m     guardMemory
		stall bool
		n     int
		want  freezeGateWhy
	}{
		{"not short", guardMemory{}, true, 0, gateQuiet},
		{"a thaw this tick", short, true, 1, gateQuiet},
		{"short, no stall", short, false, 0, gateNoStall},
		{"short, stalls", short, true, 0, gateOpen},
		{"short, critical", guardMemory{short: true, critical: true}, false, 0, gateOpen},
		{"short, near oomd", guardMemory{short: true, nearOomd: true}, false, 0, gateOpen},
	} {
		if got := freezeGate(c.m, c.stall, c.n); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	now := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	for _, c := range []struct {
		name       string
		kept, max  int
		last       time.Time
		oomd       bool
		block, say bool
	}{
		{"none frozen, room", 0, 2, time.Time{}, false, false, false},
		{"none frozen, max 0: one thread runs, so one may freeze", 0, 0, time.Time{}, false, false, false},
		{"one frozen, max 0", 1, 0, now.Add(-time.Hour), false, true, false},
		{"at the limit", 2, 2, now.Add(-time.Hour), false, true, true},
		{"room but the last is young", 1, 2, now.Add(-time.Second), false, true, false},
		{"young last, near oomd", 1, 2, now.Add(-time.Second), true, false, false},
		{"room, last old enough", 1, 2, now.Add(-AddFreezeAfter), false, false, false},
	} {
		block, say := freezeRoom(c.kept, c.kept, c.max, c.last, now, c.oomd)
		if block != c.block || say != c.say {
			t.Errorf("%s: block=%v say=%v", c.name, block, say)
		}
	}
	// V0.freeze-cap-holds-memory: freezes that hold no memory do not fill the
	// limit, but a young last freeze still makes the next wait.
	if block, _ := freezeRoom(2, 0, 2, now.Add(-time.Hour), now, false); block {
		t.Error("two freezes holding no memory filled the limit")
	}
	if block, _ := freezeRoom(2, 0, 2, now.Add(-time.Second), now, false); !block {
		t.Error("a young last freeze did not make the next wait")
	}
}
