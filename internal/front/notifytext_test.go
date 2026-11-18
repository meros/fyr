package front

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/push"
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
		}), "Approval · Allow Bash git push"},
		{"an approval without one", th(core.Asking, "approval", func(x *core.Thread) {
			x.Decisions = []core.Decision{{Kind: core.DecisionApproval, Tool: "Write"}}
		}), "Approval · Allow Write"},
		{"a failure", th(core.Failed, "API error 500", func(x *core.Thread) { x.Failure = &core.Failure{Message: "API error 500"} }), "Failed · API error 500"},
		{"a ready pull request", th(core.Blocked, "Add login: Merge", nil), "Add login: Merge"},
		{"a command to run", th(core.Blocked, "Run: install the driver", nil), "Run: install the driver"},
		{"a question in text", th(core.Blocked, "Shall I also fix the phone?", func(x *core.Thread) {
			x.TextAsk = &core.TextAsk{Question: "Shall I also fix the phone?"}
		}), "Question in text · Shall I also fix the phone?"},
		{"a text question behind another cause", th(core.Blocked, "Add login: Merge", func(x *core.Thread) {
			x.TextAsk = &core.TextAsk{Question: "Shall I also fix the phone?"}
		}), "Add login: Merge"},
		{"finished", th(core.Done, "", func(x *core.Thread) { x.Summary = "Both tests pass." }), "Done · Both tests pass."},
		{"finished without a last line", th(core.Done, "", nil), "Done"},
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
			th.Decisions, th.Running, th.ActivityAt = nil, running, now
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
