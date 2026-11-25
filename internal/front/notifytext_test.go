package front

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/stream"
)

// V0.notification-polish: a push says what the inbox row says (rows.ts
// rowState and rowMeta): no action id, no "Blocked" before a cause that is
// its own sentence, and a question beside a merge ask leads.
func TestPushSaysWhatTheRowSays(t *testing.T) {
	th := func(state core.State, reason string, change func(*core.Thread)) api.Thread {
		x := &core.Thread{ID: "t1", Title: "Fix lasso"}
		if change != nil {
			change(x)
		}
		return api.Thread{Thread: x, Attention: core.Attention{State: state, Reason: reason}}
	}
	merge := core.Decision{ID: "d1", Kind: core.DecisionEffect, Effect: &core.EffectReq{Action: "pr.merge", Args: map[string]any{"pr": "https://github.com/acme/app/pull/12"}}}
	question := core.Decision{ID: "d2", Kind: core.DecisionAsk, Questions: []core.Question{{Question: "Which colour?"}, {Question: "Which size?"}}}
	for _, c := range []struct {
		name string
		d    api.Thread
		want string
	}{
		{"a question", th(core.Asking, "ask", func(x *core.Thread) { x.Decisions = []core.Decision{question} }), "2 questions · Which colour?"},
		{"a question beside a merge ask", th(core.Asking, "effect", func(x *core.Thread) { x.Decisions = []core.Decision{merge, question} }), "2 questions · Which colour?"},
		{"a merge ask", th(core.Asking, "effect", func(x *core.Thread) { x.Decisions = []core.Decision{merge} }), "Merge #12 · Merge pull request #12"},
		{"a merge ask with a reason", th(core.Asking, "effect", func(x *core.Thread) {
			m := merge
			m.Effect = &core.EffectReq{Action: "pr.merge", Args: merge.Effect.Args, Reason: "CI is green and you said to merge."}
			x.Decisions = []core.Decision{m}
		}), "Merge #12 · CI is green and you said to merge."},
		{"an update of Fyr", th(core.Asking, "effect", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionEffect, Effect: &core.EffectReq{Action: "fyr.deploy"}}}
		}), "Update Fyr · Update Fyr to a new version"},
		{"another effect", th(core.Asking, "effect", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionEffect, Effect: &core.EffectReq{Action: "issue.close"}}}
		}), "Issue close · Waits for your approval"},
		{"an approval with its command", th(core.Asking, "approval", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionApproval, Tool: "Bash", Input: map[string]any{"command": "git push"}}}
		}), "Approval · Run command: git push"},
		{"an approval without input", th(core.Asking, "approval", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionApproval, Tool: "Write"}}
		}), "Approval · Write file"},
		{"an approval for an offered action", th(core.Asking, "approval", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionApproval, Tool: "mcp__fyr__offer_action", Input: map[string]any{"label": "Write ADR and spec", "on": "thread"}}}
		}), "Approval · Offer action: Write ADR and spec"},
		{"an approval for an external MCP tool", th(core.Asking, "approval", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionApproval, Tool: "mcp__github__get_pr", Input: map[string]any{"title": "Fix typo"}}}
		}), "Approval · Allow GitHub get pr: Fix typo"},
		{"a failure", th(core.Failed, "API error 500", func(x *core.Thread) { x.Failure = &core.Failure{Message: "API error 500"} }), "Failed · API error 500"},
		{"a ready pull request", th(core.Blocked, "Add login: Merge", nil), "Add login: Merge"},
		{"a command to run", th(core.Blocked, "Run: install the driver", nil), "Run: install the driver"},
		{"finished", th(core.Done, "", func(x *core.Thread) { x.Summary = "Both tests pass." }), "Finished · Both tests pass."},
		{"finished without a last line", th(core.Done, "", nil), "Finished"},
	} {
		meta, detail := AttentionDetail(c.d)
		if got := push.PayloadFor("t1", "Fix lasso", string(c.d.Attention.State), meta, detail).Body; got != c.want {
			t.Errorf("%s: body %q, want %q", c.name, got, c.want)
		}
	}
}

// V0.notification-polish: a thread that finishes, starts again and finishes
// inside the wait sends one push, not one per finish.
func TestFrontPushesOnceForATurnThatEndsTwice(t *testing.T) {
	a := fullHost(t, "a", "pa")
	p := &fakePush{}
	const wait = 1500 * time.Millisecond
	fs := startFront(t, Options{Push: p, NotifyDelay: wait}, HostConfig{Name: "a", URL: a.hs.URL})
	online(t, fs.URL, 1)
	id := create(t, fs.URL, "a", "twice")
	state := func(want string) {
		t.Helper()
		eventually(t, want+" on the front", func() bool {
			at, _ := threadOf(getState(t, fs.URL), id)["attention"].(map[string]any)
			return at["state"] == want
		})
	}
	set := func(running bool) {
		coretest.Patch(a.eng, id, func(th *core.Thread, now time.Time) []core.Event {
			th.Decisions, th.Running, th.Starting, th.ActivityAt = nil, running, nil, now
			return nil
		})
	}
	ask(a, id, "Which colour?")
	state("asking")
	start := time.Now()
	set(false)
	state("done")
	set(true)
	state("working")
	set(false)
	state("done")
	if time.Since(start) > wait/2 {
		t.Skip("the machine is too slow for this test's wait")
	}
	dones := func() (n int) {
		for _, pl := range p.payloads() {
			if pl.ThreadID == id && pl.State == "done" {
				n++
			}
		}
		return n
	}
	eventually(t, "the done push", func() bool { return dones() == 1 })
	time.Sleep(wait + 200*time.Millisecond)
	if n := dones(); n != 1 {
		t.Fatalf("%d done pushes for one finished thread", n)
	}
}

// V0.quota-reset-wake: when a host reports a quota reset for a thread, the
// front pushes after NotifyDelay if the thread still waits, and stays silent
// if the thread resumed working.
func TestFrontPushesOnQuotaResetWhenThreadStillWaits(t *testing.T) {
	a := fullHost(t, "a", "pa")
	p := &fakePush{}
	const wait = 80 * time.Millisecond
	fs := startFront(t, Options{Push: p, NotifyDelay: wait}, HostConfig{Name: "a", URL: a.hs.URL})
	online(t, fs.URL, 1)
	idWait := create(t, fs.URL, "a", "waiting thread")
	idRun := create(t, fs.URL, "a", "running thread")
	coretest.Patch(a.eng, idWait, func(th *core.Thread, now time.Time) []core.Event {
		th.Running, th.Starting, th.ActivityAt = false, nil, now
		return nil
	})
	coretest.Patch(a.eng, idRun, func(th *core.Thread, now time.Time) []core.Event {
		th.Running, th.Starting, th.ActivityAt = true, nil, now
		return nil
	})
	// The front's own copy must hold both states before the reset arrives:
	// getState reads through to the host and says nothing about it, and the
	// reset's timer reads that copy.
	eventually(t, "threads synced to front", func() bool {
		return fs.frontAttention(idWait) == "done" && fs.frontAttention(idRun) == "working"
	})
	a.eng.Hub.Send(stream.FrameQuotaReset, map[string]any{"threadId": idWait, "provider": "claude", "window": "5 h", "at": time.Now()})
	a.eng.Hub.Send(stream.FrameQuotaReset, map[string]any{"threadId": idRun, "provider": "claude", "window": "5 h", "at": time.Now()})
	eventually(t, "quota reset push for waiting thread", func() bool {
		for _, pl := range p.payloads() {
			if pl.ThreadID == idWait && pl.State == "quota" {
				return pl.Body == "Quota reset · The 5 h window reset; this thread still waits"
			}
		}
		return false
	})
	time.Sleep(wait + 80*time.Millisecond)
	for _, pl := range p.payloads() {
		if pl.ThreadID == idRun && pl.State == "quota" {
			t.Fatalf("pushed quota reset for a working thread: %+v", pl)
		}
	}
}
