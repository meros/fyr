package core

import (
	"strings"
	"testing"
	"time"
)

// SPEC 0.5 V0.62: open changes the agent owns go stale in a run without a
// change tool call; observed changes and ended ones never do.
func TestChangesStale(t *testing.T) {
	start := ct0
	th := &Thread{Conversation: Conversation{RunStartedAt: &start}}
	ChangePRObserved(th, PR{URL: "https://github.com/o/r/pull/1", Repo: "o/r", Number: 1, State: "OPEN"}, ct0)
	if ChangesStale(th) {
		t.Fatal("an observed PR change asked for upkeep")
	}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k8s:x", Kind: "infra", Title: "x"}, ByAgent, ct0.Add(-time.Hour))
	if !ChangesStale(th) {
		t.Fatal("an agent change untouched this run is not stale")
	}
	ChangesChecked(th, ct0.Add(time.Minute))
	if ChangesStale(th) {
		t.Fatal("a change tool call this run did not count")
	}
	old := ct0.Add(-2 * time.Hour)
	th.ChangesCheckedAt = &old
	c.Phase = PhaseAbandoned
	if ChangesStale(th) {
		t.Fatal("an ended change asked for upkeep")
	}
	if !(ViewStop{Changes: true}).Block() || (ViewStop{Changes: true, Blocked: true}).Block() || (ViewStop{Changes: true, Tasks: 1}).Block() {
		t.Fatal("the Stop hook rules for changes differ from the view's")
	}
	if r := StopReason(true, true); !strings.Contains(r, "view_unchanged") || !strings.Contains(r, "change_unchanged") {
		t.Fatalf("combined reason: %s", r)
	}
	if StopReason(true, false) != ViewStopReason || !strings.Contains(StopReason(false, true), "change_status") {
		t.Fatal("single reasons")
	}
}
