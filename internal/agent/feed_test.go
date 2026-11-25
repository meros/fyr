package agent

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/claude/fake"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_CLAUDE") == "1" {
		fake.Main()
		return
	}
	os.Exit(m.Run())
}

// feedHost is a host with one thread whose feed holds notices; it records
// every take and the agent's last summary.
type feedHost struct {
	cwd string

	mu      sync.Mutex
	queue   []core.Notice
	takes   []core.Point
	summary string
}

func (h *feedHost) Apply(in core.Input) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch in.Kind {
	case core.KindFeedTake:
		a := in.Args.(core.FeedTake)
		h.takes = append(h.takes, a.At)
		if a.At != core.AtTool {
			return []core.Notice(nil), nil
		}
		ns := h.queue
		h.queue = nil
		return ns, nil
	case core.KindDigest:
		if d := in.Args.(core.DigestArgs); d.Summary != "" {
			h.summary = d.Summary
		}
	}
	return nil, nil
}

func (h *feedHost) Thread(id string) (core.Thread, bool) {
	return core.Thread{ID: id, Profile: "fake", Cwd: h.cwd, SessionID: "s1"}, true
}

func (h *feedHost) state() (string, []core.Point, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.summary, append([]core.Point(nil), h.takes...), len(h.queue)
}

// fakeManager runs the fake CLI with Fyr's tools, so the feed's hooks are
// registered.
func fakeManager(t *testing.T, h *feedHost) *Manager {
	t.Helper()
	m := New(h, Config{
		Profile: func(string) provider.Profile {
			return provider.Profile{ID: "fake", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}}
		},
		MCPURL: "http://127.0.0.1:1/mcp",
		Prompt: "-",
	})
	t.Cleanup(m.StopAll)
	return m
}

// #49: a subagent's tool call takes no notice and leaves the queue as it
// is; the next top-level tool call takes it.
func TestToolHookSkipsSubagent(t *testing.T) {
	h := &feedHost{cwd: t.TempDir(), queue: []core.Notice{{About: "thread", What: "Renamed."}}}
	m := fakeManager(t, h)
	if err := m.Do("t1", core.Effect{Kind: core.EffectSend, Text: "subtool"}); err != nil {
		t.Fatal(err)
	}
	var summary string
	var takes []core.Point
	if !waitFor(8*time.Second, func() bool { summary, takes, _ = h.state(); return summary != "" }) {
		t.Fatal("the run did not end")
	}
	if want := "sub:  top: [Fyr] Renamed."; summary != want {
		t.Fatalf("the agent read %q, want %q", summary, want)
	}
	tool := 0
	for _, p := range takes {
		if p == core.AtTool {
			tool++
		}
	}
	if tool != 1 {
		t.Fatalf("%d takes at a tool call, want 1 (the top-level one): %v", tool, takes)
	}
}

// #49: a hook input that names a subagent takes nothing, at a tool call or
// at a stop. The CLI sends a subagent's stop as SubagentStop, which Fyr
// registers for V0.subagent-admission (TestSubagentHooksCarryItsAdmission).
func TestSubagentHooksTakeNothing(t *testing.T) {
	h := &feedHost{cwd: t.TempDir(), queue: []core.Notice{{About: "thread", What: "Renamed."}}}
	m := fakeManager(t, h)
	s := &session{thread: "t1", changeTools: true}
	in := claude.Msg{"hook_event_name": "PostToolUse", "agent_id": "a1", "agent_type": "general-purpose", "tool_name": "Read"}
	if r := m.hook(s, "viewnudge", in); r != nil {
		t.Fatalf("a subagent's tool call got %v", r)
	}
	in = claude.Msg{"hook_event_name": "Stop", "agent_id": "a1", "agent_type": "general-purpose", "stop_hook_active": false}
	if r := m.hook(s, "view", in); r != nil {
		t.Fatalf("a subagent's stop got %v", r)
	}
	if _, takes, q := h.state(); len(takes) != 0 || q != 1 {
		t.Fatalf("takes %v, queue %d", takes, q)
	}
}

// V0.subagent-admission: a subagent a hook names runs until its SubagentStop;
// its tool call's result carries what waits for it (its heavy job's
// admission), and its stop hands what is left to the engine.
func TestSubagentHooksCarryItsAdmission(t *testing.T) {
	h := &feedHost{cwd: t.TempDir(), queue: []core.Notice{{About: "thread", What: "Renamed."}}}
	m := fakeManager(t, h)
	s := &session{thread: "t1", changeTools: true}
	m.mu.Lock()
	m.sessions["t1"] = s
	m.mu.Unlock()
	t.Cleanup(func() { // before StopAll: the session has no process to close
		m.mu.Lock()
		delete(m.sessions, "t1")
		m.mu.Unlock()
	})
	ended := make(chan string, 1)
	m.SetSubagentHooks(func(thread, agent string) string {
		if thread == "t1" && agent == "a1" {
			return "[Fyr] Your test job is admitted now: run the command again."
		}
		return ""
	}, func(thread, agent string) { ended <- thread + "/" + agent })
	if m.SubagentLive("t1", "a1") {
		t.Fatal("live before any hook named it")
	}
	in := claude.Msg{"hook_event_name": "PostToolUse", "agent_id": "a1", "agent_type": "worker", "tool_name": "Read"}
	r := m.hook(s, "viewnudge", in)
	out, _ := r["hookSpecificOutput"].(claude.Msg)
	if out["additionalContext"] != "[Fyr] Your test job is admitted now: run the command again." {
		t.Fatalf("the subagent's tool call got %v", r)
	}
	if !m.SubagentLive("t1", "a1") {
		t.Fatal("not live after its tool call")
	}
	in["agent_id"] = "a2"
	if r := m.hook(s, "viewnudge", in); r != nil {
		t.Fatalf("another subagent got %v", r)
	}
	if _, takes, q := h.state(); len(takes) != 0 || q != 1 {
		t.Fatalf("a subagent took the thread's feed: takes %v, queue %d", takes, q)
	}
	if r := m.hook(s, "substop", claude.Msg{"hook_event_name": "SubagentStop", "agent_id": "a1", "agent_type": "worker"}); r != nil {
		t.Fatalf("SubagentStop got %v", r)
	}
	if got := <-ended; got != "t1/a1" {
		t.Fatalf("ended %q", got)
	}
	if m.SubagentLive("t1", "a1") || !m.SubagentLive("t1", "a2") {
		t.Fatal("liveness after the stop")
	}
	if _, ok := initHooks(false)["SubagentStop"]; !ok {
		t.Fatal("SubagentStop is not registered")
	}
}
