package guard

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// A turn pays for the swap-in: turnFactor times the read-back time, within
// TurnMin and TurnMax (decision 0032 slice 2).
func TestT4ATurnLength(t *testing.T) {
	if got := turnLength(1<<30, 0); got != TurnMin {
		t.Fatalf("no rate: %s", got)
	}
	if got := turnLength(1<<30, 100<<20); got != 30720*time.Millisecond {
		t.Fatalf("1 GB at 100 MB/s: %s", got)
	}
	if got := turnLength(1<<20, 1<<30); got != TurnMin {
		t.Fatalf("floor: %s", got)
	}
	if got := turnLength(1<<40, 1<<20); got != TurnMax {
		t.Fatalf("ceiling: %s", got)
	}
}

func TestMultiSmallPartsOnlyStillTakeTurns(t *testing.T) {
	units := []resource.WorkUnit{{PID: 30, RSS: 1 << 20}, {PID: 31, RSS: 1 << 20}}
	if got := turnTakers(units); len(got) != 2 {
		t.Fatalf("no heavy part: all take turns, got %v", got)
	}
}
