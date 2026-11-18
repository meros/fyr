package core

import "testing"

// V0.164: the owner's commands exist and are owner-only.
func TestUndeclaredKindsAreOwnerOnly(t *testing.T) {
	for _, k := range []string{KindHeavyRemove, KindUndeclaredSettings} {
		ik := Kinds[k]
		if ik == nil || len(ik.From) != 1 || ik.From[0] != ByOwner {
			t.Errorf("%s is not owner-only: %+v", k, ik)
		}
	}
}
