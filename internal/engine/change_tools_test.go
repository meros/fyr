package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
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
	idle(h) // V0.101: a failure is the owner's 5 minutes after the run
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
	call("change_status", map[string]any{"change": "k8s:prod/api", "phase": "review", "label": "applied to staging", "conditions": []any{map[string]any{"type": "health", "status": "fail", "reason": "2 of 4 pods crash"}}})
	c := changeOf(h, id, "k8s:prod/api")
	if c.Phase != core.PhaseReview || c.PhaseBy != core.ByAgent || c.Label != "applied to staging" {
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
	if out, _ := call("change_list", map[string]any{"id": c.ID}); !strings.Contains(out, "https://logs/x") || !strings.Contains(out, "Raise api replicas") {
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
	idle(h)
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
	if out, isErr := call("offer_action", map[string]any{"on": "tf:prod", "id": "apply", "label": "Apply plan", "run": "script", "argv": []any{"terraform", "apply", "plan.out"}, "bind": true, "onOk": "review", "primary": true}); isErr {
		t.Fatalf("declare: %s", out)
	}
	if _, isErr := call("offer_action", map[string]any{"on": "tf:prod", "id": "fyr.x", "label": "x", "run": "script", "argv": []any{"x"}}); !isErr {
		t.Fatal("a fyr. id was accepted")
	}
	c := changeOf(h, id, "tf:prod")
	a := findOffer(c.Offers, "apply")
	if a == nil || a.Revision != "sha256:p1" || a.Digest == "" {
		t.Fatalf("action: %+v", a)
	}
	press := func(cmd, digest string) api.Reply {
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
	if !c.Runs[0].OK || c.Phase != core.PhaseReview || !strings.Contains(c.Runs[0].Output, "applied 3") || strings.Join(ran[0], " ") != "terraform apply plan.out" {
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
	rb := findOffer(changeOf(h, id, c.ID).Offers, "rollback")
	if r := h.cmdID("r3", "action.run", id, core.Args{"on": c.ID, "action": "rollback", "digest": rb.Digest}); !r.OK {
		t.Fatalf("rollback: %+v", r)
	}
	eventually(t, "the rollback ended", func() bool {
		th, _ := h.e.Thread(id)
		c := core.FindChange(&th, c.ID)
		return !th.Running && c != nil && c.Blocking() != ""
	})
	// V0.101: the failure wakes the agent; it is the owner's 5 minutes
	// after that run, when the clock re-reduces the thread.
	h.e.Now = func() time.Time { return time.Now().Add(30 * time.Minute) }
	// Judge one snapshot: a second read can land after the woken agent
	// has moved the thread on (Working, empty reason).
	var d api.Thread
	eventually(t, "stopped on the failure", func() bool {
		h.e.Tick()
		d = dtoOf(h.e, id)
		return d.Attention.State == core.Blocked
	})
	if !strings.Contains(d.Attention.Reason, "Roll back failed (exit 3)") {
		t.Fatalf("blocked reason: %q", d.Attention.Reason)
	}
}

// SPEC 0.5 V0.60: a PR change gets Fyr's own actions from what was observed.
func TestBuiltinActions(t *testing.T) {
	th := &core.Thread{}
	now := time.Now()
	core.ChangePRObserved(th, core.PR{URL: "https://github.com/o/r/pull/5", Repo: "o/r", Number: 5, State: "OPEN", Checks: "fail", Review: "CHANGES_REQUESTED"}, now)
	core.EvalRules(core.Rec{T: th}, now) // #46: the built-in actions are a Rule
	c := &th.Changes[0]
	ids := map[string]bool{}
	for _, a := range c.Offers {
		ids[a.ID] = true
	}
	for _, want := range []string{"fyr.rerun", "fyr.fixci", "fyr.review", "fyr.merge"} {
		if !ids[want] {
			t.Fatalf("missing %s in %+v", want, c.Offers)
		}
	}
	if fix := findOffer(c.Offers, "fyr.fixci"); fix.Run != core.RunAgent || !fix.Primary {
		t.Fatalf("Fix CI: %+v", fix)
	}
	core.ChangePRObserved(th, core.PR{URL: "https://github.com/o/r/pull/5", Repo: "o/r", Number: 5, State: "MERGED"}, now)
	core.EvalRules(core.Rec{T: th}, now)
	if len(th.Changes[0].Offers) != 0 {
		t.Fatalf("a merged PR kept actions: %+v", th.Changes[0].Offers)
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

func findOffer(list []core.ActionSpec, id string) *core.ActionSpec {
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
		return len(th.Runs) == 1 && !th.Runs[0].Running && th.Runs[0].OK
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
	var view []core.ActionSpec
	if th.Live != nil {
		view = th.Live.Offers
	}
	if findOffer(view, "approve") != nil || findOffer(th.Offers, "e2e") == nil {
		t.Fatalf("after the view closed: %+v %+v", view, th.Offers)
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
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})
}

// SPEC 0.5 V0.12, review 2026-11-15: pr_link and change_open store the PR in
// the one canonical form, whatever page of it the agent passed.
func TestPRLinkStoresCanonicalURL(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	if out, isErr := callAs(t, h.e, id, "pr_link", map[string]any{"url": "https://github.com/o/r/pull/5/files?w=1"}); isErr {
		t.Fatal(out)
	}
	if out, isErr := callAs(t, h.e, id, "change_open", map[string]any{"key": "k", "title": "K", "pr": "https://github.com/o/r/pull/6#top"}); isErr {
		t.Fatal(out)
	}
	th, _ := h.e.Thread(id)
	if th.MainPR() == nil || th.MainPR().URL != "https://github.com/o/r/pull/5" {
		t.Fatalf("pr_link stored %+v", th.MainPR())
	}
	if c := core.FindChange(&th, "k"); c == nil || c.PRURL() != "https://github.com/o/r/pull/6" {
		t.Fatalf("change_open stored %+v", c)
	}
}

// #31: merge has one guard. The owner's PR-tab command, the press of a
// change's built-in Merge, and the agent's effect_request and change_request
// all refuse a PR with a conflict with the same reason; once it clears, the
// press merges the change's PR and the agent hears the result (V0.60 (4)).
func TestMergeOneGuard(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	url := "https://github.com/acme/app/pull/9"
	set := func(conflict bool) {
		coretest.Patch(h.e, id, func(t *core.Thread, now time.Time) []core.Event {
			core.PRObserved(t, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: "pass", Conflict: conflict, Head: "abc1234def"}, now)
			return nil
		})
	}
	set(true)
	const why = "The PR has merge conflicts"
	if r := h.cmd("pr.merge", id, nil); r.OK || !strings.Contains(r.Error, why) {
		t.Fatalf("PR tab: %+v", r)
	}
	c := changeOf(h, id, "pr:acme/app#9")
	m := findOffer(c.Offers, "fyr.merge")
	if r := h.cmdID("p1", "action.run", id, core.Args{"on": c.ID, "action": "fyr.merge", "digest": m.Digest}); r.OK || r.Status != 409 || !strings.Contains(r.Error, why) {
		t.Fatalf("press: %+v", r)
	}
	if out, isErr := callAs(t, h.e, id, "effect_request", map[string]any{"action": "pr.merge", "args": map[string]any{"pr": url}}); !isErr || !strings.Contains(out, why) {
		t.Fatalf("effect_request: %s", out)
	}
	if out, isErr := callAs(t, h.e, id, "change_request", map[string]any{"change": c.ID, "ask": "merge"}); !isErr || !strings.Contains(out, why) {
		t.Fatalf("change_request: %s", out)
	}
	if h.r.n() != 0 {
		t.Fatalf("the runner ran %d times", h.r.n())
	}
	set(false)
	m = findOffer(changeOf(h, id, c.ID).Offers, "fyr.merge")
	if r := h.cmdID("p2", "action.run", id, core.Args{"on": c.ID, "action": "fyr.merge", "digest": m.Digest, "head": "abc1234def"}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	// A slow machine sends the conflict notice and the merge result in one
	// message, so look for the line in any message the agent got.
	eventually(t, "the agent heard the merge", func() bool {
		for _, ev := range h.events(id) {
			var m map[string]any
			if ev.Type == "user" && json.Unmarshal(ev.Data, &m) == nil {
				text, _ := m["text"].(string)
				if strings.Contains(text, `[Fyr] The owner ran "Merge"`) && strings.Contains(text, "succeeded") {
					return true
				}
			}
		}
		return false
	})
	h.r.mu.Lock()
	req := h.r.calls[0]
	h.r.mu.Unlock()
	if req.Action != core.FyrMerge || req.Args["pr"] != url || req.Args["method"] != "squash" {
		t.Fatalf("request %+v", req)
	}
}

// SPEC 0.5 V0.71: a thread that an action starts (in: "new") follows the
// pressing thread's provider, model and effort and names it as its parent;
// a second press while the first runs starts nothing (the live case, 15 Nov:
// two threads per press, on the owner's personal account instead of
// claudeAcme).
func TestNewThreadActionFollowsParent(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "x", "profile": "claudeAcme", "model": "claude-opus-5-5", "effort": "high"})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	id := r.ThreadID
	h.wait(id, "done", state(core.Done))
	if out, isErr := callAs(t, h.e, id, "offer_action", map[string]any{"on": "thread", "id": "pick", "label": "Pick up #12476", "run": "agent", "in": "new", "prompt": "Pick up issue 12476."}); isErr {
		t.Fatal(out)
	}
	th, _ := h.e.Thread(id)
	a := findOffer(th.Offers, "pick")
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.cmdID(fmt.Sprintf("pick%d", i), "action.run", id, core.Args{"on": "thread", "action": "pick", "digest": a.Digest})
		}()
	}
	wg.Wait()
	var children []core.Thread
	eventually(t, "the child thread", func() bool {
		children = nil
		for _, d := range h.e.Snapshot() {
			if c, _ := h.e.Thread(d.ID); c.Parent == id {
				children = append(children, c)
			}
		}
		return len(children) >= 1
	})
	time.Sleep(200 * time.Millisecond) // a second thread would appear by now
	children = nil
	for _, d := range h.e.Snapshot() {
		if c, _ := h.e.Thread(d.ID); c.Parent == id {
			children = append(children, c)
		}
	}
	if len(children) != 1 {
		t.Fatalf("%d threads for one press", len(children))
	}
	c := children[0]
	if c.Profile != "claudeAcme" || c.Model != "claude-opus-5-5" || c.Effort != "high" {
		t.Fatalf("child provider %q model %q effort %q", c.Profile, c.Model, c.Effort)
	}
}
