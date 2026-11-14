package engine

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/claude/fake"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_CLAUDE") == "1" {
		fake.Main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type runner struct {
	mu    sync.Mutex
	calls []core.EffectReq
}

func (r *runner) Run(_ context.Context, _ string, req core.EffectReq) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return "merged", nil
}

func (r *runner) n() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

type harness struct {
	t   *testing.T
	e   *Engine
	m   *agent.Manager
	r   *runner
	dir string
	db  string
	seq int
}

func newHarness(t *testing.T, idle time.Duration) *harness {
	dir := t.TempDir()
	db := filepath.Join(dir, "fyr.db")
	return startHarness(t, dir, db, idle)
}

func startHarness(t *testing.T, dir, db string, idle time.Duration) *harness {
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, e: e, r: &runner{}, dir: dir, db: db}
	var m *agent.Manager
	srv := mcp.New(func(tok string) (string, bool) { return m.ThreadForToken(tok) })
	e.Tools(srv)
	hs := httptest.NewServer(srv)
	m = agent.New(e, agent.Config{
		Profile: func(string) agent.Profile {
			return agent.Profile{ID: "fake", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}}
		},
		IdleStop: idle, MCPURL: hs.URL + "/mcp",
	})
	e.Agents, e.Effects, h.m = m, h.r, m
	t.Cleanup(func() { m.StopAll(); hs.Close(); st.Close() })
	return h
}

func (h *harness) cmd(action, thread string, args core.Args) Reply {
	h.seq++
	return h.cmdID(action+"-"+string(rune('a'+h.seq)), action, thread, args)
}

func (h *harness) cmdID(id, action, thread string, args core.Args) Reply {
	h.t.Helper()
	return h.e.Command(Cmd{CommandID: id, Action: action, ThreadID: thread, Args: args})
}

func (h *harness) create(text string) string {
	h.t.Helper()
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": text})
	if !r.OK {
		h.t.Fatalf("create: %+v", r)
	}
	return r.ThreadID
}

func (h *harness) wait(id string, what string, ok func(t core.Thread, a core.Attention) bool) core.Thread {
	h.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		t, _ := h.e.Thread(id)
		v := h.e.view(id)
		if ok(t, core.Compute(&t, v, time.Now())) {
			return t
		}
		time.Sleep(20 * time.Millisecond)
	}
	t, _ := h.e.Thread(id)
	b, _ := json.Marshal(t)
	h.t.Fatalf("timeout waiting for %s: %s", what, b)
	return t
}

func state(s core.State) func(core.Thread, core.Attention) bool {
	return func(_ core.Thread, a core.Attention) bool { return a.State == s }
}

func (h *harness) events(id string) []store.Event {
	evs, err := h.e.Events(id, 0, 1000)
	if err != nil {
		h.t.Fatal(err)
	}
	return evs
}

func hasEvent(evs []store.Event, typ, contains string) bool {
	for _, e := range evs {
		if e.Type == typ && strings.Contains(string(e.Data), contains) {
			return true
		}
	}
	return false
}

// SPEC 0.5 V0.3, 6.2: a run goes working → done, with its timeline.
func TestRunLifecycle(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello there")
	th := h.wait(id, "done", state(core.Done))
	if th.SessionID == "" || th.Summary != "echo: hello there" || th.Title != "hello there" {
		t.Fatalf("thread %+v", th)
	}
	evs := h.events(id)
	for _, typ := range []string{"user", "run.started", "msg", "run.ended"} {
		if !hasEvent(evs, typ, "") {
			t.Errorf("missing event %s", typ)
		}
	}
	h.cmd("thread.markSeen", id, nil)
	h.wait(id, "seen", state(core.Seen))
}

// SPEC I7: the same command id twice changes state once.
func TestCommandExactlyOnce(t *testing.T) {
	h := newHarness(t, time.Minute)
	r1 := h.cmdID("same", "thread.create", "", core.Args{"cwd": h.dir, "text": "x"})
	r2 := h.cmdID("same", "thread.create", "", core.Args{"cwd": h.dir, "text": "x"})
	if r1.ThreadID != r2.ThreadID || len(h.e.Snapshot()) != 1 {
		t.Fatalf("%+v %+v threads=%d", r1, r2, len(h.e.Snapshot()))
	}
}

// SPEC 5.6, 13.2: AskUserQuestion becomes an ask Decision; the answer
// reaches the agent.
func TestAskRoundTrip(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("ask")
	th := h.wait(id, "asking", state(core.Asking))
	d := th.Decisions[0]
	if d.Kind != "ask" || d.Questions[0].Question != "Which colour?" || len(d.Questions[0].Options) != 2 {
		t.Fatalf("decision %+v", d)
	}
	if r := h.cmd("thread.send", id, core.Args{"text": "hi"}); r.OK {
		t.Fatal("send while asking must be refused")
	}
	r := h.cmd("thread.answer", id, core.Args{"decisionId": d.ID, "answers": map[string]any{"Which colour?": "Blue"}})
	if !r.OK {
		t.Fatalf("answer %+v", r)
	}
	th = h.wait(id, "done", state(core.Done))
	if th.Summary != "You chose Blue" {
		t.Fatalf("summary %q", th.Summary)
	}
}

// SPEC 0.5 V0.3: an interrupt ends the run without a failure.
func TestInterruptIsNotAFailure(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hang")
	h.wait(id, "working with activity", func(t core.Thread, a core.Attention) bool {
		return a.State == core.Working && strings.HasPrefix(t.Activity, "Bash sleep")
	})
	if r := h.cmd("thread.interrupt", id, nil); !r.OK {
		t.Fatalf("%+v", r)
	}
	th := h.wait(id, "not running", func(t core.Thread, _ core.Attention) bool { return !t.Running })
	if th.Failure != nil {
		t.Fatalf("interrupt became a failure: %+v", th.Failure)
	}
}

// SPEC 0.5 V0.3, 3.3: an API error is failed with class api; Retry resumes.
func TestFailureAndRetry(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("fail")
	th := h.wait(id, "failed", state(core.Failed))
	if th.Failure.Class != "api" {
		t.Fatalf("class %q", th.Failure.Class)
	}
	h.cmd("thread.markSeen", id, nil)
	if a := core.Compute(&th, h.e.view(id), time.Now()); a.State != core.Failed {
		t.Fatal("a visit hid the failure")
	}
	if r := h.cmd("thread.retry", id, nil); !r.OK {
		t.Fatalf("%+v", r)
	}
	th = h.wait(id, "done after retry", state(core.Done))
	if !strings.HasPrefix(th.Summary, "echo: The last run stopped") {
		t.Fatalf("summary %q", th.Summary)
	}
}

// SPEC 0.5 V0.3: a crash during a run is failed with class process and the stderr tail.
func TestCrashIsAFailure(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("crash")
	th := h.wait(id, "failed", state(core.Failed))
	if th.Failure.Class != "process" || !strings.Contains(th.Failure.Message, "boom") {
		t.Fatalf("failure %+v", th.Failure)
	}
}

// SPEC 0.5 V0.7: an idle process stops; the next message resumes the same session.
func TestIdleStopAndResume(t *testing.T) {
	h := newHarness(t, 150*time.Millisecond)
	id := h.create("first")
	th := h.wait(id, "done", state(core.Done))
	sid := th.SessionID
	deadline := time.Now().Add(5 * time.Second)
	for h.m.Live(id) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if h.m.Live(id) {
		t.Fatal("process did not stop when idle")
	}
	if th.Failure != nil {
		t.Fatal("idle stop became a failure")
	}
	h.cmd("thread.send", id, core.Args{"text": "second"})
	th = h.wait(id, "second done", func(t core.Thread, _ core.Attention) bool { return t.Summary == "echo: second" })
	if th.SessionID != sid || th.Failure != nil {
		t.Fatalf("session %q → %q, failure %v", sid, th.SessionID, th.Failure)
	}
}

// SPEC 0.5 V0.6: a host restart turns live work into failed/restart.
func TestRestartRecovery(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hang")
	h.wait(id, "working", state(core.Working))
	h.m.StopAll() // the processes die with the old host…
	h.e.st.Close()
	// …and a new host opens the same database. Its Load runs before any
	// late observation from the old one could matter.
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	th, _ := e2.Thread(id)
	if th.Running || th.Failure == nil || (th.Failure.Class != "restart" && th.Failure.Class != "process") {
		t.Fatalf("after restart: running=%v failure=%+v", th.Running, th.Failure)
	}
}

// SPEC 0.5 V0.8: gh pr create output links the PR.
func TestPRLinkedFromGhPrCreate(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("pr")
	th := h.wait(id, "pr", func(t core.Thread, _ core.Attention) bool { return t.PR != nil })
	if th.PR.Repo != "acme/app" || th.PR.Number != 42 {
		t.Fatalf("pr %+v", th.PR)
	}
}

// SPEC 0.5 V0.12, 12.3: the gate hook denies gh pr merge with the redirect.
func TestGateHookDeniesMerge(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("gate")
	th := h.wait(id, "done", state(core.Done))
	if !strings.Contains(th.Summary, "deny") || !strings.Contains(th.Summary, "effect_request") {
		t.Fatalf("summary %q", th.Summary)
	}
}

// SPEC 0.5 V0.12, 5.10: effect_request → asking → approve runs once → the
// agent gets the result in a new turn.
func TestEffectMergeLoop(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("merge https://github.com/acme/app/pull/42")
	th := h.wait(id, "asking (effect) and run ended", func(t core.Thread, a core.Attention) bool {
		return a.State == core.Asking && !t.Running
	})
	d := th.Decisions[0]
	if d.Kind != "effect" || d.Effect.Action != "pr.merge" || d.Effect.Args["pr"] != "https://github.com/acme/app/pull/42" {
		t.Fatalf("decision %+v", d)
	}
	if !strings.Contains(th.Summary, "tool said: Requested") {
		t.Fatalf("the tool did not return at once: %q", th.Summary)
	}
	r1 := h.cmd("thread.answer", id, core.Args{"decisionId": d.ID, "allow": true})
	r2 := h.cmd("thread.answer", id, core.Args{"decisionId": d.ID, "allow": true})
	if !r1.OK || r2.OK {
		t.Fatalf("first %+v second %+v", r1, r2)
	}
	th = h.wait(id, "agent got the result", func(t core.Thread, _ core.Attention) bool {
		return strings.HasPrefix(t.Summary, "echo: [Fyr] Your effect_request pr.merge")
	})
	if h.r.n() != 1 {
		t.Fatalf("runner called %d times", h.r.n())
	}
	if !hasEvent(h.events(id), "effect.result", `"ok":true`) {
		t.Fatal("no effect.result event")
	}
	if !strings.Contains(th.Summary, "succeeded") {
		t.Fatalf("summary %q", th.Summary)
	}
}

// SPEC 5.10: a declined effect is reported to the agent and never runs.
func TestEffectDeclined(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("merge https://github.com/acme/app/pull/7")
	th := h.wait(id, "asking", func(t core.Thread, a core.Attention) bool { return a.State == core.Asking && !t.Running })
	h.cmd("thread.answer", id, core.Args{"decisionId": th.Decisions[0].ID, "allow": false})
	th = h.wait(id, "declined delivered", func(t core.Thread, _ core.Attention) bool {
		return strings.Contains(t.Summary, "declined")
	})
	if h.r.n() != 0 {
		t.Fatal("a declined effect ran")
	}
}

// SPEC 0.5 V0.2: rev increases on every change, also across a restart.
func TestRevIsMonotonic(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	rev := func(e *Engine) int64 {
		for _, d := range e.Snapshot() {
			if d.ID == id {
				return d.Rev
			}
		}
		return 0
	}
	r1 := rev(h.e)
	h.cmd("thread.markSeen", id, nil)
	r2 := rev(h.e)
	if r1 == 0 || r2 <= r1 {
		t.Fatalf("rev %d → %d", r1, r2)
	}
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	e2.Load()
	if r3 := rev(e2); r3 <= r2 {
		t.Fatalf("rev after restart %d <= %d", r3, r2)
	}
}

// SPEC 0.5 V0.2: events page back with before.
func TestEventsBefore(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	all := h.events(id)
	if len(all) < 4 {
		t.Fatalf("only %d events", len(all))
	}
	last := all[len(all)-1].Seq
	page, err := h.e.EventsBefore(id, last, 2)
	if err != nil || len(page) != 2 || page[1].Seq >= last || page[0].Seq >= page[1].Seq {
		t.Fatalf("page %+v %v", page, err)
	}
}

// SPEC 0.5 V0.2: the branch of the checkout is read after each run.
func TestBranchObserved(t *testing.T) {
	h := newHarness(t, time.Minute)
	for _, args := range [][]string{{"init", "-q", "-b", "feat/lasso"}, {"commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = h.dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
	id := h.create("hello")
	h.wait(id, "branch", func(t core.Thread, _ core.Attention) bool { return t.Branch == "feat/lasso" })
}

// SPEC 16.2 MCP binding: the token works from the moment the process
// starts, because the CLI connects its MCP servers before initialize.
func TestMCPTokenValidAtStartup(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("mcpstatus")
	th := h.wait(id, "done", state(core.Done))
	if th.Summary != "mcp: ok" {
		t.Fatalf("summary %q", th.Summary)
	}
}

// SPEC 0.5 V0.15: the host pushes when a thread enters asking or failed,
// once per transition; a snapshot or a visit does not push again.
func TestAttentionTransitionsNotify(t *testing.T) {
	h := newHarness(t, time.Minute)
	var mu sync.Mutex
	got := []string{}
	h.e.OnAttention = func(d DTO, prev core.State) {
		mu.Lock()
		got = append(got, string(prev)+"→"+string(d.Attention.State))
		mu.Unlock()
	}
	count := func(to string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, g := range got {
			if strings.HasSuffix(g, "→"+to) {
				n++
			}
		}
		return n
	}
	id := h.create("ask")
	th := h.wait(id, "asking", state(core.Asking))
	h.e.Snapshot()
	h.cmd("thread.markSeen", id, nil)
	if count("asking") != 1 {
		t.Fatalf("asking notified %d times: %v", count("asking"), got)
	}
	h.cmd("thread.answer", id, core.Args{"decisionId": th.Decisions[0].ID, "answers": map[string]any{"Which colour?": "Red"}})
	h.wait(id, "done", state(core.Done))
	id2 := h.create("fail")
	h.wait(id2, "failed", state(core.Failed))
	if count("failed") != 1 {
		t.Fatalf("failed notified %d times: %v", count("failed"), got)
	}
}

// SPEC 0.5 V0.15: a host restart does not push old states again.
func TestRestartDoesNotRenotify(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("fail")
	h.wait(id, "failed", state(core.Failed))
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	e2.Load()
	n := 0
	e2.OnAttention = func(DTO, core.State) { n++ }
	e2.Tick()
	if n != 0 {
		t.Fatalf("restart re-notified %d threads", n)
	}
}

// SPEC 6.2, 0.5 V0.3: a turn the CLI starts on its own (a background task
// finished) is a run too: working while it goes, done when it ends.
func TestTurnWithoutUserMessageIsARun(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("bg")
	h.wait(id, "working again in the second turn", func(t core.Thread, _ core.Attention) bool {
		return t.Running && t.Summary == "background task done"
	})
	th := h.wait(id, "done after the second turn", state(core.Done))
	starts := 0
	for _, e := range h.events(id) {
		if e.Type == "run.started" {
			starts++
		}
	}
	if starts != 2 || th.Running {
		t.Fatalf("run.started %d, running %v", starts, th.Running)
	}
}
