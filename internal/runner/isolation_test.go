// #167: one install's runner serves only the fyrd its launcher started. A
// client from anywhere else (an agent's test fyrd) is refused, and the
// processes and their open questions go on.
package runner_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/runner"
)

// serveAllow is serve with the launcher's rule: allow says whether the
// connecting process is the launcher's fyrd.
func serveAllow(t *testing.T, allow *atomic.Bool, tune func(*runner.Server)) (*runner.Server, *runner.Client) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "runner.sock")
	srv := &runner.Server{Logf: t.Logf, Allow: func(pid int) bool { return allow.Load() }}
	if tune != nil {
		tune(srv)
	}
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(time.Second) })
	return srv, &runner.Client{Path: sock}
}

func TestPeerPidIsTheListener(t *testing.T) {
	var allow atomic.Bool
	allow.Store(true)
	_, rc := serveAllow(t, &allow, nil)
	pid, err := runner.PeerPid(rc.Path)
	if err != nil {
		t.Skip("no peer pid here:", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("PeerPid = %d, the listener is %d", pid, os.Getpid())
	}
}

// A question pending across a stray client, the runner's rules and an
// update (decision 0023 (4), V0.90 (4) and (2)): a fyrd from another
// install can neither list, attach to nor end the process; a linked
// process is never abandoned however long its question waits; the next
// fyrd gets the question again and its answer reaches the CLI.
func TestQuestionPendingAcrossStrayAndUpdate(t *testing.T) {
	var allow atomic.Bool
	allow.Store(true)
	_, rc := serveAllow(t, &allow, func(s *runner.Server) {
		s.Tick, s.Abandon, s.Grace = 10*time.Millisecond, 50*time.Millisecond, 10*time.Millisecond
	})
	c1, err := claude.Start(claude.Spec{Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"},
		Runner: rc, ID: "p1", Key: "t1", Meta: []byte(`{"thread":"t1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	asked := make(chan struct{}, 1)
	c1.OnCanUseTool = func(claude.Msg) claude.Msg {
		asked <- struct{}{}
		select {} // the owner has not answered yet
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c1.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := c1.SendUser([]claude.Msg{{"type": "text", "text": "approve"}}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("no permission request")
	}

	// A stray fyrd (an agent's test, #167) does what Adopt does with a
	// process whose thread it does not know: attach, kill, forget.
	allow.Store(false)
	if ps, err := rc.List(); err == nil {
		t.Errorf("a stray client listed the processes: %+v", ps)
	}
	if conn, err := rc.Attach("p1"); err == nil {
		conn.Kill()
		conn.Forget()
		t.Error("a stray client attached to the process")
	}
	if _, err := rc.Spawn(runner.Spawn{ID: "x", Key: "t1", Binary: "/bin/true"}); err == nil {
		t.Error("a stray client spawned a process")
	}
	// Many janitor ticks past Abandon: the process stays linked, so it lives.
	select {
	case <-c1.Done():
		t.Fatal("the live fyrd lost its process while its question was pending")
	case <-time.After(300 * time.Millisecond):
	}

	// An update: the live fyrd detaches, the next one attaches.
	allow.Store(true)
	c1.Pipe().(*runner.Conn).Detach()
	procs, err := rc.List()
	if err != nil || len(procs) != 1 || procs[0].Exited {
		t.Fatalf("after the update: %+v %v", procs, err)
	}
	conn, err := rc.Attach("p1")
	if err != nil {
		t.Fatal(err)
	}
	answered := make(chan struct{}, 1)
	c2 := claude.Open(conn, nil, func(c *claude.Client) {
		c.OnCanUseTool = func(req claude.Msg) claude.Msg {
			answered <- struct{}{}
			return claude.Msg{"behavior": "allow", "updatedInput": req["input"]}
		}
	})
	var said string
	for {
		m := next(t, c2)
		c2.Handled(m)
		if m["type"] == "assistant" {
			said += fmt.Sprint(m["message"])
		}
		if m["type"] == "result" {
			break
		}
	}
	select {
	case <-answered:
	default:
		t.Fatal("the pending question did not come back after the update")
	}
	if !strings.Contains(said, "Permission: allow") {
		t.Fatalf("the answer did not reach the CLI: %s", said)
	}
}

// #167: an agent process never sees the launcher's variables, whether it
// runs in the runner or as fyrd's child.
func TestAgentProcessGetsNoLauncherVars(t *testing.T) {
	t.Setenv("FYR_RUNNER", "/live/runner.sock")
	t.Setenv("FYR_LAUNCHER", "1")
	t.Setenv("FYR_READY_FILE", "/live/ready")
	t.Setenv("FYR_LAUNCHER_PID", "1")
	dir := t.TempDir()
	script := filepath.Join(dir, "agent")
	os.WriteFile(script, []byte("#!/bin/sh\nenv > \"$OUT\"\n"), 0o755)
	var allow atomic.Bool
	allow.Store(true)
	_, rc := serveAllow(t, &allow, nil)
	for _, via := range []string{"runner", "child"} {
		out := filepath.Join(dir, via+".env")
		sp := claude.Spec{Binary: script, Env: []string{"OUT=" + out}, ID: via}
		if via == "runner" {
			sp.Runner = rc
		}
		c, err := claude.Start(sp)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-c.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the agent did not end")
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"FYR_RUNNER=", "FYR_LAUNCHER=", "FYR_READY_FILE=", "FYR_LAUNCHER_PID="} {
			if strings.Contains("\n"+string(b), "\n"+n) {
				t.Errorf("%s: the agent process has %s", via, n)
			}
		}
	}
}
