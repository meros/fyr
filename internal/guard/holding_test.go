package guard

import "testing"

// V0.freeze-cap-holds-memory: a freeze of a small part, or work taking turns,
// does not fill a slot of the freeze limit.
func TestHoldingFreezesCountsOnlyWholeFreezesThatHoldMemory(t *testing.T) {
	fs := []Frozen{
		{Owner: "a", Mem: 0},                  // an esbuild of 0.0 GB
		{Owner: "b", Mem: 7 << 30, Active: 2}, // taking turns: a part runs
		{Owner: "c", Mem: 5 << 30},            // whole and heavy
		{Owner: "d", Mem: TurnMinRSS},         // just enough
		{Owner: "e", Mem: TurnMinRSS - 1},     // just under
	}
	if got := HoldingFreezes(fs); got != 2 {
		t.Errorf("HoldingFreezes = %d, want 2", got)
	}
	if got := HoldingFreezes(nil); got != 0 {
		t.Errorf("HoldingFreezes(nil) = %d, want 0", got)
	}
}
