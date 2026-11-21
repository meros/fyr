package agent

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

// qhHost records the Inputs the manager observes.
type qhHost struct {
	mu sync.Mutex
	in []core.Input
}

func (h *qhHost) Apply(in core.Input) (any, error) {
	h.mu.Lock()
	h.in = append(h.in, in)
	h.mu.Unlock()
	return nil, nil
}
func (h *qhHost) Thread(id string) (core.Thread, bool) { return core.Thread{ID: id, Profile: "claude"}, true }

func (h *qhHost) runEnded() *core.RunEndedArgs {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, in := range h.in {
		if in.Kind == core.KindRunEnded {
			a, _ := in.Args.(core.RunEndedArgs)
			return &a
		}
	}
	return nil
}

// V0.quota-wait: claude ends the run with an error whose text is the
// provider's notice; the failure is classed quota, not api or error, so the
// thread waits for the reset instead of needing the owner.
func TestQuotaFailureFromAnErroredResult(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "error", "is_error": true,
		"result": "You've hit your weekly limit · resets 23 Nov, 4am (Europe/Stockholm)",
	})
	a := h.runEnded()
	if a == nil || a.Failure == nil {
		t.Fatalf("no failure: %+v", a)
	}
	if a.Failure.Class != core.ClassQuota {
		t.Fatalf("class %q: %+v", a.Failure.Class, a.Failure)
	}
}

// V0.quota-wait: agy ends the run cleanly and writes the notice as the turn's
// last words; the same class, so the thread reads the same on both providers.
func TestQuotaFailureFromACleanResult(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "agy"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": "\n⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 35m47s.\n",
	})
	a := h.runEnded()
	if a == nil || a.Failure == nil || a.Failure.Class != core.ClassQuota {
		t.Fatalf("no quota failure: %+v", a)
	}
	if !strings.HasPrefix(a.Failure.Message, "⚠ Individual quota reached.") {
		t.Fatalf("message %q", a.Failure.Message)
	}
}

// An interrupted run is the owner's Stop, never a quota stop.
func TestAnInterruptedRunIsNotAQuotaStop(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "agy"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true, interrupt: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "interrupted", "is_error": false,
		"result": "⚠ Individual quota reached. Resets in 35m47s.",
	})
	if a := h.runEnded(); a == nil || a.Failure != nil {
		t.Fatalf("an interrupted run failed: %+v", a)
	}
}

// A clean end whose text is not the notice keeps failing nothing.
func TestACleanEndWithAnAgentReportIsNotAQuotaStop(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": "I made the retry show an Individual quota reached error as a wait.",
	})
	if a := h.runEnded(); a == nil || a.Failure != nil {
		t.Fatalf("an agent's report failed the run: %+v", a)
	}
}

// V0.quota-wait-midturn: when agy writes prose earlier in a turn, runs tools,
// and then hits quota, its result concatenates the whole turn's text while the
// final assistant message holds only the quota notice; the run still ends as a
// quota wait.
func TestQuotaFailureMidTurnAfterEarlierProseAndTool(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "agy"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "Found the root cause in the adapter."}},
		},
	})
	m.handle(s, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "call_1", "name": "Bash", "input": map[string]any{"command": "go test ./..."}}},
		},
	})
	m.handle(s, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"}},
		},
	})
	m.handle(s, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "\n⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 35m47s.\n"}},
		},
	})
	m.handle(s, map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": "Found the root cause in the adapter.\n⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 35m47s.\n",
	})
	a := h.runEnded()
	if a == nil || a.Failure == nil || a.Failure.Class != core.ClassQuota {
		t.Fatalf("mid-turn quota halt missed: %+v", a)
	}
	if !strings.HasPrefix(a.Failure.Message, "⚠ Individual quota reached.") {
		t.Fatalf("message %q", a.Failure.Message)
	}
}

// V0.quota-wait-midturn: a subagent's quota notice or an agent report after a
// tool call never marks the main turn as quota-stopped.
func TestSubagentOrProseAfterToolIsNotAQuotaStop(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "agy"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type":               "assistant",
		"parent_tool_use_id": "sub_1",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "⚠ Individual quota reached. Resets in 10m."}},
		},
	})
	m.handle(s, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "The log showed an Individual quota reached line, fixed now."}},
		},
	})
	m.handle(s, map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": "The log showed an Individual quota reached line, fixed now.",
	})
	if a := h.runEnded(); a == nil || a.Failure != nil {
		t.Fatalf("false positive quota stop: %+v", a)
	}
}

// V0.quota-auto-continue: when KindClock auto-continues a quota-stopped thread
// and emits EffectFeed, Manager.Do(EffectFeed) takes the waking continue notice
// and delivers it to the agent to resume the turn.
type quotaFeedHost struct {
	mu      sync.Mutex
	th      *core.Thread
	v       *core.View
	now     time.Time
	sent    string
	summary string
}

func (h *quotaFeedHost) Apply(in core.Input) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if in.Kind == core.KindMessageSent {
		if a, ok := in.Args.(core.MessageSentArgs); ok {
			h.sent = a.Text
		}
	}
	if in.Kind == core.KindDigest {
		if a, ok := in.Args.(core.DigestArgs); ok && a.Summary != "" {
			h.summary = a.Summary
		}
	}
	nt, nv, res, err := core.Reduce(h.th, h.v, in, h.now)
	if err != nil {
		return nil, err
	}
	h.th, h.v = nt, nv
	return res.Reply, nil
}

func (h *quotaFeedHost) Thread(id string) (core.Thread, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return *core.CloneThread(h.th), true
}

func TestQuotaAutoContinueWakesAgentViaFeed(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	h := &quotaFeedHost{
		th: &core.Thread{
			ID:        "t_qauto",
			Profile:   "fake",
			Cwd:       t.TempDir(),
			SessionID: "s_q1",
			Conversation: core.Conversation{
				Failure: &core.Failure{Class: core.ClassQuota, Message: "⚠ Individual quota reached. Resets in 30m.", At: t0},
			},
		},
		v:   &core.View{User: core.Owner, Entity: "t_qauto", SeenAt: t0},
		now: t0.Add(30*time.Minute + core.QuotaResumeBuffer),
	}
	nt, nv, res, err := core.Reduce(h.th, h.v, core.Input{Source: core.Observer(core.ObsClock), Kind: core.KindClock, Thread: "t_qauto"}, h.now)
	if err != nil {
		t.Fatal(err)
	}
	h.th, h.v = nt, nv
	var feedEff *core.Effect
	for i := range res.Effects {
		if res.Effects[i].Kind == core.EffectFeed {
			feedEff = &res.Effects[i]
			break
		}
	}
	if feedEff == nil {
		t.Fatalf("no EffectFeed emitted on quota auto-continue: %+v", res.Effects)
	}

	m := New(h, Config{
		Profile: func(string) provider.Profile {
			return provider.Profile{ID: "fake", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}}
		},
		MCPURL: "http://127.0.0.1:1/mcp",
		Prompt: "-",
	})
	t.Cleanup(m.StopAll)

	if err := m.Do("t_qauto", *feedEff); err != nil {
		t.Fatalf("EffectFeed failed: %v", err)
	}
	if !waitFor(8*time.Second, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.sent != "" && h.summary != ""
	}) {
		h.mu.Lock()
		defer h.mu.Unlock()
		t.Fatalf("agent did not resume from quota auto-continue: sent=%q summary=%q", h.sent, h.summary)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !strings.Contains(h.sent, "[Fyr] The provider quota has reset") {
		t.Fatalf("unexpected continue notice sent to agent: %q", h.sent)
	}
}
