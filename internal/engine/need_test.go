// SPEC V0.101: computed Needs you in the DTO, the owner's corrections on
// both storages, the four change tools and their aliases.
package engine

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// idle moves the harness clock past the 5-minute rule (V0.101).
func idle(h *harness) {
	h.e.Now = func() time.Time { return time.Now().Add(10 * time.Minute) }
}

func observePRIn(t *testing.T, e *Engine, id string, p core.PR) {
	t.Helper()
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsGitHub), Kind: core.KindPRObserved, Thread: id, Args: p}); err != nil {
		t.Fatal(err)
	}
}

const needPR = "https://github.com/o/r/pull/7"

// SPEC V0.101 (1): the DTO carries need {cause, head}; the client only
// reads it.
func TestDTOCarriesNeed(t *testing.T) {
	e, _, now := bare(t)
	id := bareThread(t, e)
	observePRIn(t, e, id, core.PR{URL: needPR, Repo: "o/r", Number: 7, State: "OPEN", Checks: core.ChecksPass, MergeState: "CLEAN", Head: "a1b2c3d4e5f6"})
	cs := shownIn(e, id)
	if len(cs) != 1 || cs[0].Need == nil || cs[0].Need.Cause != core.CauseReady || cs[0].Need.Head != "a1b2c3d4e5f6" {
		t.Fatalf("need: %+v", cs)
	}
	*now = now.Add(time.Minute)
	observePRIn(t, e, id, core.PR{URL: needPR, Repo: "o/r", Number: 7, State: "OPEN", Checks: core.ChecksPending, MergeState: "CLEAN", Head: "a1b2c3d4e5f6"})
	if cs := shownIn(e, id); cs[0].Need != nil {
		t.Fatalf("need with checks pending: %+v", cs[0].Need)
	}
}

// SPEC V0.101 (4): the agent's change_update on an observed PR gets the
// observed value and no error; abandoned on an open PR is taken;
// change_unchanged is a no-op; the aliases are not listed but still work.
func TestChangeUpdateObserved(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	call := func(tool string, a map[string]any) (string, bool) { return callAs(t, h.e, id, tool, a) }
	merged := time.Now().Add(-time.Hour)
	observePRIn(t, h.e, id, core.PR{URL: needPR, Repo: "o/r", Number: 7, State: "MERGED", Checks: core.ChecksPass, Head: "a1b2c3d", MergedAt: &merged})
	out, isErr := call("change_update", map[string]any{"change": "#7", "phase": "review"})
	want := "Fyr reads #7 from GitHub: merged " + merged.Local().Format("15:04") + "."
	if isErr || !strings.Contains(out, want) {
		t.Fatalf("observed phase: %q (error %v)", out, isErr)
	}
	if c := changeOf(h, id, "o/r#7"); c.Phase != core.PhaseLanded {
		t.Fatalf("the agent's value was taken: %s", c.Phase)
	}
	open := "https://github.com/o/r/pull/8"
	observePRIn(t, h.e, id, core.PR{URL: open, Repo: "o/r", Number: 8, State: "OPEN", Checks: core.ChecksPending, Head: "b2c3d4e"})
	out, isErr = call("change_update", map[string]any{"change": open, "phase": "abandoned"})
	if isErr || !strings.Contains(out, core.StillOpenText) {
		t.Fatalf("abandoned on an open PR: %q (error %v)", out, isErr)
	}
	if c := changeOf(h, id, open); c.Phase != core.PhaseAbandoned || !c.IsDone() {
		t.Fatalf("not abandoned: %+v", c)
	}
	before := dtoOf(h.e, id).Rev
	if out, isErr := call("change_unchanged", map[string]any{"reason": "x"}); isErr || out != UnchangedText {
		t.Fatalf("change_unchanged: %q", out)
	}
	if after := dtoOf(h.e, id).Rev; after != before {
		t.Fatalf("change_unchanged changed the thread: %d -> %d", before, after)
	}
	if out, isErr := call("change_status", map[string]any{"change": "#7", "phase": "review"}); isErr || !strings.Contains(out, "Fyr reads #7") {
		t.Fatalf("the alias: %q", out)
	}
	if out, isErr := call("change_update", map[string]any{"change": "#7", "needs": "owner"}); isErr || strings.Contains(out, "needs") {
		t.Fatalf("needs is not in the tool: %q", out)
	}
	listed := map[string]bool{}
	for _, n := range toolNames(t, h.e, id) {
		listed[n] = true
	}
	for _, n := range []string{"change_open", "change_update", "change_request", "change_list"} {
		if !listed[n] {
			t.Errorf("%s is not listed", n)
		}
	}
	for _, n := range []string{"change_status", "change_revise", "change_attach", "change_unchanged", "pr_link"} {
		if listed[n] {
			t.Errorf("the alias %s is listed", n)
		}
	}
}

// SPEC V0.101 (3): Refresh now reads GitHub and git now, on a thread change
// and on a host record; Abandon on a host record goes down the record's
// path and the thread shows it done.
func TestOwnerCorrectionsOnBothStorages(t *testing.T) {
	e, _, _ := bare(t)
	e.Git = &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	dir := t.TempDir()
	a := threadIn(t, e, dir)
	e.observeGit(a, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"})
	observePRIn(t, e, a, core.PR{URL: needPR, Repo: "o/r", Number: 7, State: "OPEN", Checks: core.ChecksPending})
	var mu sync.Mutex
	kicks := 0
	e.OnPR = func(string) { mu.Lock(); kicks++; mu.Unlock() }
	var rec, own string
	for _, c := range shownIn(e, a) {
		if c.Threads != nil {
			rec = c.ID
		} else {
			own = c.ID
		}
	}
	if rec == "" || own == "" {
		t.Fatalf("setup: %+v", shownIn(e, a))
	}
	for i, id := range []string{own, rec} {
		if r := e.Command(api.Cmd{CommandID: "refresh-" + id, Action: core.KindChangeRefresh, ThreadID: a, Args: core.Args{"change": id}}); !r.OK {
			t.Fatalf("refresh %s: %+v", id, r)
		}
		eventually(t, "the poller was kicked", func() bool { mu.Lock(); defer mu.Unlock(); return kicks == i+1 })
	}
	if r := e.Command(api.Cmd{CommandID: "end-rec", Action: core.KindChangeEnd, ThreadID: a, Args: core.Args{"change": rec}}); !r.OK {
		t.Fatalf("abandon a record: %+v", r)
	}
	if c := e.records[rec]; c.Phase != core.PhaseAbandoned || c.PhaseBy != core.ByOwner || !c.IsDone() {
		t.Fatalf("record after Abandon: %+v", c)
	}
	if r := e.Command(api.Cmd{CommandID: "end-own", Action: core.KindChangeEnd, ThreadID: a, Args: core.Args{"change": own}}); !r.OK {
		t.Fatalf("abandon a thread change: %+v", r)
	}
	th, _ := e.Thread(a)
	if c := core.FindChange(&th, own); c.Phase != core.PhaseAbandoned || c.PhaseBy != core.ByOwner || !c.IsDone() {
		t.Fatalf("thread change after Abandon: %+v", c)
	}
}
