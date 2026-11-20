package core

import (
	"testing"
	"time"
)

// V0.row-group-code (#454): every Needs you attention carries the code of
// the owner's move, and a reason a row splits carries its word and subject,
// so no client parses the reason's words.
func TestAttentionGroupCodes(t *testing.T) {
	v := &View{SeenAt: at(9)}
	type want struct {
		state         State
		group         Group
		word, subject string
	}
	check := func(name string, a Attention, w want) {
		t.Helper()
		if a.Section != NeedsYou || a.State != w.state || a.Group != w.group || a.Word != w.word || a.Subject != w.subject {
			t.Fatalf("%s: %+v, want %+v", name, a, w)
		}
	}

	ask := &Thread{ID: "t", Decisions: []Decision{{ID: "d1", Kind: DecisionApproval, Assignee: Owner, CreatedAt: at(1)}, {ID: "d2", Kind: DecisionAsk, Assignee: Owner, CreatedAt: at(1)}}}
	check("a question among the asks", Compute(ask, v, at(10)), want{Asking, GroupAnswer, "", ""})
	approval := &Thread{ID: "t", Decisions: []Decision{{ID: "d1", Kind: DecisionApproval, Assignee: Owner, CreatedAt: at(1)}}}
	check("a tool approval", Compute(approval, v, at(10)), want{Asking, GroupApprove, "", ""})

	failed := &Thread{ID: "t"}
	failed.Failure = &Failure{Message: "boom", At: at(1)}
	check("a failure", Compute(failed, v, at(10)), want{Failed, GroupUnblock, "", ""})

	gone := &Thread{ID: "t"}
	gone.Unresponsive = &Unresponsive{Since: at(1), Reason: "no reply"}
	gone.Running = true
	check("not responding", Compute(gone, v, at(10)), want{Blocked, GroupUnblock, "", ""})

	run, _, err := runReq(t, requestThread(), RunRequestArgs{Command: "ls", Why: "Mount the SD card"})
	if err != nil {
		t.Fatal(err)
	}
	a := Compute(run, v, at(10))
	check("a run request", a, want{Blocked, GroupApprove, "Run", "Mount the SD card"})
	if a.Reason != "Run: Mount the SD card" {
		t.Fatalf("run reason %q", a.Reason)
	}

	settle := &Thread{ID: "t", SettleAsk: &SettleAsk{At: at(1)}}
	a = Compute(settle, v, at(10))
	check("a settle offer", a, want{Blocked, GroupApprove, "Settle", "the agent is done"})
	if a.Reason != "Settle: the agent is done" {
		t.Fatalf("settle reason %q", a.Reason)
	}

	ready := needThread(t, ready(headA))
	title := ready.Changes[0].Title
	check("a ready PR", Compute(ready, &View{SeenAt: nt0.Add(time.Hour)}, nt0), want{Blocked, GroupApprove, ReadyMerge, title})

	broken := needThread(t, PR{State: "OPEN", Checks: ChecksFail, Head: headA})
	broken = clock(t, broken, nt0.Add(StopAfter))
	later := nt0.Add(StopAfter)
	a = Compute(broken, &View{SeenAt: later}, later)
	if a.Group != GroupUnblock || a.Subject != broken.Changes[0].Title || a.Reason != a.Subject+": "+a.Word || a.Word == "" {
		t.Fatalf("a change stopped on a failure: %+v", a)
	}

	// Out of Needs you there is no code.
	if a := Compute(&Thread{ID: "t"}, nil, at(10)); a.Group != "" || a.Word != "" || a.Subject != "" {
		t.Fatalf("done: %+v", a)
	}
}
