package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// SPEC V0.149: run_request shows a Run card; the press runs the command
// on this host, streams its output, Stop stops it, and root goes through
// pkexec.
func TestRunRequestTool(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	call := func(a map[string]any) (string, bool) { return callAs(t, h.e, id, "run_request", a) }

	if out, isErr := call(map[string]any{"command": "sudo udisksctl mount -b /dev/sdc1", "why": "Mount the SD card"}); !isErr || !strings.Contains(out, "root: true") {
		t.Fatalf("sudo accepted: %s", out)
	}
	out, isErr := call(map[string]any{"command": "udisksctl mount -b /dev/sdc1", "why": "Mount the SD card", "root": true})
	if isErr || !strings.Contains(out, "Run card") {
		t.Fatalf("request: %s", out)
	}
	th, _ := h.e.Thread(id)
	o := findOffer(th.Offers, "run-1")
	if o == nil || !o.Request || !o.Root {
		t.Fatalf("offer: %+v", th.Offers)
	}
	if d := dtoOf(h.e, id); d.Attention.Section != core.NeedsYou || d.Attention.Reason != "Run: Mount the SD card" {
		t.Fatalf("attention: %+v", d.Attention)
	}

	var mu sync.Mutex
	var argv []string
	h.e.LiveScripts = func(ctx context.Context, dir string, a []string, out func(string)) (string, int, error) {
		mu.Lock()
		argv = a
		mu.Unlock()
		out("mounted /dev/sdc1 at /run/media/o/SD\n")
		<-ctx.Done() // runs until stopped
		return "mounted /dev/sdc1 at /run/media/o/SD\n", -1, ctx.Err()
	}
	if r := h.cmdID("p1", "action.run", id, core.Args{"on": "thread", "action": "run-1", "digest": o.Digest}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "the output streams into the run", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.Runs) == 1 && th.Runs[0].Running && strings.Contains(th.Runs[0].Output, "mounted")
	})
	mu.Lock()
	got := strings.Join(argv, " ")
	mu.Unlock()
	if !strings.HasPrefix(got, "pkexec ") || !strings.HasSuffix(got, "bash -euo pipefail -c udisksctl mount -b /dev/sdc1") || !strings.Contains(got, "--chdir=") {
		t.Fatalf("root argv: %q", got)
	}
	if r := h.cmdID("s1", "action.stop", id, core.Args{"on": "thread", "action": "run-1"}); !r.OK {
		t.Fatalf("stop: %+v", r)
	}
	eventually(t, "the run stopped", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})
	th, _ = h.e.Thread(id)
	if r := th.Runs[0]; r.OK || !strings.Contains(r.Output, core.StoppedText) {
		t.Fatalf("stopped run: %+v", r)
	}
	if d := dtoOf(h.e, id); d.Attention.Reason == "Run: Mount the SD card" {
		t.Fatalf("a request that ran is still Needs you: %+v", d.Attention)
	}
	if r := h.cmdID("d1", "action.dismiss", id, core.Args{"on": "thread", "action": "run-1"}); !r.OK {
		t.Fatalf("close: %+v", r)
	}
	if th, _ := h.e.Thread(id); len(th.Offers) != 0 {
		t.Fatalf("close kept it: %+v", th.Offers)
	}
}

// V0.149 (4): the thread says why a root request cannot ask for a
// password on this host.
func TestRootPromptOnTheThread(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	h.e.RootPrompt = func() string { return "no polkit agent runs here" }
	if d := dtoOf(h.e, id); d.RootPrompt != "" {
		t.Fatalf("a thread without a root request: %q", d.RootPrompt)
	}
	callAs(t, h.e, id, "run_request", map[string]any{"command": "nixos-rebuild switch", "why": "Apply", "root": true})
	if d := dtoOf(h.e, id); d.RootPrompt != "no polkit agent runs here" {
		t.Fatalf("rootPrompt: %q", d.RootPrompt)
	}
}

func TestPolkitAgentFromProcesses(t *testing.T) {
	cases := []struct {
		names  []string
		pkexec bool
		want   string
	}{
		{[]string{"polkit-gnome-authentication-agent-1"}, true, ""},
		{[]string{"polkit-kde-authentication-agent-1", "bash"}, true, ""},
		{[]string{"hyprpolkitagent"}, true, ""},
		{[]string{"swaypplet polkit-agent"}, true, ""}, // the owner's desktop: the role is an argument
		{[]string{"lxpolkit"}, true, ""},
		{[]string{"polkitd --no-debug", "polkit-agent-helper-1 o", "firefox"}, true, "no polkit authentication agent runs in your session"},
		{[]string{"polkit-gnome-authentication-agent-1"}, false, "pkexec is not installed"},
	}
	for _, c := range cases {
		if got := rootPromptFrom(c.names, c.pkexec); got != c.want {
			t.Errorf("%v %v: got %q, want %q", c.names, c.pkexec, got, c.want)
		}
	}
}

func TestRootArgv(t *testing.T) {
	got := strings.Join(rootArgv("/srv/x", []string{"bash", "-c", "ls"}, "/usr/bin/env", "/bin:/usr/bin"), " ")
	if got != "pkexec /usr/bin/env --chdir=/srv/x PATH=/bin:/usr/bin bash -c ls" {
		t.Fatalf("argv: %q", got)
	}
}

func TestLiveScriptStreams(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	out, exit, err := runScriptLive(context.Background(), t.TempDir(), []string{"bash", "-c", "echo one; echo two >&2; exit 3"}, func(s string) {
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	})
	if exit != 3 || err == nil || !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("out %q exit %d err %v", out, exit, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || !strings.Contains(seen[len(seen)-1], "two") {
		t.Fatalf("streamed: %q", seen)
	}
}

// V0.run-long-notice: a run request that still runs after LongRunAfter
// tells the agent it started; one that ends before says only its result.
func TestRunRequestLongRunTellsStart(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	callAs(t, h.e, id, "run_request", map[string]any{"command": "make test", "why": "Run the tests"})
	callAs(t, h.e, id, "run_request", map[string]any{"command": "ls", "why": "List it", "alongside": true})
	th, _ := h.e.Thread(id)
	long, short := findOffer(th.Offers, "run-1"), findOffer(th.Offers, "run-2")

	release := make(chan struct{})
	h.e.LongRunAfter = 10 * time.Millisecond
	h.e.LiveScripts = func(ctx context.Context, dir string, a []string, out func(string)) (string, int, error) {
		<-release
		return "ok\n", 0, nil
	}
	if r := h.cmdID("p1", "action.run", id, core.Args{"on": "thread", "action": "run-1", "digest": long.Digest}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "the long run tells that it started", func() bool { return hasEvent(h.events(id), core.EventActionLong, `"run-1"`) })
	close(release)
	eventually(t, "the long run ended", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})

	h.e.LongRunAfter = time.Hour // the short run ends long before
	h.e.LiveScripts = func(ctx context.Context, dir string, a []string, out func(string)) (string, int, error) {
		return "", 0, nil
	}
	if r := h.cmdID("p2", "action.run", id, core.Args{"on": "thread", "action": "run-2", "digest": short.Digest}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "the short run ended", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.Runs) == 2 && !th.Runs[1].Running
	})
	if hasEvent(h.events(id), core.EventActionLong, `"run-2"`) {
		t.Fatal("a short run told that it started")
	}
}

// V0.run-card-rerun: action.run and action.dismiss sent through CommandAs
// record the acting device and front on their stored action events.
func TestActionEventsRecordDeviceThroughEngine(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	callAs(t, h.e, id, "run_request", map[string]any{"command": "echo hi", "why": "Say hi"})
	th, _ := h.e.Thread(id)
	o := findOffer(th.Offers, "run-1")
	h.e.LiveScripts = func(ctx context.Context, dir string, a []string, out func(string)) (string, int, error) {
		return "hi\n", 0, nil
	}
	r := h.e.CommandAs(api.Cmd{
		CommandID: "p-actor",
		Action:    "action.run",
		ThreadID:  id,
		Args:      core.Args{"on": "thread", "action": "run-1", "digest": o.Digest},
	}, "device:dev_abc", "FRNT-0000-0000-0000")
	if !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "run ended", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})
	if !hasEvent(h.events(id), "action", `"actor":"device:dev_abc"`) || !hasEvent(h.events(id), "action", `"front":"FRNT-0000-0000-0000"`) {
		t.Fatalf("stored action press event missing actor/front: %+v", h.events(id))
	}
	r = h.e.CommandAs(api.Cmd{
		CommandID: "d-actor",
		Action:    "action.dismiss",
		ThreadID:  id,
		Args:      core.Args{"on": "thread", "action": "run-1"},
	}, "device:dev_xyz", "FRNT-1111-1111-1111")
	if !r.OK {
		t.Fatalf("dismiss: %+v", r)
	}
	if !hasEvent(h.events(id), "action", `"actor":"device:dev_xyz"`) || !hasEvent(h.events(id), "action", `"front":"FRNT-1111-1111-1111"`) {
		t.Fatalf("stored action dismiss event missing actor/front: %+v", h.events(id))
	}
}

// V0.run-card-replaces: a new card replaces the agent's earlier one that
// never ran, and the tool's reply names it.
func TestRunRequestToolSaysWhatItReplaced(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	callAs(t, h.e, id, "run_request", map[string]any{"command": "old", "why": "Create the rule (old script)"})
	out, isErr := callAs(t, h.e, id, "run_request", map[string]any{"command": "new", "why": "Create the rule (fixed script)"})
	if isErr || !strings.Contains(out, `replaced your earlier cards that the owner had not run ("Create the rule (old script)")`) {
		t.Fatalf("reply: %s", out)
	}
	th, _ := h.e.Thread(id)
	if len(th.Offers) != 1 || th.Offers[0].Why != "Create the rule (fixed script)" {
		t.Fatalf("offers: %+v", th.Offers)
	}
	if !hasEvent(h.events(id), "action", `"replaced":true`) {
		t.Fatal("no replaced event in the log")
	}
}

// SPEC V0.run-card-keeps-run (3), I7: a press sent again with the same
// commandId (a retry, a resend) runs its card once, also after the run ended
// and the double-press window has passed. The press event records the id.
func TestRunRequestPressOncePerCommandID(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	if out, isErr := callAs(t, h.e, id, "run_request", map[string]any{"command": "false", "why": "Check it"}); isErr {
		t.Fatalf("request: %s", out)
	}
	var mu sync.Mutex
	runs := 0
	h.e.LiveScripts = func(ctx context.Context, dir string, a []string, out func(string)) (string, int, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		return "no\n", 1, nil
	}
	th, _ := h.e.Thread(id)
	o := findOffer(th.Offers, "run-1")
	args := core.Args{"on": "thread", "action": "run-1", "digest": o.Digest}
	if r := h.cmdID("press-1", "action.run", id, args); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "the run ended", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})
	h.e.Now = func() time.Time { return time.Now().Add(time.Minute) }
	for range 3 {
		if r := h.cmdID("press-1", "action.run", id, args); !r.OK {
			t.Fatalf("the repeat did not answer the first reply: %+v", r)
		}
	}
	time.Sleep(50 * time.Millisecond) // a second run, if one started, would have begun by now
	mu.Lock()
	n := runs
	mu.Unlock()
	if th, _ := h.e.Thread(id); n != 1 || len(th.Runs) != 1 {
		t.Fatalf("one commandId ran %d times; runs %+v", n, th.Runs)
	}
	saw := false
	for _, ev := range h.events(id) {
		if ev.Type == "action" && strings.Contains(string(ev.Data), `"commandId":"press-1"`) {
			saw = true
		}
	}
	if !saw {
		t.Fatal("the press event has no commandId")
	}
}
