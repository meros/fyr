package core

import (
	"strings"
	"testing"
	"time"
)

// V0.changes-code-only (4): a change that is not code and has not ended ends
// as abandoned with the reason; it lands under Done here, nothing about it
// reaches Your move, and an open ask on it is closed and the agent told once.
func TestMigrateCodeOnly(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	was := now.Add(-time.Hour)
	th := &Thread{ID: "t1", ActivityAt: was, Changes: []Change{
		{ID: "c_infra", Key: "k8s:prod/api", Kind: KindInfra, Title: "Raise replicas", Phase: PhaseReview, PhaseBy: ByAgent, UpdatedAt: was,
			Actions: Actions{Offers: []ActionSpec{{ID: "apply", Label: "Apply"}}}},
		{ID: "c_doc", Key: "doc:x", Kind: KindDoc, Title: "Design doc", Phase: PhaseWorking, PhaseBy: ByAgent, UpdatedAt: was},
		{ID: "c_gone", Key: "dns:old", Kind: KindOther, Title: "Old", Phase: PhaseAbandoned, PhaseBy: ByAgent, Label: "dropped", UpdatedAt: was},
		{ID: "c_live", Key: "host:a", Kind: KindHost, Title: "Rebuilt", Phase: PhaseLive, PhaseBy: ByAgent, UpdatedAt: was},
		{ID: "c_code", Key: "git:o/r#b", Kind: KindCode, Title: "Branch b", Branch: "b", Phase: PhaseWorking, PhaseBy: ByObserved, UpdatedAt: was},
	}, Decisions: []Decision{
		{ID: "d_apply", Kind: DecisionEffect, Assignee: Owner, Effect: &EffectReq{Action: "change.apply", Args: map[string]any{"change": "c_infra", "title": "Raise replicas"}}, CreatedAt: was},
		{ID: "d_q", Kind: DecisionAsk, Assignee: Owner, Async: true, CreatedAt: was},
	}}
	if !migrateCodeOnly(th, now) {
		t.Fatal("the pass reported no change")
	}
	for _, id := range []string{"c_infra", "c_doc"} {
		c := FindChange(th, id)
		if c.Phase != PhaseAbandoned || c.Label != CodeOnlyReason || c.Offers != nil {
			t.Fatalf("%s: %+v", id, c)
		}
	}
	if c := FindChange(th, "c_gone"); c.Label != "dropped" {
		t.Fatalf("an ended change was touched: %+v", c)
	}
	if c := FindChange(th, "c_live"); c.Phase != PhaseLive {
		t.Fatalf("a live change was ended: %+v", c)
	}
	if c := FindChange(th, "c_code"); c.Phase != PhaseWorking {
		t.Fatalf("a code change was ended: %+v", c)
	}
	if !th.ActivityAt.Equal(was) {
		t.Fatal("the pass moved the thread: it would reach Your move")
	}
	if len(th.Decisions) != 1 || th.Decisions[0].ID != "d_q" {
		t.Fatalf("decisions: %+v", th.Decisions)
	}
	if len(th.Feed.Queue) != 1 {
		t.Fatalf("notices: %+v", th.Feed.Queue)
	}
	n := th.Feed.Queue[0]
	if n.Urgency != Next || !strings.Contains(n.What, "apply") || !strings.Contains(n.What, "Raise replicas") || !strings.Contains(n.What, "run_request") {
		t.Fatalf("notice: %+v", n)
	}
	// migrateDone after it files them under Done here, as abandoned.
	migrateDone(th, now)
	if c := FindChange(th, "c_infra"); !c.IsDone() || c.Done.Reason != string(PhaseAbandoned) {
		t.Fatalf("done: %+v", c.Done)
	}
	if migrateCodeOnly(th, now) || len(th.Feed.Queue) != 1 {
		t.Fatal("the pass is not idempotent: the agent would hear it twice")
	}
}

// V0.changes-code-only (3): the agent opens code changes only, and asks
// merge only; a kind or an ask that went answers with its replacement.
func TestCodeOnlyRefusals(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	open := func(th *Thread, o ChangeOpen, pr string) error {
		_, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeOpen, Thread: th.ID, Args: ChangeOpenArgs{Open: o, PR: pr}}, now)
		return err
	}
	th := &Thread{ID: "t1"}
	for k, want := range map[ChangeKind]string{KindInfra: "run_request", KindHost: "run_request", KindData: "run_request", KindDoc: "ask", KindDesign: "ask", KindOther: "show"} {
		err := open(th, ChangeOpen{Key: "x", Kind: k, Title: "X", Branch: "b"}, "")
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
			t.Fatalf("%s: %v", k, err)
		}
	}
	if err := open(th, ChangeOpen{Key: "x", Kind: KindCode, Title: "X"}, ""); err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("a code change with no branch and no PR: %v", err)
	}
	if err := open(th, ChangeOpen{Key: "x", Title: "X", Branch: "b"}, ""); err != nil {
		t.Fatalf("no kind is code: %v", err)
	}
	x, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeOpen, Thread: th.ID, Args: ChangeOpenArgs{Open: ChangeOpen{Key: "y", Kind: KindCode, Title: "Y", Branch: "y"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c := FindChange(x, "y"); c == nil || c.Kind != KindCode {
		t.Fatalf("code change: %+v", c)
	}
	for ask, want := range map[string]string{"apply": "run_request", "deploy": "run_request", "revert": "run_request", "review": "ask", "approve": "ask", "ship": "merge"} {
		_, _, _, err := Reduce(x, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: x.ID, Args: ChangeRequest{ID: "d1", Change: "y", Ask: ChangeAsk(ask)}}, now)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", ask, err)
		}
	}
	// V0.one-tool-vocabulary: Fyr's own deploy is the other ask.
	if got := Names(ChangeAsks()); len(got) != 2 || got[0] != string(AskMerge) || got[1] != string(AskFyrDeploy) {
		t.Fatalf("asks: %v", got)
	}
	if got := OpenKinds(); len(got) != 1 || got[0] != KindCode {
		t.Fatalf("kinds: %v", got)
	}
	// Old records still decode: every stored kind stays valid.
	for _, k := range []ChangeKind{KindInfra, KindHost, KindData, KindDoc, KindDesign, KindOther} {
		if !k.Valid() {
			t.Fatalf("%s no longer decodes", k)
		}
	}
}
