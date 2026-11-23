// Decision 0029 slice S3 (#187): a cost from learned raw measures, as the dominant share of the host's capacity over
// N; guard.EASY backfill with an aging cap.
package slots

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

func TestS3BackfillPassesTheHeadAtMostThreeTimesAndForTenMinutes(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	p := New(2)
	p.Now = func() time.Time { return now }
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		if len(held) == 0 {
			return true, "", ""
		}
		return len(head.Owner) < 3 || head.Owner[:3] != "big", "needs ≈ 75 % CPU, 50 % free", resource.ResCPU
	}
	p.Backfill = func(head, c resource.Holder, held []resource.Holder) bool {
		return len(c.Owner) > 4 && c.Owner[:5] == "small"
	}
	p.Acquire("a", resource.Test)
	p.Acquire("big", resource.Test)
	if held, _ := p.Acquire("small1", resource.Test); !held {
		t.Fatal("a cheap job behind the head was not backfilled")
	}
	p.Release("small1")
	if held, _ := p.Acquire("small2", resource.Test); !held {
		t.Fatal("second backfill")
	}
	p.Release("small2")
	if held, _ := p.Acquire("small3", resource.Test); !held {
		t.Fatal("third backfill")
	}
	p.Release("small3")
	if held, ahead := p.Acquire("small4", resource.Test); held || ahead != 1 {
		t.Fatal("the head was passed a fourth time")
	}
	if s := p.Snapshot(); s.Waiting[0].Owner != "big" {
		t.Fatalf("the head lost its place: %+v", s)
	}
	// A new head: its own count; but once it waited 10 min, no backfill.
	p.Release("big")
	p.Release("small4")
	p.Acquire("big2", resource.Test)
	now = now.Add(11 * time.Minute)
	if held, _ := p.Acquire("small5", resource.Test); held {
		t.Fatal("backfilled past a head that waited 10 min")
	}
}

func TestS3EASYPromise(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	end := now.Add(5 * time.Minute)
	for _, c := range []struct {
		name  string
		cheap bool
		wall  float64
		ends  []time.Time
		ok    bool
	}{
		{"cheap: always", true, 0, nil, true},
		{"ends before the earliest end", false, 120, []time.Time{end, end.Add(time.Hour)}, true},
		{"would end after it", false, 600, []time.Time{end}, false},
		{"no estimate of its own", false, 0, []time.Time{end}, false},
		{"a holder without an estimate", false, 60, []time.Time{end, {}}, false},
		{"nothing held", false, 60, nil, false},
	} {
		if got := guard.EASY(now, c.cheap, c.wall, c.ends); got != c.ok {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
