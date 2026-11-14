package notify

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// SPEC 0.5 V0.20: which transitions push, and when.
func TestPlan(t *testing.T) {
	cases := []struct {
		prev, next string
		want       Plan
	}{
		{"working", "asking", Now},
		{"working", "failed", Now},
		{"working", "done", Later},
		{"seen", "blocked", Later},
		{"done", "seen", Never},
		{"asking", "working", Never},
		{"asking", "asking", Never},
		{"", "done", Never}, // a new thread or a restart: no push for old states
	}
	for _, c := range cases {
		if got := PlanFor(c.prev, c.next); got != c.want {
			t.Errorf("%s→%s: got %v want %v", c.prev, c.next, got, c.want)
		}
	}
}

// SPEC 0.5 V0.20: a thread is watched for 45 s after a presence beat.
func TestPresence(t *testing.T) {
	var p Presence
	p.Beat("dev1", "t1", t0)
	if !p.Watched("t1", t0.Add(44*time.Second)) || p.Watched("t1", t0.Add(46*time.Second)) || p.Watched("t2", t0) {
		t.Fatal("watch window wrong")
	}
	p.Beat("dev1", "", t0.Add(10*time.Second)) // stopped showing it
	if p.Watched("t1", t0.Add(11*time.Second)) {
		t.Fatal("still watched after the client stopped showing it")
	}
	p.Beat("dev1", "t1", t0)
	p.Beat("dev2", "t1", t0.Add(30*time.Second))
	p.Beat("dev1", "t2", t0.Add(31*time.Second))
	if !p.Watched("t1", t0.Add(60*time.Second)) {
		t.Fatal("another device still watches t1")
	}
}
