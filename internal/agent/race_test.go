// #40: one process per session when starts race, and a handover brief
// matched to its own message. The race detector needs CGO, so the tests
// make the overlap certain: a start hook holds initialize open while the
// other caller arrives, and a scripted session orders the stream.
package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
)

// stubAdapter is a CLI adapter whose sessions are scripted in Go.
type stubAdapter struct {
	starts atomic.Int32
	// gate, when set, holds every Initialize until it is closed.
	gate chan struct{}
	// onSend scripts the stream after each user message.
	onSend func(s *stubSession, text, uuid string)

	mu       sync.Mutex
	sessions []*stubSession
}

func (a *stubAdapter) ID() string                          { return "stub" }
func (a *stubAdapter) Caps() cli.Caps                      { return cli.Caps{} }
func (a *stubAdapter) Env(cli.Launch) ([]string, error)    { return nil, nil }
func (a *stubAdapter) SessionsDir(cli.Auth) string         { return "" }
func (a *stubAdapter) SessionFile(_, _, _ string) string   { return "" }
func (a *stubAdapter) LoggedIn(cli.Auth) bool              { return true }
func (a *stubAdapter) CopySession(_, _, _, _ string) error { return nil }

func (a *stubAdapter) Start(l cli.Launch, _ cli.Handlers) (cli.Session, error) {
	a.starts.Add(1)
	s := &stubSession{a: a, model: l.Model, out: make(chan cli.Msg, 64), done: make(chan struct{})}
	a.mu.Lock()
	a.sessions = append(a.sessions, s)
	a.mu.Unlock()
	return s, nil
}

// running counts the sessions not closed.
func (a *stubAdapter) running() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, s := range a.sessions {
		select {
		case <-s.done:
		default:
			n++
		}
	}
	return n
}

type stubSession struct {
	a     *stubAdapter
	model string
	out   chan cli.Msg
	done  chan struct{}
	once  sync.Once
}

func (s *stubSession) Out() <-chan cli.Msg                    { return s.out }
func (s *stubSession) Done() <-chan struct{}                  { return s.done }
func (s *stubSession) Err() error                             { return nil }
func (s *stubSession) Interrupt(context.Context) error        { return nil }
func (s *stubSession) SetModel(context.Context, string) error { return nil }
func (s *stubSession) CancelMessage(context.Context, string) (bool, error) {
	return false, nil
}
func (s *stubSession) Close() error {
	s.once.Do(func() { close(s.out); close(s.done) })
	return nil
}
func (s *stubSession) Kill() { s.Close() }

func (s *stubSession) Initialize(ctx context.Context, _ cli.Msg) (cli.Msg, error) {
	if s.a.gate != nil {
		select {
		case <-s.a.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return cli.Msg{}, nil
}

func (s *stubSession) SendUser(content []cli.Msg, uuid string) error {
	text := ""
	for _, c := range content {
		if t, ok := c["text"].(string); ok {
			text += t
		}
	}
	if s.a.onSend != nil {
		s.a.onSend(s, text, uuid)
	}
	return nil
}

func (s *stubSession) emit(msgs ...cli.Msg) {
	for _, m := range msgs {
		s.out <- m
	}
}

func lifecycle(uuid, state string) cli.Msg {
	return cli.Msg{"type": "command_lifecycle", "command_uuid": uuid, "state": state}
}

func resultMsg(text string) cli.Msg {
	return cli.Msg{"type": "result", "subtype": "success", "is_error": false, "result": text}
}

// stubHost is a host with one thread per id, all in one folder.
type stubHost struct{ cwd string }

func (stubHost) Apply(core.Input) (any, error) { return nil, nil }
func (h stubHost) Thread(id string) (core.Thread, bool) {
	return core.Thread{ID: id, Profile: "stub", Cwd: h.cwd, SessionID: "s1"}, true
}

func stubManager(t *testing.T, a *stubAdapter) *Manager {
	t.Helper()
	m := New(stubHost{cwd: t.TempDir()}, Config{
		Profile:  func(id string) Profile { return Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": a},
		Prompt:   "-",
	})
	t.Cleanup(m.StopAll)
	return m
}

// waitFor polls cond for up to d.
func waitFor(d time.Duration, cond func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// #40: two sends to a stopped thread start one process.
func TestConcurrentSendsStartOneProcess(t *testing.T) {
	a := &stubAdapter{gate: make(chan struct{})}
	m := stubManager(t, a)
	errs := make(chan error, 2)
	send := func(text string) {
		errs <- m.Do("t1", core.Effect{Kind: core.EffectSend, Text: text})
	}
	go send("from the user")
	if !waitFor(2*time.Second, func() bool { return a.starts.Load() == 1 }) {
		t.Fatal("the first send did not start a process")
	}
	// The first start now waits in initialize; the second send arrives.
	go send("from an action")
	waitFor(300*time.Millisecond, func() bool { return a.starts.Load() > 1 })
	close(a.gate)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if n := a.starts.Load(); n != 1 {
		t.Fatalf("%d processes started for one session, want 1", n)
	}
}

// #40: a handover start that races a send ends with one process, the
// handover's, and the send's process does not stay behind.
func TestHandoverStartRacingSendLeavesOneProcess(t *testing.T) {
	a := &stubAdapter{gate: make(chan struct{})}
	m := stubManager(t, a)
	errs := make(chan error, 2)
	go func() { errs <- m.Do("t1", core.Effect{Kind: core.EffectSend, Text: "hi"}) }()
	if !waitFor(2*time.Second, func() bool { return a.starts.Load() == 1 }) {
		t.Fatal("the send did not start a process")
	}
	var got *session
	go func() {
		s, err := m.ensureAs("t1", &as{profile: Profile{ID: "other", CLI: "stub"}, model: "target", session: "s2"})
		got = s
		errs <- err
	}()
	waitFor(300*time.Millisecond, func() bool { return a.starts.Load() > 1 })
	close(a.gate)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if n := a.running(); n != 1 {
		t.Fatalf("%d processes run for one session, want 1", n)
	}
	m.mu.Lock()
	cur := m.sessions["t1"]
	m.mu.Unlock()
	if cur == nil || cur != got || cur.c.(*stubSession).model != "target" {
		t.Fatalf("the thread's process is not the handover's")
	}
}

// #40: a brief sent while a turn runs takes the result of its own turn,
// not the running one's. This CLI reports no command_lifecycle (an older
// CLI), so only waiting for the running turn tells the results apart.
func TestBriefWaitsForRunningTurn(t *testing.T) {
	a := &stubAdapter{}
	var mu sync.Mutex
	var cur *stubSession
	busy, queued := false, false
	a.onSend = func(s *stubSession, text, uuid string) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case text == "work":
			cur, busy = s, true
			s.emit(cli.Msg{"type": "system", "subtype": "init"}) // the turn runs on
		case busy:
			queued = true // the CLI takes it after the running turn
		default:
			s.emit(cli.Msg{"type": "system", "subtype": "init"}, resultMsg("the handover"))
		}
	}
	m := stubManager(t, a)
	if err := m.Do("t1", core.Effect{Kind: core.EffectSend, Text: "work"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		out, err := m.brief("t1", Profile{ID: "other"})
		if err != nil {
			out = "error: " + err.Error()
		}
		done <- out
	}()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	busy = false
	cur.emit(resultMsg("old work done"))
	if queued {
		cur.emit(cli.Msg{"type": "system", "subtype": "init"}, resultMsg("the handover"))
	}
	mu.Unlock()
	select {
	case out := <-done:
		if out != "the handover" {
			t.Fatalf("brief = %q, want the reply to the brief", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("brief did not return")
	}
}

// #40: a turn the CLI starts on its own just as the brief is sent ends
// first; its result is not the brief.
func TestBriefMatchesItsOwnMessage(t *testing.T) {
	a := &stubAdapter{}
	a.onSend = func(s *stubSession, text, uuid string) {
		s.emit(lifecycle(uuid, "queued"),
			cli.Msg{"type": "system", "subtype": "init"},
			resultMsg("background task done"),
			lifecycle(uuid, "started"),
			cli.Msg{"type": "system", "subtype": "init"},
			resultMsg("the handover"),
			lifecycle(uuid, "completed"))
	}
	m := stubManager(t, a)
	if _, err := m.ensure("t1"); err != nil {
		t.Fatal(err)
	}
	out, err := m.brief("t1", Profile{ID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "the handover" {
		t.Fatalf("brief = %q, want %q", out, "the handover")
	}
}
