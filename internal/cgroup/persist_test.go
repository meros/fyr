package cgroup

import (
	"testing"
	"time"
)

// V0.persist-runtime-state: when a leftover was first seen and since when a
// thread is idle survive a restart, so a restart does not start the grace
// and the idle limit again.
func TestLeftoverTimersSurviveRestart(t *testing.T) {
	now := time.Now()
	db := kv{}
	l := &Leftovers{KV: db, Now: func() time.Time { return now }}
	l.loadLocked()
	l.seen = map[string]time.Time{"cg/1": now.Add(-10 * time.Minute)}
	l.idleSince = map[string]time.Time{"t_a": now.Add(-5 * time.Minute), "t_old": now.Add(-leftoverTimerTTL - time.Minute)}
	l.saveTimersLocked()

	l2 := &Leftovers{KV: db, Now: func() time.Time { return now }}
	l2.loadLocked()
	if !l2.seen["cg/1"].Equal(now.Add(-10*time.Minute)) || !l2.idleSince["t_a"].Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("after restart: seen %v idle %v", l2.seen, l2.idleSince)
	}
	if _, ok := l2.idleSince["t_old"]; ok {
		t.Fatal("a stamp older than a day came back")
	}
}

// V0.persist-runtime-state: a repository's hourly PR-head fetch is not
// repeated because fyrd restarted.
func TestPullFetchTimesSurviveRestart(t *testing.T) {
	now := time.Now()
	db := &memKV{}
	tp := &Temps{KV: db, Now: func() time.Time { return now }}
	tp.loadFetchedLocked(now)
	tp.fetched = map[string]time.Time{"/src/acme/.git": now.Add(-10 * time.Minute), "/src/old/.git": now.Add(-2 * pullFetchEvery)}
	tp.saveFetchedLocked(now)

	tp2 := &Temps{KV: db, Now: func() time.Time { return now }}
	tp2.loadFetchedLocked(now)
	if !tp2.fetched["/src/acme/.git"].Equal(now.Add(-10 * time.Minute)) {
		t.Fatalf("after restart %v", tp2.fetched)
	}
	if _, ok := tp2.fetched["/src/old/.git"]; ok {
		t.Fatal("a fetch older than pullFetchEvery came back")
	}
}

// V0.survive-update: a thread told about its compose containers is not told
// again because fyrd restarted, only after serviceNoticeEvery.
func TestServiceNoticeSurvivesRestart(t *testing.T) {
	now := time.Now()
	db := kv{}
	c := Capacity{Memory: 10 << 30}
	open := func() *Services {
		s := &Services{KV: db, Now: func() time.Time { return now }}
		s.pub.Store(&map[string][]Service{"t_a": {{Name: "compose acme", Project: "acme", Containers: 4 << 30}}})
		return s
	}
	if open().Notice("t_a", c) == "" {
		t.Fatal("no notice for 4 GiB of containers")
	}
	if got := open().Notice("t_a", c); got != "" {
		t.Fatalf("told again after a restart: %q", got)
	}
	now = now.Add(serviceNoticeEvery)
	if open().Notice("t_a", c) == "" {
		t.Fatal("not told again after serviceNoticeEvery")
	}
}
