package notify

import (
	"testing"
	"time"
)

type mapKV map[string]string

func (m mapKV) GetKV(k string) (string, error) { return m[k], nil }
func (m mapKV) SetKV(k, v string) error        { m[k] = v; return nil }

// V0.push-summary-batch: what becomes of a due push.
func TestRouteFor(t *testing.T) {
	until := t0.Add(time.Hour)
	cases := []struct {
		state   string
		watched bool
		quiet   *time.Time
		away    bool
		want    Route
	}{
		{"done", false, nil, false, Send},
		{"done", true, nil, true, Drop},
		{"asking", false, &until, false, Hold},
		{"done", false, &until, true, Hold},
		{"done", false, nil, true, Batch},
		{"blocked", false, nil, true, Batch},
		{"asking", false, nil, true, Send}, // asking and failed still push at once
		{"failed", false, nil, true, Send},
	}
	for _, c := range cases {
		if got := RouteFor(c.state, c.watched, c.quiet, c.away, t0); got != c.want {
			t.Errorf("%+v: got %v want %v", c, got, c.want)
		}
	}
	if RouteFor("done", false, &until, false, until) != Send {
		t.Fatal("held at the window's end")
	}
}

// V0.push-summary-batch: the quiet hours' held pushes come out at the
// window's end, one group per host and project, and survive a restart.
func TestQuietHeldUntilTheEndAndAcrossARestart(t *testing.T) {
	kv := mapKV{}
	q := &Queue{KV: kv}
	end := t0.Add(2 * time.Hour)
	q.Hold(Held{Host: "a", Thread: "t1", Project: "p1", State: "done", At: t0, Until: end})
	q.Hold(Held{Host: "a", Thread: "t2", Project: "p1", State: "asking", At: t0, Until: end})
	q.Hold(Held{Host: "a", Thread: "t1", Project: "p1", State: "blocked", At: t0.Add(time.Minute), Until: end}) // newer, same thread
	q.Hold(Held{Host: "a", Thread: "t3", Project: "p2", State: "done", At: t0, Until: end.Add(time.Hour)})

	q = &Queue{KV: kv} // restart
	if err := q.Load(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if g, b, _ := q.Due(end.Add(-time.Second), t0); len(g) != 0 || len(b) != 0 {
		t.Fatalf("due inside the window: %v %v", g, b)
	}
	g, _, _ := q.Due(end, t0)
	if len(g) != 1 || len(g[0]) != 2 || g[0][0].State != "blocked" || g[0][1].Thread != "t2" {
		t.Fatalf("p1 at its end: %+v", g)
	}
	if g, _, _ := q.Due(end, t0); len(g) != 0 {
		t.Fatalf("sent twice: %+v", g)
	}
	q = &Queue{KV: kv} // what was sent is gone from the table too
	q.Load(end)
	if g, _, _ := q.Due(end.Add(time.Hour), t0); len(g) != 1 || g[0][0].Thread != "t3" {
		t.Fatalf("p2 at its end: %+v", g)
	}
	if kv[heldKey] != "" {
		t.Fatalf("table not emptied: %q", kv[heldKey])
	}
}

// V0.push-summary-batch: a held push older than HoldFor expires.
func TestHeldExpires(t *testing.T) {
	kv := mapKV{}
	q := &Queue{KV: kv}
	q.Hold(Held{Host: "a", Thread: "t1", State: "done", At: t0, Until: t0.Add(HoldFor + time.Hour)})
	q = &Queue{KV: kv}
	q.Load(t0.Add(HoldFor + time.Minute))
	if g, _, _ := q.Due(t0.Add(HoldFor+2*time.Hour), t0); len(g) != 0 {
		t.Fatalf("an expired hold came out: %+v", g)
	}
}

// V0.push-summary-batch: the away batch goes out BatchEvery after its oldest
// push, and a beat after a push drops it.
func TestBatch(t *testing.T) {
	var q Queue
	seen := t0.Add(-AwayAfter)
	q.Batch(Held{Host: "a", Thread: "t1", State: "done", At: t0})
	q.Batch(Held{Host: "a", Thread: "t2", State: "blocked", At: t0.Add(2 * time.Minute)})
	if _, b, _ := q.Due(t0.Add(BatchEvery-time.Second), seen); b != nil {
		t.Fatalf("batch before BatchEvery: %+v", b)
	}
	_, b, _ := q.Due(t0.Add(BatchEvery), seen)
	if len(b) != 2 {
		t.Fatalf("batch: %+v", b)
	}
	if _, b, _ := q.Due(t0.Add(2*BatchEvery), seen); b != nil {
		t.Fatalf("sent twice: %+v", b)
	}

	q.Batch(Held{Host: "a", Thread: "t1", State: "done", At: t0.Add(10 * time.Minute)})
	q.Batch(Held{Host: "a", Thread: "t3", State: "done", At: t0.Add(12 * time.Minute)})
	seen = t0.Add(11 * time.Minute) // the owner came back after t1
	_, b, _ = q.Due(t0.Add(30*time.Minute), seen)
	if len(b) != 1 || b[0].Thread != "t3" {
		t.Fatalf("the batch after a beat: %+v", b)
	}
}

func TestCounts(t *testing.T) {
	if got := Counts([]string{"done", "asking", "done", "done", "seen"}); got != "1 asking, 3 done" {
		t.Fatal(got)
	}
	if got := Counts([]string{"done", "blocked", "done", "done"}); got != "1 blocked, 3 done" {
		t.Fatal(got)
	}
	if Counts(nil) != "" || !Summarised("asking", true) || Summarised("asking", false) || Summarised("seen", true) {
		t.Fatal("summarised wrong")
	}
}

// V0.push-summary-batch: away is AwayAfter without a beat of any device; the
// start counts as one.
func TestAway(t *testing.T) {
	var p Presence
	p.Seen(t0)
	if p.Away(t0.Add(AwayAfter-time.Second)) || !p.Away(t0.Add(AwayAfter)) {
		t.Fatal("away after the start wrong")
	}
	p.Beat("dev1", "", t0.Add(5*time.Minute)) // the page went hidden: the owner was there
	if p.Away(t0.Add(AwayAfter)) || !p.Away(t0.Add(5*time.Minute+AwayAfter)) {
		t.Fatal("away after a beat wrong")
	}
}
