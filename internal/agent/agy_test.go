package agent

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/agycli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/runner"
)

type fakeHost struct{ th core.Thread }

func (f fakeHost) Apply(core.Input) (any, error)     { return nil, nil }
func (f fakeHost) Thread(string) (core.Thread, bool) { return f.th, true }

func TestAgyAdapterSystemModelAndHandoverPlan(t *testing.T) {
	gdir := filepath.Join(t.TempDir(), ".gemini")
	convDir := filepath.Join(gdir, "antigravity-cli", "conversations")
	os.MkdirAll(convDir, 0o700)
	os.WriteFile(filepath.Join(convDir, "s-agy.db"), []byte("db"), 0o600)

	agyProf := provider.Profile{
		ID: "agy", Name: "Antigravity", CLI: agycli.ID, Binary: "agy",
		Auth:   cli.Auth{Kind: cli.CLILogin, ConfigDir: gdir},
		Models: []string{"gemini-4-neon", "gemini-3.8-flash"}, DefaultModel: "gemini-4-neon",
	}
	agy2 := provider.Profile{
		ID: "agy2", Name: "Antigravity 2", CLI: agycli.ID, Binary: "agy",
		Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: filepath.Join(t.TempDir(), ".gemini-2")},
	}
	claudeProf := provider.Profile{
		ID: "claude", Name: "Claude", CLI: "claude", Binary: "claude",
	}
	profs := map[string]provider.Profile{"agy": agyProf, "agy2": agy2, "claude": claudeProf}

	m := New(fakeHost{th: core.Thread{ID: "t1", Profile: "agy", SessionID: "s-agy", Cwd: "/work"}}, Config{
		Profile: func(id string) provider.Profile { return profs[id] },
	})
	ad, err := m.adapter(agyProf)
	if err != nil || ad.ID() != agycli.ID {
		t.Fatalf("adapter: %v %v", ad, err)
	}
	if got := HaikuModel(agyProf); got != "gemini-3.8-flash" {
		t.Fatalf("fast model for agy: %q", got)
	}
	// Same CLI with existing conversation DB → Rung 1 (resume).
	if plan := m.HandoverPlan("t1", "agy2"); plan.Rung != RungResume {
		t.Fatalf("agy->agy handover plan: %+v", plan)
	}
	// Different CLI (agy -> claude) → Rung 2 (adopt).
	if plan := m.HandoverPlan("t1", "claude"); plan.Rung != RungAdopt || plan.Note != "The target runs another CLI" {
		t.Fatalf("agy->claude handover plan: %+v", plan)
	}
}

type recordHost struct {
	mu     sync.Mutex
	th     core.Thread
	inputs []core.Input
}

func (h *recordHost) Apply(in core.Input) (any, error) {
	h.mu.Lock()
	h.inputs = append(h.inputs, in)
	h.mu.Unlock()
	return nil, nil
}

func (h *recordHost) Thread(id string) (core.Thread, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.th.ID != id {
		return core.Thread{}, false
	}
	return h.th, true
}

func (h *recordHost) snapshot() []core.Input {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]core.Input(nil), h.inputs...)
}

// V0.agy-reattach-queue: when a queued message begins a follow-up turn after
// the previous turn's result, command_lifecycle "started" must open the new run
// (KindRunStarted) before marking the message read, even before the model emits
// any assistant text or tool call.
func TestQueuedMessageStartedOpensRun(t *testing.T) {
	host := &recordHost{th: core.Thread{ID: "t1", Profile: "stub", Cwd: t.TempDir(), SessionID: "s1"}}
	a := &stubAdapter{}
	m := New(host, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": a},
		Prompt:   "-",
	})
	t.Cleanup(m.StopAll)

	if err := m.Do("t1", core.Effect{Kind: core.EffectSend, MessageID: "m1", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Do("t1", core.Effect{Kind: core.EffectSend, MessageID: "m2", Text: "second"}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	s := m.sessions["t1"]
	m.mu.Unlock()
	ss := s.c.(*stubSession)
	ss.emit(
		lifecycle("m1", "started"),
		lifecycle("m2", "queued"),
		resultMsg("first done"),
		lifecycle("m2", "started"),
	)
	if !waitFor(2*time.Second, func() bool {
		for _, in := range host.snapshot() {
			if in.Kind == core.KindMessageState {
				if a, ok := in.Args.(core.MessageStateArgs); ok && a.ID == "m2" && a.State == "read" {
					return true
				}
			}
		}
		return false
	}) {
		t.Fatal("m2 never became read")
	}
	m.mu.Lock()
	live := s.live
	m.mu.Unlock()
	if !live {
		t.Fatal("session should be live after m2 command_lifecycle started")
	}
	inputs := host.snapshot()
	var endIdx, startIdx, readIdx = -1, -1, -1
	for i, in := range inputs {
		switch in.Kind {
		case core.KindRunEnded:
			endIdx = i
		case core.KindRunStarted:
			if endIdx >= 0 && startIdx < 0 {
				startIdx = i
			}
		case core.KindMessageState:
			if a, ok := in.Args.(core.MessageStateArgs); ok && a.ID == "m2" && a.State == "read" {
				readIdx = i
			}
		}
	}
	if endIdx < 0 || startIdx < 0 || readIdx < 0 || !(endIdx < startIdx && startIdx < readIdx) {
		t.Fatalf("expected RunEnded < RunStarted < m2 read, got end=%d start=%d read=%d", endIdx, startIdx, readIdx)
	}
}

type reattachStubAdapter struct {
	stubAdapter
	mu    sync.Mutex
	got   cli.ReattachState
	queue []cli.QueuedMessage
}

func (a *reattachStubAdapter) ID() string { return "reattach-stub" }

func (a *reattachStubAdapter) ReattachSession(p runner.Pipe, state cli.ReattachState, _ io.Writer, _ cli.Handlers) (cli.Session, error) {
	a.mu.Lock()
	a.got = state
	a.queue = append([]cli.QueuedMessage(nil), state.Queued...)
	a.mu.Unlock()
	return &reattachStubSession{
		stubSession: stubSession{out: make(chan cli.Msg, 16), done: make(chan struct{})},
		p:           p,
		a:           a,
	}, nil
}

type reattachStubSession struct {
	stubSession
	p runner.Pipe
	a *reattachStubAdapter
}

func (s *reattachStubSession) Pipe() runner.Pipe { return s.p }

func (s *reattachStubSession) CancelMessage(_ context.Context, uuid string) (bool, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	for i, q := range s.a.queue {
		if q.ID == uuid {
			s.a.queue = append(s.a.queue[:i], s.a.queue[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// V0.agy-reattach-queue: Adopt passes the thread's running flag and queued
// outbox messages into SessionReattacher, so a restored queued message can be
// cancelled after a fyrd restart.
func TestAttachPassesQueuedOutboxToSessionReattacher(t *testing.T) {
	sock := runner.SocketPath(t.TempDir())
	srv := &runner.Server{Logf: t.Logf}
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(time.Second) })
	rc := &runner.Client{Path: sock}

	meta, _ := json.Marshal(procMeta{Thread: "t1", CLI: "reattach-stub"})
	conn, err := rc.Spawn(runner.Spawn{
		ID:     "t1-proc",
		Key:    "t1",
		Binary: "/bin/sh",
		Args:   []string{"-c", "read _"},
		Dir:    t.TempDir(),
		Meta:   meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	conn.Detach()

	procs, err := rc.List()
	if err != nil || len(procs) != 1 {
		t.Fatalf("runner list: %+v %v", procs, err)
	}

	ad := &reattachStubAdapter{}
	host := &recordHost{th: core.Thread{
		ID:        "t1",
		Profile:   "p1",
		SessionID: "sess-1",
		Model:     "gemini-4-neon",
		Cwd:       t.TempDir(),
		Conversation: core.Conversation{
			Running: true,
			Outbox: []core.OutMsg{
				{ID: "m-sent", State: "sent", Text: "in-flight"},
				{ID: "m-queued", State: "queued", Text: "follow-up work"},
			},
		},
	}}
	m := New(host, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "reattach-stub"} },
		Adapters: map[string]cli.Adapter{"reattach-stub": ad},
		Runner:   rc,
		Prompt:   "-",
	})
	t.Cleanup(func() {
		if c, err := rc.Attach("t1-proc"); err == nil {
			c.Kill()
			c.Forget()
		}
		m.StopAll()
	})

	m.Adopt(procs)

	ad.mu.Lock()
	got := ad.got
	ad.mu.Unlock()
	if got.SessionID != "sess-1" || got.Model != "gemini-4-neon" || !got.Running {
		t.Fatalf("unexpected reattach state: %+v", got)
	}
	if len(got.Queued) != 1 || got.Queued[0].ID != "m-queued" {
		t.Fatalf("expected only queued outbox message restored, got %+v", got.Queued)
	}
	if len(got.Queued[0].Content) != 1 || got.Queued[0].Content[0]["text"] != "follow-up work" {
		t.Fatalf("unexpected restored queued content: %+v", got.Queued[0].Content)
	}

	if err := m.Do("t1", core.Effect{Kind: core.EffectCancelMessage, MessageID: "m-queued"}); err != nil {
		t.Fatalf("cancel restored queued message failed: %v", err)
	}
	var cancelled bool
	for _, in := range host.snapshot() {
		if in.Kind == core.KindMessageState {
			if a, ok := in.Args.(core.MessageStateArgs); ok && a.ID == "m-queued" && a.State == "cancelled" {
				cancelled = true
			}
		}
	}
	if !cancelled {
		t.Fatal("expected cancelled message.state for m-queued")
	}
}

// V0.agy-reattach-allowlist: the adapter answers Fyr's own MCP tools from the
// launch's allowlist, which is not part of the ACP handshake. A process that
// ran with Fyr's MCP server is adopted with the same allowlist, its server
// name, and its timer policy; one that ran without it is not given Fyr's
// tools it never had.
func TestAttachPassesTheMCPAllowlistToSessionReattacher(t *testing.T) {
	for _, tc := range []struct {
		name  string
		meta  procMeta
		allow bool
	}{
		{"with Fyr's MCP server", procMeta{Thread: "t1", CLI: "reattach-stub", MCP: true}, true},
		{"without it", procMeta{Thread: "t1", CLI: "reattach-stub"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sock := runner.SocketPath(t.TempDir())
			srv := &runner.Server{Logf: t.Logf}
			if err := srv.Listen(sock); err != nil {
				t.Fatal(err)
			}
			go srv.Serve()
			t.Cleanup(func() { srv.Close(time.Second) })
			rc := &runner.Client{Path: sock}

			meta, _ := json.Marshal(tc.meta)
			conn, err := rc.Spawn(runner.Spawn{
				ID: "t1-proc", Key: "t1", Binary: "/bin/sh",
				Args: []string{"-c", "read _"}, Dir: t.TempDir(), Meta: meta,
			})
			if err != nil {
				t.Fatal(err)
			}
			conn.Detach()
			procs, err := rc.List()
			if err != nil || len(procs) != 1 {
				t.Fatalf("runner list: %+v %v", procs, err)
			}
			ad := &reattachStubAdapter{}
			host := &recordHost{th: core.Thread{ID: "t1", Profile: "p1", Cwd: t.TempDir()}}
			m := New(host, Config{
				Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "reattach-stub"} },
				Adapters: map[string]cli.Adapter{"reattach-stub": ad},
				Runner:   rc,
				Prompt:   "-",
			})
			t.Cleanup(func() {
				if c, err := rc.Attach("t1-proc"); err == nil {
					c.Kill()
					c.Forget()
				}
				m.StopAll()
			})
			m.Adopt(procs)

			ad.mu.Lock()
			got := ad.got
			ad.mu.Unlock()
			hasAllow := len(got.AllowedTools) == 1 && got.AllowedTools[0] == fyrMCPTools
			hasName := len(got.MCPServers) == 1 && got.MCPServers[0]["name"] == fyrMCPName
			hasTimers := len(got.DisallowedTools) > 0
			if hasAllow != tc.allow || hasName != tc.allow || hasTimers != tc.allow {
				t.Fatalf("allow=%v name=%v timers=%v, want %v: %+v", hasAllow, hasName, hasTimers, tc.allow, got)
			}
		})
	}
}

// V0.agy-reactive-wakeup: KindDigest updates Thread.Activity only while a run
// is active, so a late tool_result between runs never leaves stale Activity.
func TestKindDigestActivityRequiresRunning(t *testing.T) {
	idle := &core.Thread{ID: "t1"}
	afterIdle, _, _, err := core.Reduce(idle, &core.View{}, core.Input{
		Source: core.Observer(core.ObsProcess),
		Kind:   core.KindDigest,
		Args:   core.DigestArgs{Activity: "Thinking"},
	}, idle.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if afterIdle.Activity != "" {
		t.Fatalf("idle thread got Activity %q", afterIdle.Activity)
	}

	running := &core.Thread{ID: "t1", Conversation: core.Conversation{Running: true, Activity: "Fyr thread rename"}}
	afterRun, _, _, err := core.Reduce(running, &core.View{}, core.Input{
		Source: core.Observer(core.ObsProcess),
		Kind:   core.KindDigest,
		Args:   core.DigestArgs{Activity: "Thinking"},
	}, running.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if afterRun.Activity != "Thinking" {
		t.Fatalf("running thread Activity = %q, want Thinking", afterRun.Activity)
	}
}
