package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/store"
)

// restartEngine closes st and opens the same database in a new engine, as a
// fyrd restart does.
func restartEngine(t *testing.T, dir string, st *store.Store) (*Engine, *store.Store) {
	t.Helper()
	st.Close()
	st2, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st2.Close() })
	e := New(st2)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	return e, st2
}

func openEngine(t *testing.T) (*Engine, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	return e, st, dir
}

// V0.persist-runtime-state: the runaway limits of thread_new and
// thread_send (V0.22) are not reset by a restart.
func TestThreadToolLimitsSurviveRestart(t *testing.T) {
	e, st, dir := openEngine(t)
	now := time.Now()
	for i := 0; i < 12; i++ {
		if !e.newLimit.allow("t_a", now) {
			t.Fatalf("new %d refused", i)
		}
	}
	for i := 0; i < 30; i++ {
		e.sendLimit.allow("t_a", now)
	}
	e2, _ := restartEngine(t, dir, st)
	if e2.newLimit.allow("t_a", now.Add(time.Minute)) {
		t.Error("thread_new limit reset by a restart")
	}
	if e2.sendLimit.allow("t_a", now.Add(time.Minute)) {
		t.Error("thread_send limit reset by a restart")
	}
	if !e2.newLimit.allow("t_a", now.Add(time.Hour+time.Second)) || !e2.newLimit.allow("t_b", now) {
		t.Error("an hour later, or another thread, is still refused")
	}
}

// V0.persist-runtime-state: an exhausted quota window's reset time and the
// resets already announced survive a restart, so the wake at the reset is
// neither lost nor sent twice (V0.quota-reset-wake).
func TestQuotaResetsSurviveRestart(t *testing.T) {
	e, st, dir := openEngine(t)
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(2 * time.Hour)
	e.mu.Lock()
	e.recordExhaustedQuotaLocked([]quota.Status{{Profile: "claude", Windows: []quota.Window{{Name: "5 h", UsedPct: 100, ResetsAt: &reset}}}}, now)
	e.quotaResetSent["t_a\x00claude\x005 h"] = now.Add(-time.Hour)
	e.saveQuotaResetsLocked(now)
	e.mu.Unlock()

	e2, st2 := restartEngine(t, dir, st)
	if got := e2.quotaWindows["claude\x005 h"]; !got.Equal(reset) {
		t.Fatalf("reset time after restart %v", got)
	}
	if got := e2.quotaResetSent["t_a\x00claude\x005 h"]; !got.Equal(now.Add(-time.Hour)) {
		t.Fatalf("sent mark after restart %v", got)
	}
	// Due: the window leaves the store with the next save.
	e2.mu.Lock()
	e2.dueQuotaResetsLocked(reset.Add(time.Second))
	e2.mu.Unlock()
	e3, _ := restartEngine(t, dir, st2)
	if _, ok := e3.quotaWindows["claude\x005 h"]; ok {
		t.Fatal("a reset that fired came back after a restart")
	}
}

// V0.persist-runtime-state: the rerun cap (decision 0036 (F)) counts kills
// from before a restart.
func TestRerunKillsSurviveRestart(t *testing.T) {
	e, st, dir := openEngine(t)
	now := time.Now()
	e.mu.Lock()
	e.gs.RerunKills = map[string][]time.Time{"make test": {now.Add(-time.Minute), now}, "old": {now.Add(-guard.RerunCapFor - time.Minute)}}
	e.keepRerunKills(e.gs.RerunKills)
	e.mu.Unlock()
	e2, st2 := restartEngine(t, dir, st)
	e2.UseSlots(slots.Open(2, st2)) // as fyrd wires it, after Load (V0.survive-update)
	if got := e2.gs.RerunKills["make test"]; len(got) != 2 {
		t.Fatalf("kills after restart %v", got)
	}
	if _, ok := e2.gs.RerunKills["old"]; ok {
		t.Fatal("kills older than RerunCapFor came back")
	}
}

// V0.survive-update: a hold told once that its job is heavier than predicted
// (decision 0029 S3) is not told again after a restart; a new hold is.
func TestSlotOverToldSurvivesRestart(t *testing.T) {
	told := 0
	over := func(string) (string, float64, float64, bool) { told++; return "build", 40, 20, true }
	e, st, dir := openEngine(t)
	e.UseSlots(slots.Open(2, st))
	e.SlotOver = over
	since := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	snap := resource.Snapshot{Held: []resource.Holder{{Owner: "t_a", Since: since}}}
	e.heavierThanPredicted(snap)
	if told != 1 {
		t.Fatalf("told %d times, want 1", told)
	}
	e2, st2 := restartEngine(t, dir, st)
	e2.UseSlots(slots.Open(2, st2))
	e2.SlotOver = over
	e2.heavierThanPredicted(snap)
	if told != 1 {
		t.Fatalf("the same hold was told again after a restart (%d)", told)
	}
	e2.heavierThanPredicted(resource.Snapshot{Held: []resource.Holder{{Owner: "t_a", Since: since.Add(time.Minute)}}})
	if told != 2 {
		t.Fatalf("a new hold was not told (%d)", told)
	}
}
