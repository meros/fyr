// SPEC V0.101: Needs you is computed on the host (decision 0024 section 5,
// slice 5): an open ask, Ready, Stopped on a failure; Not now by head.
package core

import (
	"strings"
	"testing"
	"time"
)

var nt0 = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

const (
	headA = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	headB = "b2c3d4e5f60718293a4b5c6d7e8f901234567890"
)

// needText is the thread's Needs you reason on a stopped thread, or "".
func needText(th *Thread) string {
	th.Stopped = true
	c, n := ThreadNeed(th)
	if n == nil {
		return ""
	}
	return c.Title + ": " + n.Text
}

// needThread is an idle thread whose last run ended at nt0, with an open PR.
func needThread(t *testing.T, p PR) *Thread {
	t.Helper()
	th := &Thread{ID: "t", Title: "x"}
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, nt0.Add(-time.Minute))
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, nt0)
	th, _ = observePR(t, th, p, nt0)
	return th
}

func ready(head string) PR {
	return PR{State: "OPEN", Checks: ChecksPass, MergeState: "CLEAN", Head: head, Title: "Fix"}
}

func clock(t *testing.T, th *Thread, now time.Time) *Thread {
	t.Helper()
	th, _ = fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, now)
	return th
}

// SPEC V0.101 (1): Ready is a cause with the head; a run in progress takes
// it away; a done change is never a cause.
func TestNeedReady(t *testing.T) {
	th := needThread(t, ready(headA))
	n := ChangeNeed(th, &th.Changes[0])
	if n == nil || n.Cause != CauseReady || n.Text != ReadyMerge || n.Head != headA {
		t.Fatalf("ready: %+v", n)
	}
	if a := Compute(th, &View{SeenAt: nt0.Add(time.Hour)}, nt0); a.Section != NeedsYou || a.State != Blocked {
		t.Fatalf("not in Needs you: %+v", a)
	}
	running, _ := fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, nt0.Add(time.Minute))
	if n := ChangeNeed(running, &running.Changes[0]); n != nil {
		t.Fatalf("ready while a run goes: %+v", n)
	}
	done := dstep(t, th, FromOwner, KindChangeDone, Args{"change": th.Changes[0].ID}, nt0.Add(time.Minute))
	if n := ChangeNeed(done, &done.Changes[0]); n != nil {
		t.Fatalf("a done change is a cause: %+v", n)
	}
}

// SPEC V0.101 (1): an open ask is a cause; answering or declining it (Not
// now on an ask) ends it.
func TestNeedAsk(t *testing.T) {
	th := &Thread{ID: "t"}
	OpenChange(th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Design"}, ByAgent, nt0)
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeRequest, Args: ChangeRequest{ID: "d1", Change: "doc:x", Ask: AskApprove, Reason: "Approve the design"}}, nt0)
	n := ChangeNeed(th, &th.Changes[0])
	if n == nil || n.Cause != CauseAsk || n.Ask != "d1" || n.Text != "Approve the design" {
		t.Fatalf("ask: %+v", n)
	}
	if a := Compute(th, &View{SeenAt: nt0.Add(time.Hour)}, nt0); a.Section != NeedsYou {
		t.Fatalf("an ask is not in Needs you: %+v", a)
	}
	no, _ := fstep(t, th, Input{Source: FromOwner, Kind: "thread.answer", Args: Args{"decisionId": "d1", "allow": false}}, nt0.Add(time.Minute))
	if n := ChangeNeed(no, &no.Changes[0]); n != nil || len(no.Decisions) != 0 {
		t.Fatalf("declined ask stays: %+v", n)
	}
}

// SPEC V0.101 (1): an archived thread voids its asks; a done change voids
// its ask.
func TestAskVoid(t *testing.T) {
	th := &Thread{ID: "t"}
	OpenChange(th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Design"}, ByAgent, nt0)
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeRequest, Args: ChangeRequest{ID: "d1", Change: "doc:x", Ask: AskReview, Reason: "look"}}, nt0)
	arch, res := fstep(t, th, Input{Source: FromOwner, Kind: "thread.archive"}, nt0.Add(time.Minute))
	if len(arch.Decisions) != 0 || !strings.Contains(eventsText(res), "archived") {
		t.Fatalf("archived thread keeps its ask: %+v", arch.Decisions)
	}
	done := dstep(t, th, FromOwner, KindChangeDone, Args{"change": th.Changes[0].ID}, nt0.Add(time.Minute))
	if len(done.Decisions) != 0 {
		t.Fatalf("an ask on a done change stays: %+v", done.Decisions)
	}
}

func eventsText(r Result) string {
	var b strings.Builder
	for _, e := range r.Events {
		b.WriteString(e.Type)
		data, _ := e.Data.(map[string]any)
		for _, v := range data {
			if s, ok := v.(string); ok {
				b.WriteString(" " + s)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// SPEC V0.101 (1): a failure is Stopped on a failure only 5 minutes after
// the last run ended, and only through the clock's reduce at NextCheck.
func TestNeedStoppedOnFailure(t *testing.T) {
	th := needThread(t, PR{State: "OPEN", Checks: ChecksFail, Head: headA})
	if n := ChangeNeed(th, &th.Changes[0]); n != nil {
		t.Fatalf("a cause right after the run: %+v", n)
	}
	if at := NextCheck(th, nil, nt0); at == nil || !at.Equal(nt0.Add(StopAfter)) {
		t.Fatalf("next check: %v, want %v", at, nt0.Add(StopAfter))
	}
	later := nt0.Add(6 * time.Minute)
	if a := Compute(th, &View{SeenAt: later}, later); a.Section == NeedsYou {
		t.Fatalf("in Needs you without the clock's reduce: %+v", a)
	}
	th = clock(t, th, nt0.Add(StopAfter))
	n := ChangeNeed(th, &th.Changes[0])
	if n == nil || n.Cause != CauseFailure || n.Head != headA {
		t.Fatalf("stopped on a failure: %+v", n)
	}
	if a := Compute(th, &View{SeenAt: later}, later); a.State != Blocked {
		t.Fatalf("not blocked: %+v", a)
	}
	if at := NextCheck(th, nil, later); at != nil {
		t.Fatalf("the clock keeps coming: %v", at)
	}
}

// SPEC V0.101 (1): a CI failure while the agent runs is "Agent is fixing
// CI", muted, not in Needs you.
func TestNeedFixingWhileRunning(t *testing.T) {
	th := needThread(t, PR{State: "OPEN", Checks: ChecksFail, Head: headA})
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, nt0.Add(10*time.Minute))
	n := ChangeNeed(th, &th.Changes[0])
	if n == nil || n.Cause != CauseFixing || !n.Muted || n.Text != FixingCI {
		t.Fatalf("fixing: %+v", n)
	}
	if c, x := ThreadNeed(th); x != nil {
		t.Fatalf("a muted need is in Needs you: %s %+v", c.Title, x)
	}
}

// SPEC V0.101 (2): Not now holds until the head changes or a new failure
// type appears, not before.
func TestNotNowByHead(t *testing.T) {
	th := needThread(t, PR{State: "OPEN", Checks: ChecksFail, Head: headA})
	th = clock(t, th, nt0.Add(StopAfter))
	id := th.Changes[0].ID
	th, _ = fstep(t, th, Input{Source: FromOwner, Kind: KindChangePark, Args: Args{"change": id}}, nt0.Add(6*time.Minute))
	if n := ChangeNeed(th, &th.Changes[0]); n != nil {
		t.Fatalf("parked change is a cause: %+v", n)
	}
	// The same head, another reason and another poll: still parked.
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: headA, Review: "REVIEW_REQUIRED"}, nt0.Add(7*time.Minute))
	if th.Changes[0].Parked == nil {
		t.Fatal("back before the head changed")
	}
	// A new failure type: back.
	x, _ := observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: headA, Conflict: true}, nt0.Add(8*time.Minute))
	if x.Changes[0].Parked != nil {
		t.Fatal("a new failure type did not bring it back")
	}
	// A new head: back.
	y, _ := observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: headB}, nt0.Add(8*time.Minute))
	if y.Changes[0].Parked != nil {
		t.Fatal("a new head did not bring it back")
	}
	// Ready parks the same way.
	r := needThread(t, ready(headA))
	r, _ = fstep(t, r, Input{Source: FromOwner, Kind: KindChangePark, Args: Args{"change": r.Changes[0].ID}}, nt0.Add(time.Minute))
	if ChangeNeed(r, &r.Changes[0]) != nil {
		t.Fatal("parked Ready is a cause")
	}
	r, _ = observePR(t, r, ready(headB), nt0.Add(2*time.Minute))
	if n := ChangeNeed(r, &r.Changes[0]); n == nil || n.Head != headB {
		t.Fatalf("Ready not back on a new head: %+v", n)
	}
}

// SPEC V0.101 (1): a finished run whose result the owner has not seen is
// unread in Seen, not Needs you; a run failure stays a cause.
func TestUnseenResultInSeen(t *testing.T) {
	th := &Thread{ID: "t", ActivityAt: nt0}
	if a := Compute(th, &View{SeenAt: nt0.Add(-time.Minute)}, nt0); a.State != Done || a.Section != SeenSec {
		t.Fatalf("unseen result: %+v", a)
	}
	th.Failure = &Failure{Class: "api", Message: "overloaded", At: nt0}
	if a := Compute(th, &View{SeenAt: nt0.Add(-time.Minute)}, nt0); a.Section != NeedsYou {
		t.Fatalf("run failure: %+v", a)
	}
}

// SPEC V0.101 (3): Abandon on a host record ends it as the owner's and
// marks it done; only the owner sends it.
func TestRecordAbandon(t *testing.T) {
	rs, rec := observe(t, nil, GitFacts{Repo: "github.com/o/r", Branch: "feat", Ahead: 1, Head: "h1"}, nt0)
	if _, _, _, err := ReduceRecord(rs, Input{Source: FromAgent, Kind: KindRecordEnd, Args: Args{"change": rec.ID}}, nt0); err == nil {
		t.Fatal("the agent abandoned a host record")
	}
	_, after, _, err := ReduceRecord(rs, Input{Source: FromOwner, Kind: KindRecordEnd, Args: Args{"change": rec.ID}}, nt0)
	if err != nil || after.Phase != PhaseAbandoned || after.PhaseBy != ByOwner || !after.IsDone() {
		t.Fatalf("abandon: %+v %v", after, err)
	}
}

// SPEC V0.101 (3): Refresh now on a thread change and on a host record the
// thread shows reads git and GitHub now.
func TestRefreshNow(t *testing.T) {
	th := needThread(t, ready(headA))
	th.Refs = []Ref{{ID: "c_rec"}}
	for _, id := range []string{th.Changes[0].ID, "c_rec"} {
		_, res := fstep(t, th, Input{Source: FromOwner, Kind: KindChangeRefresh, Args: Args{"change": id}}, nt0)
		if len(res.Effects) == 0 || res.Effects[0].Kind != EffectChangeRefresh {
			t.Fatalf("%s: effects %+v", id, res.Effects)
		}
	}
	if _, _, _, err := Reduce(th, nil, Input{Source: FromOwner, Kind: KindChangeRefresh, Thread: "t", Args: Args{"change": "nope"}}, nt0); err == nil {
		t.Fatal("refresh of an unknown change")
	}
}

// SPEC V0.101 (4): a change reference is an id, a key, a PR URL,
// owner/repo#N or #N when it is unique in the thread.
func TestChangeReferences(t *testing.T) {
	th := feedThread()
	id := th.Changes[0].ID
	for _, ref := range []string{id, "https://github.com/o/r/pull/7", "o/r#7", "#7"} {
		if c := FindChange(th, ref); c == nil || c.ID != id {
			t.Errorf("%s: %+v", ref, c)
		}
	}
	ChangePRObserved(th, PR{URL: "https://github.com/o/other/pull/7", Repo: "o/other", Number: 7, State: "OPEN"}, ct0)
	if c := FindChange(th, "#7"); c != nil {
		t.Errorf("#7 is not unique but found %s", c.ID)
	}
}

// SPEC V0.101 (4): an agent write to an observed field is not refused: the
// value is ignored and the reply names what Fyr reads; abandoned on an open
// PR is taken; the owner's Done still refuses.
func TestReportObservedField(t *testing.T) {
	merged := nt0.Add(42 * time.Minute)
	th := needThread(t, PR{State: "MERGED", Checks: ChecksPass, Head: headA, MergedAt: &merged})
	th.Changes[0].Done = nil
	c := &th.Changes[0]
	notes, err := ReportChange(th, c, ChangeUpdate{Phase: PhaseReview}, ByAgent, nt0.Add(time.Hour))
	want := "Fyr reads #7 from GitHub: merged " + merged.Local().Format("15:04") + "."
	if err != nil || len(notes) != 1 || notes[0] != want || c.Phase != PhaseLanded {
		t.Fatalf("observed phase: %q %v %s", notes, err, c.Phase)
	}
	open := needThread(t, PR{State: "OPEN", Checks: ChecksPending, Head: headA})
	notes, err = ReportChange(open, &open.Changes[0], ChangeUpdate{Phase: PhaseAbandoned}, ByAgent, nt0.Add(time.Hour))
	if err != nil || len(notes) != 1 || notes[0] != StillOpenText || open.Changes[0].Phase != PhaseAbandoned {
		t.Fatalf("abandoned on an open PR: %q %v %s", notes, err, open.Changes[0].Phase)
	}
	// The next poll of the open PR does not bring it back.
	open, _ = observePR(t, open, PR{State: "OPEN", Checks: ChecksPending, Head: headA}, nt0.Add(2*time.Hour))
	if open.Changes[0].Phase != PhaseAbandoned || !open.Changes[0].IsDone() {
		t.Fatalf("back after a poll: %s", open.Changes[0].Phase)
	}
	doc := &Thread{ID: "t"}
	d, _, _ := OpenChange(doc, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc"}, ByAgent, nt0)
	d.Done = &DoneMark{At: nt0, By: ByOwner, Reason: DoneByOwner}
	if _, err := ReportChange(doc, d, ChangeUpdate{Phase: PhaseReview}, ByAgent, nt0); err == nil {
		t.Fatal("an agent write over the owner's Done")
	}
}
