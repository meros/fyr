package engine

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.153. The evidence (17 Nov, 14:04–14:11): systemd-oomd killed
// three running agents for fyr.service's memory pressure, and both restarts
// were killed by Fyr's own 60 s initialize deadline in the same thrash, so
// the thread said "Could not start the agent". Under pressure Fyr waits.

type pressure struct{ on atomic.Bool }

func (p *pressure) get() (bool, string) {
	if p.on.Load() {
		return true, "memory pressure"
	}
	return false, ""
}

// slowStart: the fake answers initialize after the ms in the returned file.
func slowStart(t *testing.T, ms string, init, maxWait time.Duration, p *pressure) (func(*agent.Config), string) {
	f := filepath.Join(t.TempDir(), "init-delay")
	os.WriteFile(f, []byte(ms), 0o600)
	return func(c *agent.Config) {
		c.Profile = func(string) provider.Profile {
			return provider.Profile{ID: "fake", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1", "FYR_FAKE_INIT_DELAY_FILE=" + f}}
		}
		c.InitTimeout, c.PressureMaxWait, c.Pressure = init, maxWait, p.get
	}, f
}

// (1): a start slower than the initialize deadline goes on while the host
// thrashes, and the thread says it waits for the machine, not a failure.
func TestSlowStartWaitsUnderPressure(t *testing.T) {
	p := &pressure{}
	p.on.Store(true)
	opt, _ := slowStart(t, "1200", 300*time.Millisecond, 10*time.Second, p)
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, opt)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "hello"})
	id := r.ThreadID
	h.wait(id, "waiting for the machine", func(th core.Thread, a core.Attention) bool {
		return th.MachineWait != nil && th.MachineWait.Why == "memory pressure"
	})
	th := h.wait(id, "answered", func(th core.Thread, _ core.Attention) bool {
		return !th.Running && strings.HasPrefix(th.Summary, "echo:")
	})
	if th.MachineWait != nil || th.Failure != nil {
		t.Fatalf("after the start: wait %+v failure %+v", th.MachineWait, th.Failure)
	}
}

// (1): on a calm host the deadline holds as before: the start fails.
func TestSlowStartFailsOnCalmHost(t *testing.T) {
	opt, _ := slowStart(t, "1200", 300*time.Millisecond, 10*time.Second, &pressure{})
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, opt)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "hello"})
	th := h.wait(r.ThreadID, "failed", state(core.Failed))
	if !strings.Contains(th.Failure.Message, "deadline") || th.MachineWait != nil {
		t.Fatalf("failure %+v wait %+v", th.Failure, th.MachineWait)
	}
}

// (2): a start that fails under pressure is tried again; once the host
// recovers the message is read.
func TestFailedStartRetriesUnderPressure(t *testing.T) {
	p := &pressure{}
	p.on.Store(true)
	// The deadline stretches to InitTimeout + PressureMaxWait; a short
	// PressureMaxWait makes the first start fail under pressure.
	opt, f := slowStart(t, "5000", 200*time.Millisecond, 600*time.Millisecond, p)
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, opt)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "hello"})
	id := r.ThreadID
	h.wait(id, "waiting for the machine", func(th core.Thread, _ core.Attention) bool { return th.MachineWait != nil })
	os.WriteFile(f, []byte("0"), 0o600) // the host recovers
	th := h.wait(id, "answered", func(th core.Thread, _ core.Attention) bool {
		return !th.Running && strings.HasPrefix(th.Summary, "echo:")
	})
	if th.Failure != nil {
		t.Fatalf("failure %+v", th.Failure)
	}
}

// (3): an unresponsive agent is not restarted while the host thrashes;
// the restart comes once it is calm.
func TestNoAutoRestartUnderPressure(t *testing.T) {
	p := &pressure{}
	dir := t.TempDir()
	h := startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, ackOpts(200*time.Millisecond, time.Second, 300*time.Millisecond),
		func(c *agent.Config) { c.Pressure = p.get })
	id := h.create("deaf 20000")
	h.wait(id, "first turn", func(th core.Thread, _ core.Attention) bool { return !th.Running && th.Summary == "proxy started" })
	h.cmd("thread.send", id, core.Args{"text": "status?"})
	h.wait(id, "not responding", unresponsive)
	p.on.Store(true)
	time.Sleep(900 * time.Millisecond)
	if hasEvent(h.events(id), "agent.restarted", "") {
		t.Fatal("restarted while the host thrashes")
	}
	p.on.Store(false)
	h.wait(id, "restarted once calm", func(core.Thread, core.Attention) bool {
		return hasEvent(h.events(id), "agent.restarted", "automatically")
	})
}

// V0.machine-wait-restart: the host dies while a start waits for the machine
// (18 Nov, 15:30: systemd-oomd killed it). The next host shows no wait, and
// the thread is not left in Working with nothing going on.
func TestMachineWaitDoesNotOutliveTheHost(t *testing.T) {
	p := &pressure{}
	p.on.Store(true)
	opt, _ := slowStart(t, "60000", 100*time.Millisecond, time.Minute, p)
	dir := t.TempDir()
	db := filepath.Join(dir, "fyr.db")
	h := startHarnessWith(t, dir, db, time.Minute, nil, nil, opt)
	id := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "hello"}).ThreadID
	h.wait(id, "waiting for the machine", func(th core.Thread, _ core.Attention) bool { return th.MachineWait != nil })
	h.e.st.Close() // nothing the old host does from here on reaches the database
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if th, _ := e2.Thread(id); th.MachineWait != nil {
		t.Fatalf("the wait outlived the host: %+v", th.MachineWait)
	}
	st.Close()
	st2, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	e3 := New(st2)
	if err := e3.Load(); err != nil {
		t.Fatal(err)
	}
	if th, _ := e3.Thread(id); th.MachineWait != nil {
		t.Fatal("the end of the wait was not saved")
	}
}
