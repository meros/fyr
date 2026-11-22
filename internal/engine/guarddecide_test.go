package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
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
	if m := memoryState(slots.Signals{}, slots.Policy{}); m.short || !m.calm {
		t.Fatalf("no signals must be calm and not short: %+v", m)
	}
	m := memoryState(slots.Signals{Provider: "x", AgentsStall: slots.StallCritical}, slots.Policy{})
	if !m.agentsCritical || !m.critical || !m.short || m.calm {
		t.Fatalf("agents' critical stall: %+v", m)
	}
}

func TestThawWords(t *testing.T) {
	now := time.Now()
	f := frozenJob{at: now.Add(-FreezeMax - time.Second)}
	for _, c := range []struct {
		t        thawing
		why, rsn string
	}{
		{thawing{f: frozenJob{at: now}, holding: true}, "memory", "memory fine for 30 s"},
		{thawing{f: f, emptied: true}, "gone", "its processes are gone"},
		{thawing{f: f}, "ended", "episode or hold ended"},
		{thawing{f: f, holding: true, woke: "because"}, "memory", "because"},
		{thawing{f: f, holding: true}, "limit", "10 min limit"},
	} {
		if r, w := thawWords(c.t, now); r != c.rsn || w != c.why {
			t.Errorf("%+v: got %q %q want %q %q", c.t, r, w, c.rsn, c.why)
		}
	}
	f.pastMax = true
	if r, _ := thawWords(thawing{f: f, holding: true}, now); r != "10 min limit, stall no longer critical" {
		t.Errorf("past max: %q", r)
	}
	if _, w := thawWords(thawing{f: f, holding: true, ceiling: true}, now); w != "ceiling" {
		t.Errorf("ceiling: %q", w)
	}
}
