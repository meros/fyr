// Decision 0029 slice S3 (#187): a cost from learned raw measures, as the dominant share of the host's capacity over
// N; EASY backfill with an aging cap.
package slots

import (
	"testing"
	"time"
)

func TestS3CostIsTheDominantShareOfTheHost(t *testing.T) {
	const gb = 1 << 30
	for _, c := range []struct {
		name   string
		demand float64
		peak   uint64
		runs   int
		cpu    float64
		mem    uint64
		want   float64
	}{
		{"a 2-core job on 4 cores, N 2", 2, 1 * gb, 5, 4, 13 * gb, 1},
		{"memory dominates", 0.5, 13 * gb, 5, 4, 13 * gb, 2},
		{"a light job", 0.2, gb / 5, 5, 4, 13 * gb, 0.1},
		{"the same job on a 16-core host is cheap", 2, 1 * gb, 5, 16, 52 * gb, 0.25},
		{"under 3 runs never below the prior", 0.2, gb / 5, 2, 4, 13 * gb, 1},
		{"under 3 runs, above the prior counts", 4, 1 * gb, 1, 4, 13 * gb, 2},
		{"no capacity: the prior", 2, gb, 5, 0, 0, 1},
	} {
		if got := CostOf(c.demand, c.peak, c.runs, c.cpu, c.mem, 2); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestS3BackfillPassesTheHeadAtMostThreeTimesAndForTenMinutes(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	p := New(2)
	p.Now = func() time.Time { return now }
	p.Admit = func(head Holder, held []Holder) (bool, string) {
		if len(held) == 0 {
			return true, ""
		}
		return len(head.Owner) < 3 || head.Owner[:3] != "big", "1.0 of 2.0 in use; the next needs 1.5"
	}
	p.Backfill = func(head, c Holder, held []Holder) bool { return len(c.Owner) > 4 && c.Owner[:5] == "small" }
	p.Acquire("a", Test)
	p.Acquire("big", Test)
	if held, _ := p.Acquire("small1", Test); !held {
		t.Fatal("a cheap job behind the head was not backfilled")
	}
	p.Release("small1")
	if held, _ := p.Acquire("small2", Test); !held {
		t.Fatal("second backfill")
	}
	p.Release("small2")
	if held, _ := p.Acquire("small3", Test); !held {
		t.Fatal("third backfill")
	}
	p.Release("small3")
	if held, ahead := p.Acquire("small4", Test); held || ahead != 1 {
		t.Fatal("the head was passed a fourth time")
	}
	if s := p.Snapshot(); s.Waiting[0].Owner != "big" {
		t.Fatalf("the head lost its place: %+v", s)
	}
	// A new head: its own count; but once it waited 10 min, no backfill.
	p.Release("big")
	p.Release("small4")
	p.Acquire("big2", Test)
	now = now.Add(11 * time.Minute)
	if held, _ := p.Acquire("small5", Test); held {
		t.Fatal("backfilled past a head that waited 10 min")
	}
}

func TestS3EASYPromise(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	end := now.Add(5 * time.Minute)
	for _, c := range []struct {
		name string
		cost float64
		wall float64
		ends []time.Time
		ok   bool
	}{
		{"cheap: always", 0.2, 0, nil, true},
		{"ends before the earliest end", 1, 120, []time.Time{end, end.Add(time.Hour)}, true},
		{"would end after it", 1, 600, []time.Time{end}, false},
		{"no estimate of its own", 1, 0, []time.Time{end}, false},
		{"a holder without an estimate", 1, 60, []time.Time{end, {}}, false},
		{"nothing held", 1, 60, nil, false},
	} {
		if got := EASY(now, c.cost, c.wall, c.ends); got != c.ok {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
