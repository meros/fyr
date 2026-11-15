package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func changeOf(h *harness, id, ref string) *core.Change {
	th, _ := h.e.Thread(id)
	return core.FindChange(&th, ref)
}

// SPEC 0.5 V0.59 (5): the change tools open, report, revise, attach, ask and
// list; facts beat claims; settle_when change.
func TestChangeTools(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	call := func(tool string, a map[string]any) (string, bool) { return callAs(t, h.e, id, tool, a) }

	out, isErr := call("change_open", map[string]any{"key": "k8s:prod/api", "kind": "infra", "title": "Raise api replicas to 4", "targets": []any{"k8s:gke-prod/api"}})
	if isErr || !strings.Contains(out, "Opened change c_") {
		t.Fatalf("open: %s", out)
	}
	if out, _ := call("change_open", map[string]any{"key": "k8s:prod/api", "kind": "infra", "title": "again"}); !strings.Contains(out, "Found") {
		t.Fatalf("second open: %s", out)
	}
	if out, isErr := call("change_status", map[string]any{"change": "k8s:prod/api", "conditions": []any{map[string]any{"type": "approval", "status": "ok"}}}); !isErr || !strings.Contains(out, "change_request") {
		t.Fatalf("agent approved itself: %s", out)
	}
	call("change_status", map[string]any{"change": "k8s:prod/api", "phase": "landed", "label": "applied", "conditions": []any{map[string]any{"type": "health", "status": "fail", "reason": "2 of 4 pods crash"}}})
	c := changeOf(h, id, "k8s:prod/api")
	if c.Phase != core.PhaseLanded || c.PhaseBy != core.ByAgent || c.Label != "applied" {
		t.Fatalf("status: %+v", c)
	}
	if d := dtoOf(h.e, id); d.Attention.State != core.Blocked || !strings.Contains(d.Attention.Reason, "2 of 4 pods crash") {
		t.Fatalf("attention: %+v", d.Attention)
	}
	call("change_revise", map[string]any{"change": c.ID, "revision": map[string]any{"id": "rev2", "summary": "fix probe"}})
	if c := changeOf(h, id, c.ID); len(c.Revisions) != 1 || c.Conditions[0].Status != "pending" {
		t.Fatalf("revise: %+v", c)
	}
	call("change_attach", map[string]any{"change": c.ID, "evidence": []any{map[string]any{"kind": "log", "url": "https://logs/x"}}})
	if out, _ := call("change_list", map[string]any{}); !strings.Contains(out, "https://logs/x") || !strings.Contains(out, "Raise api replicas") {
		t.Fatalf("list: %s", out)
	}
	if out, isErr := call("change_request", map[string]any{"change": c.ID, "ask": "approve", "reason": "before I scale prod"}); isErr {
		t.Fatalf("request: %s", out)
	}
	th, _ := h.e.Thread(id)
	if len(th.Decisions) != 1 || th.Decisions[0].Effect.Action != "change.approve" || th.Decisions[0].Effect.Args["change"] != c.ID {
		t.Fatalf("decision: %+v", th.Decisions)
	}
	if out, isErr := call("settle_when", map[string]any{"condition": "change", "change": c.ID, "phase": "live"}); isErr {
		t.Fatalf("settle_when: %s", out)
	}
	if _, isErr := call("change_status", map[string]any{"change": "nope", "phase": "live"}); !isErr {
		t.Fatal("an unknown change was accepted")
	}
}

// SPEC 0.5 V0.60: a script action on a change runs only on the owner's press with the
// digest they saw, binds to its revision, and records its result; a failed
// one blocks the thread.
func TestChangeActions(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	call := func(tool string, a map[string]any) (string, bool) { return callAs(t, h.e, id, tool, a) }
	var mu sync.Mutex
	var ran [][]string
	exit := 0
	h.e.Scripts = func(_ context.Context, dir string, argv []string) (string, int, error) {
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, argv)
		if exit != 0 {
			return "boom", exit, context.Canceled
		}
		return "applied 3 resources", 0, nil
	}
	call("change_open", map[string]any{"key": "tf:prod", "kind": "infra", "title": "Prod network", "revision": map[string]any{"id": "sha256:p1"}})
	if out, isErr := call("offer_action", map[string]any{"on": "tf:prod", "id": "apply", "label": "Apply plan", "run": "script", "argv": []any{"terraform", "apply", "plan.out"}, "bind": true, "onOk": "landed", "primary": true}); isErr {
		t.Fatalf("declare: %s", out)
	}
	if _, isErr := call("offer_action", map[string]any{"on": "tf:prod", "id": "fyr.x", "label": "x", "run": "script", "argv": []any{"x"}}); !isErr {
		t.Fatal("a fyr. id was accepted")
	}
	c := changeOf(h, id, "tf:prod")
	a := findOffer(c.Actions, "apply")
	if a == nil || a.Revision != "sha256:p1" || a.Digest == "" {
		t.Fatalf("action: %+v", a)
	}
	press := func(cmd, digest string) Reply {
		return h.cmdID(cmd, "action.run", id, core.Args{"on": c.ID, "action": "apply", "digest": digest})
	}
	if r := press("r0", "stale"); r.OK || r.Status != 409 {
		t.Fatalf("a stale digest ran: %+v", r)
	}
	if r := press("r1", a.Digest); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "the script ended", func() bool {
		c := changeOf(h, id, "tf:prod")
		return len(c.Runs) == 1 && !c.Runs[0].Running
	})
	c = changeOf(h, id, "tf:prod")
	if !c.Runs[0].OK || c.Phase != core.PhaseLanded || !strings.Contains(c.Runs[0].Output, "applied 3") || strings.Join(ran[0], " ") != "terraform apply plan.out" {
		t.Fatalf("after run: %+v %v", c, ran)
	}
	// A new revision makes the bound action unavailable.
	call("change_revise", map[string]any{"change": c.ID, "revision": map[string]any{"id": "sha256:p2"}})
	if r := press("r2", a.Digest); r.OK || !strings.Contains(r.Error, "newer revision") {
		t.Fatalf("ran on a newer revision: %+v", r)
	}
	// A failed script blocks the thread.
	call("offer_action", map[string]any{"on": c.ID, "id": "rollback", "label": "Roll back", "run": "script", "argv": []any{"bash", "-c", "exit 3"}})
	exit = 3
	rb := findOffer(changeOf(h, id, c.ID).Actions, "rollback")
	if r := h.cmdID("r3", "action.run", id, core.Args{"on": c.ID, "action": "rollback", "digest": rb.Digest}); !r.OK {
		t.Fatalf("rollback: %+v", r)
	}
	eventually(t, "the rollback ended", func() bool {
		d := dtoOf(h.e, id)
		return d.Attention.State == core.Blocked
	})
	if d := dtoOf(h.e, id); !strings.Contains(d.Attention.Reason, "Roll back failed (exit 3)") {
		t.Fatalf("blocked reason: %q", d.Attention.Reason)
	}
}

// SPEC 0.5 V0.60: a PR change gets Fyr's own actions from what was observed.
func TestBuiltinActions(t *testing.T) {
	th := &core.Thread{}
	now := time.Now()
	core.ChangePRObserved(th, core.PR{URL: "https://github.com/o/r/pull/5", Repo: "o/r", Number: 5, State: "OPEN", Checks: "fail", Review: "CHANGES_REQUESTED"}, now)
	c := &th.Changes[0]
	ids := map[string]bool{}
	for _, a := range c.Actions {
		ids[a.ID] = true
	}
	for _, want := range []string{"fyr.rerun", "fyr.fixci", "fyr.review", "fyr.merge"} {
		if !ids[want] {
			t.Fatalf("missing %s in %+v", want, c.Actions)
		}
	}
	if fix := findOffer(c.Actions, "fyr.fixci"); fix.Run != core.RunAgent || !fix.Primary {
		t.Fatalf("Fix CI: %+v", fix)
	}
	core.ChangePRObserved(th, core.PR{URL: "https://github.com/o/r/pull/5", Repo: "o/r", Number: 5, State: "MERGED"}, now)
	if len(th.Changes[0].Actions) != 0 {
		t.Fatalf("a merged PR kept actions: %+v", th.Changes[0].Actions)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func findOffer(list []core.Offer, id string) *core.Offer {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

// SPEC 0.5 V0.60: the same action model on the thread and its live view; a
// view's actions need a view and go away with it; change-only fields are
// refused elsewhere.
func TestThreadAndViewActions(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	call := func(tool string, a map[string]any) (string, bool) { return callAs(t, h.e, id, tool, a) }
	h.e.Scripts = func(_ context.Context, dir string, argv []string) (string, int, error) { return "42 passed", 0, nil }
	if out, isErr := call("offer_action", map[string]any{"on": "thread", "id": "e2e", "label": "Run e2e", "run": "script", "argv": []any{"make", "e2e"}}); isErr {
		t.Fatalf("thread action: %s", out)
	}
	if _, isErr := call("offer_action", map[string]any{"on": "thread", "id": "x", "label": "X", "run": "script", "argv": []any{"x"}, "onOk": "landed"}); !isErr {
		t.Fatal("onOk was accepted on the thread")
	}
	if _, isErr := call("offer_action", map[string]any{"on": "view", "id": "v", "label": "V", "run": "agent", "prompt": "p"}); !isErr {
		t.Fatal("a view action was accepted without a view")
	}
	th, _ := h.e.Thread(id)
	e2e := findOffer(th.Offers, "e2e")
	if r := h.cmdID("t1", "action.run", id, core.Args{"on": "thread", "action": "e2e", "digest": e2e.Digest}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	eventually(t, "the thread script ended", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.OfferRuns) == 1 && !th.OfferRuns[0].Running && th.OfferRuns[0].OK
	})
	// A view action, removed with the view.
	if r := h.cmd("thread.setView", id, core.Args{"on": true}); !r.OK {
		t.Fatalf("view on: %+v", r)
	}
	if out, isErr := call("offer_action", map[string]any{"on": "view", "id": "approve", "label": "Looks good", "run": "agent", "prompt": "The owner approves the layout; continue."}); isErr {
		t.Fatalf("view action: %s", out)
	}
	h.cmd("thread.setView", id, core.Args{"on": false})
	th, _ = h.e.Thread(id)
	if findOffer(th.Offers, "approve") != nil || findOffer(th.Offers, "e2e") == nil {
		t.Fatalf("after the view closed: %+v", th.Offers)
	}
}

// SPEC 0.5 V0.62: change tools and change_unchanged record the upkeep.
func TestChangesUpkeep(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	call := func(tool string, a map[string]any) (string, bool) { return callAs(t, h.e, id, tool, a) }
	if out, _ := call("change_unchanged", map[string]any{"reason": "nothing open"}); !strings.Contains(out, "no open changes") {
		t.Fatalf("no changes: %s", out)
	}
	call("change_open", map[string]any{"key": "doc:x", "kind": "doc", "title": "Doc"})
	th, _ := h.e.Thread(id)
	if th.ChangesCheckedAt == nil {
		t.Fatal("change_open did not record the upkeep")
	}
	first := *th.ChangesCheckedAt
	time.Sleep(2 * time.Millisecond)
	if out, _ := call("change_unchanged", map[string]any{"reason": "waiting for review"}); !strings.Contains(out, "1 open changes") {
		t.Fatalf("unchanged: %s", out)
	}
	if th, _ := h.e.Thread(id); !th.ChangesCheckedAt.After(first) {
		t.Fatal("change_unchanged did not move the upkeep time")
	}
}

// SPEC 0.5 V0.60 (3): two presses at the same time (two devices) run a
// script once; the second gets "It is running".
func TestConcurrentPressRunsOnce(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	release := make(chan struct{})
	var mu sync.Mutex
	runs := 0
	h.e.Scripts = func(context.Context, string, []string) (string, int, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		<-release
		return "", 0, nil
	}
	if out, isErr := callAs(t, h.e, id, "offer_action", map[string]any{"on": "thread", "id": "e2e", "label": "E2E", "run": "script", "argv": []any{"make", "e2e"}}); isErr {
		t.Fatal(out)
	}
	th, _ := h.e.Thread(id)
	a := findOffer(th.Offers, "e2e")
	var wg sync.WaitGroup
	oks := make(chan bool, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oks <- h.cmdID(fmt.Sprintf("p%d", i), "action.run", id, core.Args{"on": "thread", "action": "e2e", "digest": a.Digest}).OK
		}()
	}
	wg.Wait()
	close(oks)
	n := 0
	for ok := range oks {
		if ok {
			n++
		}
	}
	close(release)
	if n != 1 {
		t.Fatalf("%d of 8 concurrent presses ran", n)
	}
	eventually(t, "the script ended", func() bool {
		th, _ := h.e.Thread(id)
		return len(th.OfferRuns) == 1 && !th.OfferRuns[0].Running
	})
}
