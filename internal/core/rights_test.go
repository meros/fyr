package core

import (
	"errors"
	"testing"
	"time"
)

// #45: field rights are one table. The agent writing a fact Fyr observed
// (the PR's checks, its phase), marking a review or an approval ok, or
// sending an observer's Input is rejected by the reducer; nothing changes.
func TestFieldRightsRejectTheAgent(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t"}
	c := ChangePRObserved(th, PR{URL: "https://github.com/o/r/pull/5", Repo: "o/r", Number: 5, State: "OPEN", Checks: ChecksFail}, now)
	agent := func(kind string, args any) error {
		before := CloneThread(th)
		_, _, _, err := Reduce(th, nil, Input{Source: FromAgent, Kind: kind, Thread: "t", Args: args}, now)
		if a, b := CloneThread(th), before; a.Changes[0].Conditions[0] != b.Changes[0].Conditions[0] {
			t.Fatal("the reduce changed the record it was given")
		}
		return err
	}
	rights := func(what string, err error) {
		t.Helper()
		var r *Rejection
		if !errors.As(err, &r) || r.Code != RejectRights {
			t.Fatalf("%s: got %v, want a rights rejection", what, err)
		}
	}
	// V0.101: a value for an observed field is ignored, not refused; the
	// reply names what Fyr reads (need_test.go).
	for what, u := range map[string]ChangeUpdate{"checks ok": {Conditions: []Condition{{Type: CondChecks, Status: CondOK}}}, "phase": {Phase: PhaseLanded}} {
		nt, _, res, err := Reduce(th, nil, Input{Source: FromAgent, Kind: KindChangeStatus, Thread: "t", Args: ChangeStatusArgs{Change: c.ID, Update: u}}, now)
		r, _ := res.Reply.(ChangeReply)
		if err != nil || len(r.Notes) != 1 || nt.Changes[0].Phase != PhaseReview || nt.Changes[0].Conditions[0].Status != CondFail {
			t.Fatalf("%s: %v %q %+v", what, err, r.Notes, nt.Changes[0])
		}
	}
	rights("approval ok", agent(KindChangeStatus, ChangeStatusArgs{Change: c.ID, Update: ChangeUpdate{Conditions: []Condition{{Type: CondApproval, Status: CondOK}}}}))
	rights("review ok", agent(KindChangeStatus, ChangeStatusArgs{Change: c.ID, Update: ChangeUpdate{Conditions: []Condition{{Type: CondReview, Status: CondOK}}}}))
	rights("pr.observed", agent(KindPRObserved, PR{URL: "https://github.com/o/r/pull/5", State: "MERGED"}))
	// A claim the agent may make still applies; the owner may mark approval ok.
	if err := agent(KindChangeStatus, ChangeStatusArgs{Change: c.ID, Update: ChangeUpdate{Conditions: []Condition{{Type: "lint", Status: CondOK}}}}); err != nil {
		t.Fatalf("a claim: %v", err)
	}
	if err := CanWrite(FieldApprovalOK, ByOwner, "", "approval"); err != nil {
		t.Fatalf("owner approval: %v", err)
	}
}
