package core

import "testing"

// V0.thread-change-pr-twins: two changes never hold one PR, whoever made them.

func twinThread() *Thread {
	at := ct0
	mk := func(key string) Change {
		return Change{ID: ChangeID(key), Key: key, Kind: KindCode, Title: key, Phase: PhaseReview, PhaseBy: ByAgent, CreatedAt: at, UpdatedAt: at,
			PR: &PR{URL: ocPR, Repo: "meros/fyr", Number: 67, State: "OPEN"}, Evidence: []Evidence{{Kind: "pr", URL: ocPR}}}
	}
	return &Thread{ID: "t", Changes: []Change{mk("agent:a"), mk("agent:b")}}
}

func TestStoredAgentTwinsOnOnePRFoldOnLoad(t *testing.T) {
	th := twinThread()
	if !migrateDone(th, ct0) {
		t.Fatal("migration reported no change")
	}
	if len(th.Changes) != 1 || th.Changes[0].Key != "agent:a" {
		t.Fatalf("changes: %+v", th.Changes)
	}
	if v := checkChangesInvalid(th); v != "" {
		t.Fatalf("invariant: %s", v)
	}
}

func TestSecondAgentChangeOnOnePRFolds(t *testing.T) {
	th := &Thread{ID: "t"}
	th = ocOpen(t, th, ChangeOpen{Key: "agent:a", Kind: KindCode, Title: "a", Branch: "x"}, ocPR)
	th = ocOpen(t, th, ChangeOpen{Key: "agent:b", Kind: KindCode, Title: "b", Branch: "y"}, ocPR)
	if len(th.Changes) != 1 {
		t.Fatalf("changes: %+v", th.Changes)
	}
}
