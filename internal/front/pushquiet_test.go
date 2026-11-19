package front

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

// SPEC V0.200: a thread the host marks as an auto-merge at work sends no
// push; the same move on an unmarked thread does.
func TestFrontSendsNoPushForAnAutoMerge(t *testing.T) {
	a := fullHost(t, "a", "pa")
	p := &fakePush{}
	fs := startFront(t, Options{Push: p, NotifyDelay: 1}, HostConfig{Name: "a", URL: a.hs.URL})
	online(t, fs.URL, 1)
	const head = "abc1234def5678"
	finish := func(id string, number int, quiet bool) {
		ask(a, id, "Which colour?")
		eventually(t, "the question on the front", func() bool {
			at, _ := threadOf(getState(t, fs.URL), id)["attention"].(map[string]any)
			return at["state"] == "asking"
		})
		coretest.Patch(a.eng, id, func(th *core.Thread, now time.Time) []core.Event {
			th.Decisions, th.Starting = nil, nil // no agent here: the start never ends by itself
			if quiet {
				c := core.ChangePRObserved(th, core.PR{URL: "https://github.com/acme/app/pull/1", Repo: "acme/app", Number: number, State: "MERGED", Head: head}, now)
				c.AutoApproved = &core.AutoApproval{Repo: "acme/app", Head: head, At: now}
				th.QuietMerge = c.ID
			}
			th.ActivityAt = now
			return nil
		})
	}
	merged := create(t, fs.URL, "a", "merged")
	finish(merged, 1, true)
	plain := create(t, fs.URL, "a", "plain")
	finish(plain, 2, false)
	eventually(t, "the plain thread's done push", func() bool {
		for _, pl := range p.payloads() {
			if pl.ThreadID == plain && pl.State == "done" {
				return true
			}
		}
		return false
	})
	for _, pl := range p.payloads() {
		if pl.ThreadID == merged && pl.State == "done" {
			t.Fatalf("the auto-merge pushed: %+v", pl)
		}
	}
}
