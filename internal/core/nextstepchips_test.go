package core

import "testing"

// SPEC V0.next-step-chips (3): an optional run request waits on nobody, so
// it never puts the thread in Your move; a blocking one does.
func TestOptionalRequestIsNotPending(t *testing.T) {
	th, _, err := runReq(t, requestThread(), RunRequestArgs{Command: "make e2e", Why: "Run the e2e suite", Optional: true})
	if err != nil {
		t.Fatal(err)
	}
	if !th.Offers[0].Optional || PendingRequest(th) != nil {
		t.Fatalf("an optional request is pending: %+v", th.Offers)
	}
	th, _, err = runReq(t, th, RunRequestArgs{ID: "mount", Command: "udisksctl mount -b /dev/sdc1", Why: "Mount the SD card"})
	if err != nil {
		t.Fatal(err)
	}
	if p := PendingRequest(th); p == nil || p.ID != "mount" {
		t.Fatalf("the blocking request is not pending: %+v", p)
	}
}

// SPEC V0.next-step-chips: optional changes the digest only when set, so a
// request declared before this version keeps the digest its card showed.
func TestOptionalDigest(t *testing.T) {
	a := ActionSpec{ID: "r", Label: "Run", Run: RunScript, Argv: RunArgv("x"), Request: true, Command: "x", Why: "y"}
	b := a
	b.Optional = true
	if a.digest() == b.digest() {
		t.Fatal("optional does not change the digest")
	}
}

// SPEC V0.next-step-chips (3): a new run drops the chips of the last
// message (the agent's thread actions and the optional requests that never
// ran); a blocking request and a request that ran stay.
func TestRunStartDropsStaleSteps(t *testing.T) {
	th := requestThread()
	th.Offers = []ActionSpec{
		{ID: "fix", Run: RunAgent, Prompt: "fix it", By: ByAgent},
		{ID: "old", Run: RunScript, Argv: []string{"make"}, By: ByAgent},
		{ID: "opt", Request: true, Optional: true, By: ByAgent},
		{ID: "opt-ran", Request: true, Optional: true, By: ByAgent},
		{ID: "block", Request: true, By: ByAgent},
		{ID: "fyr.merge", Run: RunFyr, By: ByObserved},
	}
	th.Runs = []ActionRun{{On: OnThread, Action: "opt-ran", OK: true}}
	RunStarted(th, at(2))
	var ids []string
	for _, a := range th.Offers {
		ids = append(ids, a.ID)
	}
	if got := ids; len(got) != 3 || got[0] != "opt-ran" || got[1] != "block" || got[2] != "fyr.merge" {
		t.Fatalf("after the run started: %v", got)
	}
}
