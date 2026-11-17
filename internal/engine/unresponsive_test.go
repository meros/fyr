package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	rnr "github.com/meros/fyr/internal/runner"
	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.143. The incident (t_165134a0049d9982, 17 Nov): the CLI's
// stdin was closed while a 2-hour background Bash ran. The CLI lived on for
// it and read nothing more; Fyr wrote the owner's messages and seven Stops
// into the closed pipe for 40 minutes and showed "Starting · 1 agent".
// The fake's "deaf N" reads nothing after its first turn, as that CLI did.

func ackOpts(ack, control, restart time.Duration) func(*agent.Config) {
	return func(c *agent.Config) {
		c.AckTimeout, c.ControlTimeout, c.RestartAfter = ack, control, restart
	}
}

func unresponsive(th core.Thread, _ core.Attention) bool { return th.Unresponsive != nil }

// (1), (2), (4): a message the CLI does not take within the ack timeout
// makes the thread "not responding" with the reason, in Needs you; the
// owner's Restart ends the process with its background task, resumes the
// session and delivers the unread message again.
func TestUnresponsiveAgentIsNamedAndRestarted(t *testing.T) {
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, ackOpts(300*time.Millisecond, time.Second, time.Minute))
	id := h.create("deaf 20000")
	h.wait(id, "the first turn ended with the task running", func(th core.Thread, _ core.Attention) bool {
		return !th.Running && th.Summary == "proxy started" && th.RunningTasks() == 1
	})
	if r := h.cmd("thread.restart", id, nil); r.OK {
		t.Fatal("restart was available while the agent responds")
	}
	msg := messageID(t, h.cmd("thread.send", id, core.Args{"text": "status?"}))
	th := h.wait(id, "not responding", unresponsive)
	if !strings.Contains(th.Unresponsive.Reason, "Run the proxy") {
		t.Fatalf("the reason does not name the background task: %q", th.Unresponsive.Reason)
	}
	a := core.Compute(&th, nil, time.Now())
	if a.State != core.Blocked || a.Section != core.NeedsYou || !strings.HasPrefix(a.Reason, "Agent not responding since ") {
		t.Fatalf("attention %+v", a)
	}
	if outboxState(th, msg) != "sent" {
		t.Fatalf("the message is not unread: %+v", th.Outbox)
	}
	if !hasEvent(h.events(id), "agent.unresponsive", "Run the proxy") {
		t.Fatal("no agent.unresponsive event")
	}
	if r := h.cmd("thread.restart", id, nil); !r.OK {
		t.Fatalf("restart: %+v", r)
	}
	h.waitEvent(id, msg, "read")
	th = h.wait(id, "done after the restart", func(th core.Thread, _ core.Attention) bool {
		return !th.Running && strings.Contains(th.Summary, "echo:")
	})
	if th.Unresponsive != nil || th.RunningTasks() != 0 {
		t.Fatalf("after the restart: unresponsive %+v, %d tasks", th.Unresponsive, th.RunningTasks())
	}
	evs := h.events(id)
	if !hasEvent(evs, "agent.restarted", "Run the proxy") {
		t.Fatal("no agent.restarted event naming the ended task")
	}
	if stateEvent(evs, msg, "lost") {
		t.Fatal("the message was marked lost")
	}
	if n := countEvents(evs, "user", `"id":"`+msg+`"`); n != 1 {
		t.Fatalf("the redelivered message shows %d times in the conversation", n)
	}
}

// (2): without the owner, Fyr restarts an unresponsive agent after
// RestartAfter.
func TestUnresponsiveAgentRestartsItself(t *testing.T) {
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, ackOpts(200*time.Millisecond, time.Second, 400*time.Millisecond))
	id := h.create("deaf 20000")
	h.wait(id, "first turn", func(th core.Thread, _ core.Attention) bool { return !th.Running && th.Summary == "proxy started" })
	msg := messageID(t, h.cmd("thread.send", id, core.Args{"text": "status?"}))
	h.waitEvent(id, msg, "read")
	if !hasEvent(h.events(id), "agent.restarted", "automatically") {
		t.Fatal("no automatic agent.restarted event")
	}
}

// (1): a Stop the CLI does not answer marks it unresponsive too.
func TestUnansweredStopMarksUnresponsive(t *testing.T) {
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, ackOpts(time.Minute, 300*time.Millisecond, time.Minute))
	id := h.create("deaf 20000")
	h.wait(id, "first turn", func(th core.Thread, _ core.Attention) bool { return !th.Running && th.Summary == "proxy started" })
	h.cmd("thread.send", id, core.Args{"text": "status?"})
	h.wait(id, "running", func(th core.Thread, _ core.Attention) bool { return th.Running })
	h.cmd("thread.interrupt", id, nil)
	th := h.wait(id, "not responding", unresponsive)
	if !strings.Contains(th.Unresponsive.Reason, "Stop") {
		t.Fatalf("reason %q", th.Unresponsive.Reason)
	}
}

// (3): a fyrd that adopts a process whose input is closed (the runner says
// so) never writes into it. The message waits, the thread says why at once,
// and when the CLI ends after its background work the message goes to a
// new process of the same session.
func TestAdoptedClosedInputIsNotWrittenTo(t *testing.T) {
	sock := rnr.SocketPath(t.TempDir())
	srv := &rnr.Server{} // no t.Logf: the runner logs exits from its own goroutines
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(time.Second) })
	rc := &rnr.Client{Path: sock}
	dir := t.TempDir()
	db := filepath.Join(dir, "fyr.db")

	h := startHarnessWith(t, dir, db, time.Minute, rc, nil)
	id := h.create("bgwait 2500")
	h.wait(id, "first turn", func(th core.Thread, _ core.Attention) bool { return !th.Running && th.Summary == "waiting for the build" })
	h.m.StopAll()
	h.e.st.Close()
	// An older fyrd closed the input while the task ran (before V0.133).
	procs, err := rc.List()
	if err != nil || len(procs) != 1 {
		t.Fatalf("runner list: %+v %v", procs, err)
	}
	conn, err := rc.Attach(procs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	conn.CloseInput()
	conn.Detach()
	deadline := time.Now().Add(5 * time.Second)
	for {
		procs, _ = rc.List()
		if len(procs) == 1 && procs[0].InputClosed && !procs[0].Linked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runner list: %+v", procs)
		}
		time.Sleep(20 * time.Millisecond)
	}

	h2 := startHarnessWith(t, dir, db, time.Minute, rc, agent.Kept(procs), ackOpts(time.Minute, time.Minute, time.Minute))
	h2.m.Adopt(procs)
	msg := messageID(t, h2.cmd("thread.send", id, core.Args{"text": "status?"}))
	th := h2.wait(id, "not responding at once", unresponsive)
	if !strings.Contains(th.Unresponsive.Reason, "input") {
		t.Fatalf("reason %q", th.Unresponsive.Reason)
	}
	h2.waitEvent(id, msg, "read")
	if stateEvent(h2.events(id), msg, "lost") {
		t.Fatal("the message was marked lost")
	}
	th = h2.wait(id, "answered", func(th core.Thread, _ core.Attention) bool { return !th.Running && strings.Contains(th.Summary, "echo:") })
	if th.Unresponsive != nil {
		t.Fatalf("still unresponsive: %+v", th.Unresponsive)
	}
}

func countEvents(evs []store.Event, typ, contains string) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ && strings.Contains(string(e.Data), contains) {
			n++
		}
	}
	return n
}
