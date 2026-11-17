package core

import "testing"

// V0.164: the owner's commands exist and are owner-only; the event renders as a Next notice, never a wake.
func TestUndeclaredKindsAreOwnerOnly(t *testing.T) {
	for _, k := range []string{KindUndeclaredDismiss, KindHeavySuggestion, KindHeavyRemove, KindUndeclaredSettings} {
		ik := Kinds[k]
		if ik == nil || len(ik.From) != 1 || ik.From[0] != ByOwner {
			t.Errorf("%s is not owner-only: %+v", k, ik)
		}
	}
}

func TestUndeclaredNoticeIsNext(t *testing.T) {
	ns := slotNotices(Reduced{Events: []Event{{EventSlot, map[string]any{"undeclared": "This thread runs heavy work Fyr did not admit", "mem": 2.8e9, "cpu": 2.1}}}})
	if len(ns) != 1 || ns[0].Key != "heavy.undeclared" || ns[0].Urgency != Next {
		t.Fatalf("%+v", ns)
	}
}
