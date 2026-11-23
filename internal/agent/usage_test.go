package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/usage"
)

type usageHost struct{ qhHost }

func (h *usageHost) Thread(id string) (core.Thread, bool) {
	return core.Thread{ID: id, Profile: "acme", ProjectID: "p_1", Model: "opus", Effort: "high", SessionID: "s-1"}, true
}

func usageManager(t *testing.T) (*Manager, *store.Store, *usageHost) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &usageHost{}
	m := New(h, Config{
		Profile: func(string) provider.Profile { return provider.Profile{ID: "acme"} },
		Meter:   usage.New(st, "test", time.UTC),
		Account: func(p string) (string, string) { return "a_" + p, "subscription" },
	})
	return m, st, h
}

// usageResult is a result whose big model has the given cumulative counts
// and whose main agent used 1 input, mainOut output and 10 cache reads.
func usageResult(uuid string, mainOut, bigIn, bigOut, bigCR float64) map[string]any {
	return map[string]any{"type": "result", "subtype": "success", "uuid": uuid, "session_id": "s-1",
		"usage": map[string]any{"input_tokens": float64(1), "output_tokens": mainOut, "cache_read_input_tokens": float64(10)},
		"modelUsage": map[string]any{
			"claude-big":   map[string]any{"inputTokens": bigIn, "outputTokens": bigOut, "cacheReadInputTokens": bigCR},
			"claude-small": map[string]any{"inputTokens": float64(5), "outputTokens": float64(1)},
		}}
}

func rollup(t *testing.T, st *store.Store, group ...string) map[string]cli.TokenCount {
	t.Helper()
	gs, err := st.UsageRollup(group, store.UsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]cli.TokenCount{}
	for _, g := range gs {
		k := ""
		for _, n := range group {
			k += g.Key[n] + "|"
		}
		out[k] = g.TokenCount
	}
	return out
}

// V0.usage-ledger: a result's usage reaches the ledger with the turn's
// trigger and the thread's dimensions, split into the main agent's part and
// the rest, and a replayed result counts once.
func TestResultUsageReachesTheLedger(t *testing.T) {
	m, st, _ := usageManager(t)
	s := &session{thread: "t_1", proc: "p", digest: newDigest(), live: true, model: "claude-big"}
	noteTrigger(s, "msg-1", usage.TriggerOwner, true)
	m.handle(s, usageResult("r-1", 3, 1, 3, 10))
	got := rollup(t, st, "thread", "project", "provider", "account", "billing", "adapter", "trigger", "effort", "model", "part")
	main := "t_1|p_1|acme|a_acme|subscription|claude|owner|high|claude-big|main|"
	other := "t_1|p_1|acme|a_acme|subscription|claude|owner|high|claude-small|other|"
	if got[main] != (cli.TokenCount{Input: 1, Output: 3, CacheRead: 10}) || got[other] != (cli.TokenCount{Input: 5, Output: 1}) || len(got) != 2 {
		t.Fatalf("ledger %+v", got)
	}
	if s.trigger != "" {
		t.Errorf("the trigger outlived its turn: %q", s.trigger)
	}
	// The runner delivers the result again after a restart of fyrd.
	m.handle(s, usageResult("r-1", 3, 1, 3, 10))
	if got := rollup(t, st); got[""] != (cli.TokenCount{Input: 6, Output: 4, CacheRead: 10}) {
		t.Fatalf("replay counted again: %+v", got)
	}
	// The CLI goes on by itself after a background task: a stray result,
	// outside a turn, still counts, as background. The subagent's output
	// beyond the main share is the other part.
	m.handle(s, usageResult("r-2", 5, 2, 13, 20))
	got = rollup(t, st, "trigger", "part")
	if got["background|main|"] != (cli.TokenCount{Input: 1, Output: 5, CacheRead: 10}) || got["background|other|"] != (cli.TokenCount{Output: 5}) {
		t.Fatalf("background %+v", got)
	}
}

// A queued message that the CLI starts after the turn ended gives the new
// turn its trigger; a turn the CLI starts on its own is background.
func TestTriggerOfAQueuedMessage(t *testing.T) {
	m, _, _ := usageManager(t)
	s := &session{thread: "t_1", digest: newDigest(), out: map[string]string{}}
	m.mu.Lock()
	s.live = true
	noteTrigger(s, "first", usage.TriggerOwner, true)
	noteTrigger(s, "queued", usage.TriggerThread, false)
	m.mu.Unlock()
	s.live, s.trigger = false, "" // the first turn's result
	m.handle(s, map[string]any{"type": "command_lifecycle", "command_uuid": "queued", "state": "started"})
	if s.trigger != usage.TriggerThread {
		t.Fatalf("trigger %q", s.trigger)
	}
	s.live, s.trigger = false, ""
	m.turnStarted(s)
	if s.trigger != usage.TriggerBackground {
		t.Fatalf("trigger %q", s.trigger)
	}
}

func TestTriggers(t *testing.T) {
	for e, want := range map[string]string{"": usage.TriggerOwner, FromView: usage.TriggerView, FromHandover: usage.TriggerHandover, "t_9": usage.TriggerThread} {
		if got := triggerOf(core.Effect{From: e}); got != want {
			t.Errorf("from %q: %q, want %q", e, got, want)
		}
	}
	if got := noticeTrigger([]core.Notice{{About: "change:c_1"}}); got != usage.TriggerNotice {
		t.Errorf("notice: %q", got)
	}
	if got := noticeTrigger([]core.Notice{{About: "change:c_1"}, {About: "rule:w_1"}}); got != usage.TriggerWake {
		t.Errorf("wake: %q", got)
	}
}
