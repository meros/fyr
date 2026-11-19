package engine

import (
	"log"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	rnr "github.com/meros/fyr/internal/runner"
)

// V0.tool-reload: an update changes the build's MCP tool list. The CLI
// reads the new list itself when its server stream reconnects (the mcp
// package); a process that did not (the fake speaks no MCP) is replaced at
// its next idle point.

// withTools is a fyrd whose tools hash to hash; current says which threads'
// processes read this build's tools.
func withTools(m *atomic.Pointer[agent.Manager], hash string, current func(thread string) bool) func(*agent.Config) {
	return func(c *agent.Config) {
		c.Tools = func() string { return hash }
		c.ToolsCurrent = func(tok string) bool {
			th, _ := m.Load().ThreadForToken(tok)
			return current(th)
		}
		c.ToolsGrace = 50 * time.Millisecond
		c.Build = "b-" + hash
	}
}

func runnerFor(t *testing.T) *rnr.Client {
	sock := rnr.SocketPath(t.TempDir())
	srv := &rnr.Server{Logf: log.Printf} // a process may exit after the test
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(time.Second) })
	return &rnr.Client{Path: sock}
}

func liveIs(h *harness, id string, want bool) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if h.m.Live(id) == want {
			return true
		}
	}
	return false
}

// exited waits until the runner holds no live process for the thread: the
// replaced CLI has ended.
func exited(t *testing.T, rc *rnr.Client, id string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		procs, _ := rc.List()
		if !agent.Kept(procs)[id] {
			return
		}
	}
	t.Fatalf("the old process of %s did not end", id)
}

// summaryIs: the run ended with this summary.
// lineIn waits for the thread's restart line.
func lineIn(h *harness, id, text string) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if hasEvent(h.events(id), "agent.restarted", text) {
			return true
		}
	}
	return false
}

func summaryIs(s string) func(core.Thread, core.Attention) bool {
	return func(t core.Thread, _ core.Attention) bool { return t.Summary == s && !t.Running }
}

// Two processes, an update that changes the tool hash: the idle one is
// replaced at once, the one in a turn only when the turn ends; each
// resumes its session on the next message, and each thread gets one line.
func TestToolReloadReplacesAtIdlePoint(t *testing.T) {
	rc := runnerFor(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "fyr.db")
	none := func(string) bool { return false }

	var m1 atomic.Pointer[agent.Manager]
	h := startHarnessWith(t, dir, db, time.Minute, rc, nil, withTools(&m1, "old", none))
	m1.Store(h.m)
	idle := h.create("first")
	h.wait(idle, "first done", summaryIs("echo: first"))
	busy := h.create("hold")
	h.wait(busy, "working", state(core.Working))
	h.m.StopAll() // the update: fyrd stops, the runner keeps both
	h.e.st.Close()

	procs, err := rc.List()
	if err != nil || len(procs) != 2 {
		t.Fatalf("runner list: %+v %v", procs, err)
	}
	var m2 atomic.Pointer[agent.Manager]
	h2 := startHarnessWith(t, dir, db, time.Minute, rc, agent.Kept(procs), withTools(&m2, "new", none))
	m2.Store(h2.m)
	h2.m.Adopt(procs)

	if !liveIs(h2, idle, false) {
		t.Fatal("the idle process with the old tools was not replaced")
	}
	const line = "Fyr restarted this agent's process to load the new tools (build b-new)"
	if !lineIn(h2, idle, line) {
		t.Fatal("the idle thread got no line")
	}
	time.Sleep(200 * time.Millisecond)
	if !h2.m.Live(busy) {
		t.Fatal("the process in a turn was cut")
	}
	if hasEvent(h2.events(busy), "agent.restarted", "") {
		t.Fatal("a line before the turn ended")
	}

	h2.cmd("thread.send", busy, core.Args{"text": "release"})
	h2.wait(busy, "turn ended", summaryIs("took in: release"))
	if !liveIs(h2, busy, false) {
		t.Fatal("the process was not replaced after its turn")
	}
	if !lineIn(h2, busy, line) {
		t.Fatal("the busy thread got no line")
	}

	for _, id := range []string{idle, busy} {
		exited(t, rc, id)
		before, _ := h2.e.Thread(id)
		h2.cmd("thread.send", id, core.Args{"text": "again " + id})
		th := h2.wait(id, "resumed", summaryIs("echo: again "+id))
		if th.SessionID != before.SessionID || th.Failure != nil {
			t.Fatalf("%s: session %q → %q, failure %+v", id, before.SessionID, th.SessionID, th.Failure)
		}
		n := 0
		for _, ev := range h2.events(id) {
			if ev.Type == "agent.restarted" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s: %d restart lines", id, n)
		}
	}
}

// A process whose CLI read the new tools itself (the server stream), or
// that started with the same tools, keeps running.
func TestToolReloadLeavesCurrentProcesses(t *testing.T) {
	rc := runnerFor(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "fyr.db")
	none := func(string) bool { return false }

	var m1 atomic.Pointer[agent.Manager]
	h := startHarnessWith(t, dir, db, time.Minute, rc, nil, withTools(&m1, "old", none))
	m1.Store(h.m)
	read := h.create("first")
	h.wait(read, "done", summaryIs("echo: first"))
	h.m.StopAll()
	h.e.st.Close()
	procs, _ := rc.List()

	var m2 atomic.Pointer[agent.Manager]
	h2 := startHarnessWith(t, dir, db, time.Minute, rc, agent.Kept(procs), withTools(&m2, "new", func(th string) bool { return th == read }))
	m2.Store(h2.m)
	h2.m.Adopt(procs)
	time.Sleep(300 * time.Millisecond)
	if !h2.m.Live(read) || hasEvent(h2.events(read), "agent.restarted", "") {
		t.Fatal("a process that read the new tools was replaced")
	}
	h2.seq = 100 // command ids of its own: h's are taken
	same := h2.create("second")
	h2.wait(same, "done", summaryIs("echo: second"))
	h2.m.StopAll()
	h2.e.st.Close()
	procs, _ = rc.List()

	var m3 atomic.Pointer[agent.Manager]
	h3 := startHarnessWith(t, dir, db, time.Minute, rc, agent.Kept(procs), withTools(&m3, "new", none))
	m3.Store(h3.m)
	h3.m.Adopt(procs)
	time.Sleep(300 * time.Millisecond)
	if !h3.m.Live(same) || hasEvent(h3.events(same), "agent.restarted", "") {
		t.Fatal("a process with the same tools was replaced")
	}
}
