package core

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// SPEC V0.run-card-keeps-run: a thread keeps its last runs, but never drops a
// standing card's last run. A card whose run was dropped looked fresh again:
// it came back to the dock with a plain Run, and each press there brought the
// next one back, so cards that had already run ran again without the owner's
// confirmation.

func cardsThread(t *testing.T, n int) *Thread {
	t.Helper()
	th := requestThread()
	for i := 0; i < n; i++ {
		var err error
		th, _, err = runReq(t, th, RunRequestArgs{Command: fmt.Sprintf("step %d", i+1), Why: fmt.Sprintf("Step %d", i+1), Alongside: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	return th
}

// runCard presses a card at minute m and, unless keepRunning, ends its run with ok.
func runCard(t *testing.T, th *Thread, id string, m int, ok, keepRunning bool, extra Args) *Thread {
	t.Helper()
	h, _ := HolderOf(th, OnThread)
	o := h.Find(id)
	if o == nil {
		t.Fatalf("no card %s", id)
	}
	args := Args{"on": OnThread, "action": id, "digest": o.Digest}
	for k, v := range extra {
		args[k] = v
	}
	now := at(0).Add(time.Duration(m) * time.Minute)
	nt, _, res, err := Reduce(th, nil, Input{Source: FromOwner, Kind: KindActionPress, Thread: th.ID, Args: args}, now)
	if err != nil {
		t.Fatalf("press %s: %v", id, err)
	}
	if keepRunning {
		return nt
	}
	nt, _, _, err = Reduce(nt, nil, Input{Source: FromEffect("e-" + id), Kind: KindEffectResult, Thread: th.ID, Args: EffectResult{Effect: res.Effects[0], OK: ok}}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("end %s: %v", id, err)
	}
	return nt
}

func TestRunCardKeepsItsRunPastTheRunsCap(t *testing.T) {
	th := cardsThread(t, 12)
	// run-1 is still running; run-2 succeeded; the other ten run after them.
	th = runCard(t, th, "run-1", 1, true, true, nil)
	th = runCard(t, th, "run-2", 2, true, false, nil)
	for i := 3; i <= 12; i++ {
		th = runCard(t, th, fmt.Sprintf("run-%d", i), i, i%2 == 0, false, nil)
	}
	for _, o := range th.Offers {
		if lastRunOf(th.Runs, o.ID, OnThread) == nil {
			t.Fatalf("card %s lost its run: it looks fresh again; runs: %d", o.ID, len(th.Runs))
		}
	}
	if p := PendingRequest(th); p != nil {
		t.Fatalf("a card that ran waits on the owner again: %s", p.ID)
	}
	h, _ := HolderOf(th, OnThread)
	if why := h.Available(*h.Find("run-1")); why != "It is running" {
		t.Fatalf("run-1 runs, but it can be pressed: %q", why)
	}
	// The runs stay bounded: one per standing card at most beyond the cap.
	if len(th.Runs) > maxRuns+len(th.Offers) {
		t.Fatalf("runs grew: %d", len(th.Runs))
	}
	// Many runs of one card keep only its newest few.
	for i := 13; i < 40; i++ {
		th = runCard(t, th, "run-3", i*3, false, false, nil)
	}
	if len(th.Runs) > maxRuns+len(th.Offers) {
		t.Fatalf("runs grew: %d", len(th.Runs))
	}
}

// SPEC V0.run-card-keeps-run (2): the host asks for the confirmation too. A
// request whose last run succeeded runs again only with again: true, which the
// card sends after Fyr's confirmation (V0.run-card-rerun). A failed one runs
// again from its button.
func TestRunCardRerunNeedsConfirmation(t *testing.T) {
	th := cardsThread(t, 2)
	th = runCard(t, th, "run-1", 1, true, false, nil)
	th = runCard(t, th, "run-2", 2, false, false, nil)
	h, _ := HolderOf(th, OnThread)
	o := h.Find("run-1")
	_, _, _, err := Reduce(th, nil, Input{Source: FromOwner, Kind: KindActionPress, Thread: th.ID, Args: Args{"on": OnThread, "action": o.ID, "digest": o.Digest}}, at(0).Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "already succeeded") {
		t.Fatalf("a succeeded card ran again without its confirmation: %v", err)
	}
	th = runCard(t, th, "run-1", 70, true, false, Args{"again": true})
	th = runCard(t, th, "run-2", 80, true, false, nil)
	if r := lastRunOf(th.Runs, "run-2", OnThread); r == nil || !r.OK {
		t.Fatalf("the failed card did not run again: %+v", r)
	}
}

// SPEC V0.run-card-keeps-run (3): the action event of a press records its
// commandId, beside the actor and the front.
func TestActionEventRecordsCommandID(t *testing.T) {
	th := cardsThread(t, 1)
	o := th.Offers[0]
	_, _, res, err := Reduce(th, nil, Input{Source: FromOwner, Kind: KindActionPress, Thread: th.ID, CmdID: "cmd-1", Actor: "device:dev_1", Front: "FRNT-0000-0000-0000", Args: Args{"on": OnThread, "action": o.ID, "digest": o.Digest}}, at(3))
	if err != nil {
		t.Fatal(err)
	}
	if ev := res.Events[0].Data.(map[string]any); ev["commandId"] != "cmd-1" {
		t.Fatalf("press event without its commandId: %+v", ev)
	}
}
