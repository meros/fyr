package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/agycli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

type fakePrinterAdapter struct {
	cli.Adapter
	id     string
	models []cli.Model
	mu     sync.Mutex
	calls  []cli.Launch
	fn     func(ctx context.Context, l cli.Launch, system, prompt string) (string, error)
}

func (f *fakePrinterAdapter) ID() string            { return f.id }
func (f *fakePrinterAdapter) DefaultBinary() string { return f.id }
func (f *fakePrinterAdapter) Caps() cli.Caps {
	return cli.Caps{Auth: []cli.AuthKind{cli.CLILogin}, Models: f.models}
}
func (f *fakePrinterAdapter) Env(cli.Launch) ([]string, error) {
	return nil, nil
}
func (f *fakePrinterAdapter) Start(cli.Launch, cli.Handlers) (cli.Session, error) {
	return nil, errors.New("unused")
}
func (f *fakePrinterAdapter) Print(ctx context.Context, l cli.Launch, system, prompt string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, l)
	fn := f.fn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, l, system, prompt)
	}
	return "", nil
}

type stubApprovalHost struct {
	mu     sync.Mutex
	inputs []core.Input
	onOpen func(core.Decision)
}

func (h *stubApprovalHost) Apply(in core.Input) (any, error) {
	h.mu.Lock()
	h.inputs = append(h.inputs, in)
	onOpen := h.onOpen
	h.mu.Unlock()
	if in.Kind == core.KindDecisionOpened && onOpen != nil {
		if d, ok := in.Args.(core.Decision); ok {
			onOpen(d)
		}
	}
	return nil, nil
}

func (h *stubApprovalHost) Thread(id string) (core.Thread, bool) {
	return core.Thread{ID: id}, true
}

func newApprovalTestManager(t *testing.T, ad *fakePrinterAdapter, ac ApprovalContext, sa core.SystemAgents) (*Manager, *stubApprovalHost, *session) {
	t.Helper()
	h := &stubApprovalHost{}
	p := provider.Profile{
		ID:        ad.id,
		Name:      ad.id,
		CLI:       ad.id,
		Models:    []string{"gemini-4-neon", "gemini-4-hydrogen", "gemini-3.8-flash"},
		ModelCaps: agycli.Models,
	}
	if ad.id == claudecli.ID {
		p.Models = []string{"claude-opus-5-5", "claude-haiku-5-5"}
		p.ModelCaps = claudecli.Models
	}
	m := New(h, Config{
		HostName:        "host-a",
		Profile:         func(string) provider.Profile { return p },
		ProfileExists:   func(id string) bool { return id == p.ID },
		SystemAgents:    func() core.SystemAgents { return sa },
		Adapters:        map[string]cli.Adapter{ad.id: ad},
		ApprovalContext: func(string) ApprovalContext { return ac },
		ApprovalTimeout: 500 * time.Millisecond,
	})
	s := &session{
		thread: "t_1",
		proc:   "t_1-abc",
		asks:   map[string]chan core.Effect{},
		reqs:   map[string]string{},
	}
	m.sessions["t_1"] = s
	return m, h, s
}

func TestApprovalJudgeAllowsAndCaches(t *testing.T) {
	ad := &fakePrinterAdapter{
		id:     agycli.ID,
		models: agycli.Models,
		fn: func(_ context.Context, l cli.Launch, _ string, _ string) (string, error) {
			return `{"decision":"allow","reason":"Routine go test command"}`, nil
		},
	}
	ac := ApprovalContext{
		AutoJudge:     true,
		ProjectName:   "acme",
		ProjectFolder: "/src/acme",
		ThreadTitle:   "Fix tests",
		ThreadProfile: agycli.ID,
		ThreadModel:   "gemini-4-neon",
		Messages:      []core.ApprovalMessage{{Text: "Run the test suite"}},
	}
	m, h, s := newApprovalTestManager(t, ad, ac, core.SystemAgents{})

	in := map[string]any{"command": "go test ./...", "BypassSandbox": true}
	ans := m.canUseTool(s, claude.Msg{"request_id": "r1", "tool_name": "Bash", "input": in})
	if ans["behavior"] != "allow" {
		t.Fatalf("expected allow, got %+v", ans)
	}
	if len(ad.calls) != 1 {
		t.Fatalf("expected 1 judge call, got %d", len(ad.calls))
	}
	// Gemini 4 Hydrogen is selected as fast model with low effort.
	if ad.calls[0].Model != "gemini-4-hydrogen" || ad.calls[0].Effort != "low" {
		t.Fatalf("launch: model=%q effort=%q", ad.calls[0].Model, ad.calls[0].Effort)
	}
	// Exact same tool+input hits session cache without invoking judge again.
	ans2 := m.canUseTool(s, claude.Msg{"request_id": "r2", "tool_name": "Bash", "input": in})
	if ans2["behavior"] != "allow" || len(ad.calls) != 1 {
		t.Fatalf("expected cached allow: ans=%+v calls=%d", ans2, len(ad.calls))
	}
	// No DecisionApproval opened, and two quiet decision.autoApproved events logged.
	autoEvents := 0
	for _, inp := range h.inputs {
		if inp.Kind == core.KindDecisionOpened {
			t.Fatalf("unexpected DecisionOpened: %+v", inp)
		}
		if inp.Kind == core.KindRecord {
			if evs, ok := inp.Args.([]core.Event); ok {
				for _, ev := range evs {
					if ev.Type == core.EventDecisionAutoApproved {
						autoEvents++
					}
				}
			}
		}
	}
	if autoEvents != 2 {
		t.Fatalf("expected 2 decision.autoApproved events, got %d", autoEvents)
	}
}

func TestApprovalJudgeEscalatesAndFailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		fn       func(ctx context.Context, l cli.Launch, sys, prompt string) (string, error)
		wantNote string
	}{
		{
			name: "judge asks",
			fn: func(context.Context, cli.Launch, string, string) (string, error) {
				return `{"decision":"ask","reason":"Deletes files outside the repo"}`, nil
			},
			wantNote: "Deletes files outside the repo",
		},
		{
			name: "malformed json",
			fn: func(context.Context, cli.Launch, string, string) (string, error) {
				return "sure looks fine", nil
			},
			wantNote: "Approval judge gave an unclear reply",
		},
		{
			name: "deny fails closed to ask",
			fn: func(context.Context, cli.Launch, string, string) (string, error) {
				return `{"decision":"deny","reason":"bad"}`, nil
			},
			wantNote: "Approval judge gave an unclear reply",
		},
		{
			name: "provider error",
			fn: func(context.Context, cli.Launch, string, string) (string, error) {
				return "", errors.New("rate limited")
			},
			wantNote: "Approval judge failed: rate limited",
		},
		{
			name: "timeout",
			fn: func(ctx context.Context, _ cli.Launch, _, _ string) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			},
			wantNote: "Approval judge timed out",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ad := &fakePrinterAdapter{id: claudecli.ID, models: claudecli.Models, fn: c.fn}
			ac := ApprovalContext{AutoJudge: true, ProjectName: "acme", ThreadProfile: claudecli.ID, ThreadModel: "claude-opus-5-5"}
			m, h, s := newApprovalTestManager(t, ad, ac, core.SystemAgents{})
			m.cfg.ApprovalTimeout = 50 * time.Millisecond

			var opened core.Decision
			h.onOpen = func(d core.Decision) {
				opened = d
				go m.answer("t_1", core.Effect{Kind: core.EffectAnswer, DecisionID: d.ID, Allow: false})
			}

			ans := m.canUseTool(s, claude.Msg{"request_id": "r1", "tool_name": "Bash", "input": map[string]any{"command": "rm -rf /tmp/x"}})
			if ans["behavior"] != "deny" {
				t.Fatalf("expected owner deny, got %+v", ans)
			}
			if opened.Kind != core.DecisionApproval || opened.JudgeNote != c.wantNote {
				t.Fatalf("opened=%+v want JudgeNote=%q", opened, c.wantNote)
			}
			// Claude Haiku does not advertise effort levels so Effort must be empty.
			if len(ad.calls) != 1 || ad.calls[0].Model != "claude-haiku-5-5" || ad.calls[0].Effort != "" {
				t.Fatalf("launch: %+v", ad.calls)
			}
		})
	}
}

func TestApprovalJudgeRespectsGateFirst(t *testing.T) {
	ad := &fakePrinterAdapter{
		id:     agycli.ID,
		models: agycli.Models,
		fn: func(context.Context, cli.Launch, string, string) (string, error) {
			t.Fatal("judge must not be called for gated commands")
			return "", nil
		},
	}
	ac := ApprovalContext{AutoJudge: true, ProjectName: "acme", ThreadProfile: agycli.ID}
	m, h, s := newApprovalTestManager(t, ad, ac, core.SystemAgents{})

	ans := m.canUseTool(s, claude.Msg{"request_id": "r1", "tool_name": "Bash", "input": map[string]any{"command": "gh pr merge 42"}})
	if ans["behavior"] != "deny" {
		t.Fatalf("expected gate deny, got %+v", ans)
	}
	if len(h.inputs) != 0 {
		t.Fatalf("gate should not open a decision: %+v", h.inputs)
	}
}

func TestApprovalJudgeCancelledMidFlight(t *testing.T) {
	started := make(chan struct{})
	ad := &fakePrinterAdapter{
		id:     agycli.ID,
		models: agycli.Models,
		fn: func(ctx context.Context, _ cli.Launch, _, _ string) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	ac := ApprovalContext{AutoJudge: true, ProjectName: "acme", ThreadProfile: agycli.ID}
	m, h, s := newApprovalTestManager(t, ad, ac, core.SystemAgents{})
	m.cfg.ApprovalTimeout = 5 * time.Second

	done := make(chan claude.Msg, 1)
	go func() {
		done <- m.canUseTool(s, claude.Msg{"request_id": "r1", "tool_name": "Bash", "input": map[string]any{"command": "echo hi"}})
	}()
	<-started
	m.cancelRequest(s, "r1")
	ans := <-done
	if ans["behavior"] != "deny" {
		t.Fatalf("expected deny on cancel, got %+v", ans)
	}
	for _, inp := range h.inputs {
		if inp.Kind == core.KindDecisionOpened {
			t.Fatalf("cancelled judge must not open a decision: %+v", inp)
		}
	}
}

func TestResolveApprovalJudgeModes(t *testing.T) {
	profiles := map[string]provider.Profile{
		"claude": {ID: "claude", Name: "Claude", CLI: claudecli.ID, Models: []string{"claude-opus-5-5", "claude-haiku-5-5"}, DefaultModel: "claude-opus-5-5"},
		"agy":    {ID: "agy", Name: "Antigravity", CLI: agycli.ID, Models: []string{"gemini-4-neon", "gemini-4-hydrogen", "gemini-3.8-flash"}, DefaultModel: "gemini-4-neon"},
		"custom": {ID: "custom", Name: "Custom", CLI: claudecli.ID, Models: []string{"my-model"}, DefaultModel: "my-model", Auth: cli.Auth{Kind: cli.APIKey}, Endpoint: cli.Endpoint{BaseURL: "https://api.example.com"}},
	}
	mk := func(sa core.SystemAgents) *Manager {
		return New(nil, Config{
			HostName: "host-a",
			Profile: func(id string) provider.Profile {
				if p, ok := profiles[id]; ok {
					return p
				}
				return profiles["claude"]
			},
			ProfileExists: func(id string) bool { _, ok := profiles[id]; return ok },
			SystemAgents:  func() core.SystemAgents { return sa },
		})
	}

	// Default (thread's provider): agy → gemini-4-hydrogen, claude → claude-haiku-5-5, custom → DefaultModel.
	m := mk(core.SystemAgents{})
	if a, err := m.ResolveApprovalJudge("agy", "gemini-4-neon"); err != nil || a.Profile.ID != "agy" || a.Model != "gemini-4-hydrogen" {
		t.Fatalf("agy default: %+v %v", a, err)
	}
	if a, err := m.ResolveApprovalJudge("claude", "claude-opus-5-5"); err != nil || a.Profile.ID != "claude" || a.Model != "claude-haiku-5-5" {
		t.Fatalf("claude default: %+v %v", a, err)
	}
	if a, err := m.ResolveApprovalJudge("custom", "fallback-m"); err != nil || a.Profile.ID != "custom" || a.Model != "my-model" {
		t.Fatalf("custom fallback: %+v %v", a, err)
	}

	// $default override follows SystemAgents.Default regardless of thread's provider.
	m = mk(core.SystemAgents{
		Default: core.SystemAgentChoice{Profile: "claude", Model: "claude-haiku-5-5"},
		Jobs:    map[string]core.SystemAgentChoice{core.JobApproval: {Profile: core.ProfileSystemDefault}},
	})
	if a, err := m.ResolveApprovalJudge("agy", "gemini-4-neon"); err != nil || a.Profile.ID != "claude" || a.Model != "claude-haiku-5-5" {
		t.Fatalf("$default override: %+v %v", a, err)
	}

	// Explicit provider+model override.
	m = mk(core.SystemAgents{
		Jobs: map[string]core.SystemAgentChoice{core.JobApproval: {Profile: "agy", Model: "gemini-3.8-flash"}},
	})
	if a, err := m.ResolveApprovalJudge("claude", "claude-opus-5-5"); err != nil || a.Profile.ID != "agy" || a.Model != "gemini-3.8-flash" {
		t.Fatalf("explicit override: %+v %v", a, err)
	}
}
