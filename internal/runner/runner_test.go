// SPEC 0.5 V0.90: the runner keeps an agent process, its open question and
// its unhandled output across a restart of fyrd.
package runner_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/claude/fake"
	"github.com/meros/fyr/internal/runner"
)

func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_CLAUDE") == "1" {
		fake.Main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func serve(t *testing.T) *runner.Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "runner.sock")
	srv := &runner.Server{Logf: t.Logf}
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(time.Second) })
	return &runner.Client{Path: sock}
}

func next(t *testing.T, c *claude.Client) claude.Msg {
	t.Helper()
	select {
	case m, ok := <-c.Out:
		if !ok {
			t.Fatal("output closed")
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
	return nil
}

// SPEC 0.5 V0.90 (1), (3), (4): fyrd dies with a question open and output
// unread; the next fyrd attaches, gets the question again and the output,
// and the turn ends.
func TestProcessSurvivesRestart(t *testing.T) {
	rc := serve(t)
	c1, err := claude.Start(claude.Spec{Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"},
		Runner: rc, ID: "p1", Meta: []byte(`{"thread":"t1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	asked := make(chan struct{}, 1)
	c1.OnCanUseTool = func(claude.Msg) claude.Msg {
		asked <- struct{}{}
		select {} // the first fyrd never answers: it dies first
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c1.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// The first fyrd reads a whole turn but dies before it handles it.
	c1.SendUser([]claude.Msg{{"type": "text", "text": "hello"}}, "")
	for m := next(t, c1); m["type"] != "result"; m = next(t, c1) {
	}
	if err := c1.SendUser([]claude.Msg{{"type": "text", "text": "approve"}}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("no permission request")
	}
	// The first fyrd dies: its link closes, nothing was acked.
	c1.Pipe().(*runner.Conn).Close()

	procs, err := rc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 || procs[0].ID != "p1" || string(procs[0].Meta) != `{"thread":"t1"}` || procs[0].Exited {
		t.Fatalf("list after the restart: %+v", procs)
	}
	conn, err := rc.Attach("p1")
	if err != nil {
		t.Fatal(err)
	}
	answered := make(chan string, 1)
	c2 := claude.Open(conn, nil, func(c *claude.Client) {
		c.OnCanUseTool = func(req claude.Msg) claude.Msg {
			tool, _ := req["tool_name"].(string)
			answered <- tool
			return claude.Msg{"behavior": "allow", "updatedInput": req["input"]}
		}
	})
	var said string
	for results := 0; results < 2; {
		m := next(t, c2)
		c2.Handled(m)
		if m["type"] == "assistant" {
			said += fmt.Sprint(m["message"])
		}
		if m["type"] == "result" {
			results++
		}
	}
	select {
	case tool := <-answered:
		if tool != "Bash" {
			t.Fatalf("question asked again for %q", tool)
		}
	default:
		t.Fatal("the open question did not come back to the new fyrd")
	}
	if !strings.Contains(said, "Permission: allow") {
		t.Fatalf("the answer did not reach the CLI: %s", said)
	}
	if i, j := strings.Index(said, "echo: hello"), strings.Index(said, "Permission"); i < 0 || i > j {
		t.Fatalf("the unhandled turn did not come back first: %s", said)
	}

	// A third fyrd gets nothing that the second one handled.
	time.Sleep(100 * time.Millisecond) // the acks are written before the link ends
	conn.Close()
	conn3, err := rc.Attach("p1")
	if err != nil {
		t.Fatal(err)
	}
	c3 := claude.Open(conn3, nil, nil)
	select {
	case m := <-c3.Out:
		t.Fatalf("handled output came back: %v", m)
	case <-time.After(300 * time.Millisecond):
	}
	// (5): the exit reaches the link, and forget drops the process.
	c3.Close()
	select {
	case <-c3.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("no exit after close")
	}
	conn3.Forget()
	time.Sleep(50 * time.Millisecond)
	if procs, _ := rc.List(); len(procs) != 0 {
		t.Fatalf("forgotten process still listed: %+v", procs)
	}
}

// SPEC 0.5 V0.90 (5): a process that exits while no fyrd listens is listed
// as exited, and its exit reaches the next link.
func TestExitWhileDetached(t *testing.T) {
	rc := serve(t)
	c1, err := claude.Start(claude.Spec{Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}, Runner: rc, ID: "p2"})
	if err != nil {
		t.Fatal(err)
	}
	c1.Pipe().(*runner.Conn).Detach()
	conn, err := rc.Attach("p2")
	if err != nil {
		t.Fatal(err)
	}
	conn.CloseInput()
	conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ps, _ := rc.List()
		if len(ps) == 1 && ps[0].Exited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not exited: %+v", ps)
		}
		time.Sleep(20 * time.Millisecond)
	}
	conn, err = rc.Attach("p2")
	if err != nil {
		t.Fatal(err)
	}
	c := claude.Open(conn, nil, nil)
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the exit did not reach the new link")
	}
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timeout: " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func procs(t *testing.T, rc *runner.Client) []runner.Proc {
	t.Helper()
	ps, err := rc.List()
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

// SPEC 0.5 V0.90 (1): one live process per key. A second spawn names the
// first; once that one exited, the key is free.
func TestOneProcessPerKey(t *testing.T) {
	rc := serve(t)
	fake := []string{"FYR_FAKE_CLAUDE=1"}
	a, err := rc.Spawn(runner.Spawn{ID: "a", Key: "t1", Binary: os.Args[0], Env: fake})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rc.Spawn(runner.Spawn{ID: "b", Key: "t1", Binary: os.Args[0], Env: fake})
	var ex *runner.ExistsError
	if !errors.As(err, &ex) || ex.ID != "a" {
		t.Fatalf("second spawn for t1: %v", err)
	}
	if ps := procs(t, rc); len(ps) != 1 {
		t.Fatalf("a refused spawn left a process: %+v", ps)
	}
	if _, err := rc.Spawn(runner.Spawn{ID: "c", Key: "t2", Binary: os.Args[0], Env: fake}); err != nil {
		t.Fatalf("another key: %v", err)
	}
	a.CloseInput()
	until(t, "a exits", func() bool {
		for _, p := range procs(t, rc) {
			if p.ID == "a" {
				return p.Exited
			}
		}
		return false
	})
	if _, err := rc.Spawn(runner.Spawn{ID: "d", Key: "t1", Binary: os.Args[0], Env: fake}); err != nil {
		t.Fatalf("after the exit: %v", err)
	}
}

// SPEC 0.5 V0.90 (5): a process with no fyrd linked for the abandon time
// gets stdin closed, then a kill after the grace; an exited one that no
// fyrd collected is dropped. A linked process is never abandoned.
func TestAbandonedProcessEnds(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "r.sock")
	srv := &runner.Server{Abandon: 150 * time.Millisecond, Grace: 100 * time.Millisecond, Tick: 10 * time.Millisecond}
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(time.Second) })
	rc := &runner.Client{Path: sock}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on PATH")
	}
	// held keeps its link: it must outlive the abandon time.
	held, err := rc.Spawn(runner.Spawn{ID: "held", Key: "t0", Binary: sleep, Args: []string{"30"}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Kill()
	// deaf ignores stdin, so only the kill ends it.
	deaf, err := rc.Spawn(runner.Spawn{ID: "deaf", Key: "t1", Binary: sleep, Args: []string{"30"}})
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := rc.Spawn(runner.Spawn{ID: "quiet", Key: "t2", Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	deaf.Detach()
	quiet.Detach()
	until(t, "abandoned processes dropped", func() bool {
		ps := procs(t, rc)
		return len(ps) == 1 && ps[0].ID == "held"
	})
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("dropped after %s, before the abandon time", d)
	}
	if ps := procs(t, rc); ps[0].Exited || !ps[0].Linked {
		t.Fatalf("the linked process was touched: %+v", ps[0])
	}
}

// SPEC 0.5 V0.143: the runner says when a process's input is closed. A
// CLI with background work lives on after it, and a fyrd that adopts it
// must not take it for a process that reads what it writes.
func TestListSaysInputClosed(t *testing.T) {
	rc := serve(t)
	c, err := claude.Start(claude.Spec{Binary: os.Args[0], Env: []string{"FYR_FAKE_CLAUDE=1"}, Runner: rc, ID: "p142"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendUser([]claude.Msg{{"type": "text", "text": "bgwait 3000"}}, "u1"); err != nil {
		t.Fatal(err)
	}
	for m := next(t, c); m["type"] != "result"; m = next(t, c) {
	}
	if ps := procs(t, rc); len(ps) != 1 || ps[0].InputClosed {
		t.Fatalf("before the close: %+v", ps)
	}
	c.Close()
	until(t, "the list says the input is closed", func() bool {
		ps := procs(t, rc)
		return len(ps) == 1 && ps[0].InputClosed && !ps[0].Exited
	})
}
