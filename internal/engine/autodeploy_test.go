package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

const goodCommit = "c0ffee1234567890c0ffee1234567890c0ffee12"

// gate is a fake V0.111 gate: /src/fyr passes, any other dir is held. It
// counts its calls.
type gate struct {
	mu sync.Mutex
	n  int
}

func (g *gate) check(_ context.Context, dir string) (string, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	if dir == "/src/fyr" {
		return goodCommit, ""
	}
	return "", "the checkout has changes that are not committed"
}

func (g *gate) calls() int { g.mu.Lock(); defer g.mu.Unlock(); return g.n }

// onFyr makes the thread work in a Fyr checkout: only such a thread may ask to
// deploy Fyr (V0.one-tool-vocabulary).
func onFyr(t *testing.T, h *harness, id string) {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"cmd/fyrd/main.go", "docs/changes/README.md"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	coretest.Patch(h.e, id, func(th *core.Thread, _ time.Time) []core.Event {
		th.Cwd = dir
		return nil
	})
}

func askDeployTool(t *testing.T, h *harness, id, dir string) {
	t.Helper()
	onFyr(t, h, id)
	if out, isErr := callAs(t, h.e, id, "change_request", map[string]any{"ask": "fyr.deploy", "args": map[string]any{"dir": dir}, "reason": "ship"}); isErr {
		t.Fatalf("change_request fyr.deploy: %s", out)
	}
}

func deployCalls(h *harness) []core.EffectReq {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	var out []core.EffectReq
	for _, c := range h.r.calls {
		if c.Action == core.FyrDeploy {
			out = append(out, c)
		}
	}
	return out
}

// SPEC V0.111 (1)-(3), (6): with the setting on by default, an agent's deploy
// ask of a clean checkout on origin's main runs with no press, bound to the
// commit; a held ask waits for the owner with a line that says why; the
// owner turns the setting off, and it outlives a restart.
func TestAutoDeployByTheSetting(t *testing.T) {
	h := newHarness(t, time.Minute)
	g := &gate{}
	h.e.DeployGate = g.check
	guardDeps(t, h.e)
	if !h.e.AutoDeploy().On {
		t.Fatal("the setting is not on by default")
	}
	id := h.create("x")
	h.wait(id, "done", state(core.Done))

	askDeployTool(t, h, id, "/src/fyr")
	eventually(t, "the auto-approved deploy ran", func() bool { return len(deployCalls(h)) == 1 })
	call := deployCalls(h)[0]
	if call.Args["commit"] != goodCommit || call.Args["autoDeploy"] != true || call.Args["dir"] != "/src/fyr" {
		t.Fatalf("ran %+v", call)
	}
	eventually(t, "the thread says it", func() bool {
		return hasEvent(h.events(id), "effect.result", "Auto-approved by the auto-deploy setting (c0ffee1).")
	})
	if !hasEvent(h.events(id), "decision.closed", "Auto-approved: Fyr deploys c0ffee1") {
		t.Fatal("no auto-approved answer in the thread")
	}

	// A dirty checkout: the ask waits for the owner, with the reason, and
	// the gate runs once for it.
	askDeployTool(t, h, id, "/src/dirty")
	eventually(t, "the hold is shown", func() bool {
		return hasEvent(h.events(id), "effect.error", "Not auto-approved: the checkout has changes")
	})
	before := g.calls()
	h.e.AutoDeploySweep()
	time.Sleep(100 * time.Millisecond)
	if th, _ := h.e.Thread(id); len(th.Decisions) != 1 || len(deployCalls(h)) != 1 || g.calls() != before {
		t.Fatalf("the held ask: %d decisions, %d deploys, gate %d→%d", len(th.Decisions), len(deployCalls(h)), before, g.calls())
	}

	// The owner turns it off: a new ask waits with no gate and no line.
	if r := h.cmd(core.KindAutoDeploySet, "", core.Args{"on": false}); !r.OK {
		t.Fatalf("off: %+v", r)
	}
	askDeployTool(t, h, id, "/src/fyr")
	time.Sleep(100 * time.Millisecond)
	if th, _ := h.e.Thread(id); len(th.Decisions) != 2 || len(deployCalls(h)) != 1 {
		t.Fatalf("deployed with the setting off: %d decisions, %d deploys", len(th.Decisions), len(deployCalls(h)))
	}
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	if h2.e.AutoDeploy().On {
		t.Fatal("the setting did not outlive a restart")
	}
	// On again: the waiting clean ask runs at once.
	h2.e.DeployGate = g.check
	guardDeps(t, h2.e)
	if r := h2.cmd(core.KindAutoDeploySet, "", core.Args{"on": true}); !r.OK {
		t.Fatalf("on: %+v", r)
	}
	eventually(t, "the waiting ask ran", func() bool { return len(deployCalls(h2)) == 1 })
}

// SPEC V0.111 (4): one deploy at a time. While one runs, a second ask gets
// the wait line once and runs when the first ends.
func TestAutoDeployOneAtATime(t *testing.T) {
	h := newHarness(t, time.Minute)
	g := &gate{}
	h.e.DeployGate = g.check
	guardDeps(t, h.e)
	h.r.deploy = make(chan error)
	a := h.create("a")
	b := h.create("b")
	h.wait(a, "done", state(core.Done))
	h.wait(b, "done", state(core.Done))

	askDeployTool(t, h, a, "/src/fyr")
	eventually(t, "the first deploy runs", func() bool { return len(deployCalls(h)) == 1 })
	askDeployTool(t, h, b, "/src/fyr")
	eventually(t, "the second ask waits", func() bool { return hasEvent(h.events(b), "effect.error", core.AutoDeployWait) })
	h.e.AutoDeploySweep()
	time.Sleep(100 * time.Millisecond)
	if n := len(deployCalls(h)); n != 1 {
		t.Fatalf("%d deploys at once", n)
	}
	n := 0
	for _, ev := range h.events(b) {
		if ev.Type == "effect.error" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the wait line shows %d times", n)
	}
	// A busy deployer (a build in its probation) holds it too.
	busy := true
	var mu sync.Mutex
	h.e.DeployBusy = func() bool { mu.Lock(); defer mu.Unlock(); return busy }
	guardDeps(t, h.e)
	h.r.deploy <- errors.New("tests failed, nothing was switched")
	eventually(t, "the first result landed", func() bool { return hasEvent(h.events(a), "effect.result", "tests failed") })
	time.Sleep(100 * time.Millisecond)
	if n := len(deployCalls(h)); n != 1 {
		t.Fatalf("deployed while the deployer is busy: %d", n)
	}
	mu.Lock()
	busy = false
	mu.Unlock()
	h.e.AutoDeploySweep() // the deployer's OnIdle
	eventually(t, "the second deploy runs", func() bool { return len(deployCalls(h)) == 2 })
	close(h.r.deploy)
	eventually(t, "the second result landed", func() bool { return hasEvent(h.events(b), "effect.result", "Auto-approved by the auto-deploy setting") })
}

// V0.one-tool-vocabulary: a thread that is not on Fyr's own repo cannot ask
// to deploy Fyr; the refusal names run_request.
func TestFyrDeployAskOnlyOnFyrsRepo(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	out, isErr := callAs(t, h.e, id, "change_request", map[string]any{"ask": "fyr.deploy", "args": map[string]any{}})
	if !isErr || out != core.NotFyrRepoText {
		t.Fatalf("%v %q", isErr, out)
	}
	if th, _ := h.e.Thread(id); len(th.Decisions) != 0 {
		t.Fatalf("an ask opened: %+v", th.Decisions)
	}
}
