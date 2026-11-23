// SPEC V0.105: a record on the repo's default branch is done, at the next
// observation and in the load pass, so it neither stays on the board nor
// comes back.
package core

import "testing"

func TestRecordOnDefaultBranchIsDone(t *testing.T) {
	// A record from before V0.95 (4) on main, still in flight (live data,
	// 16 Nov: git:github.com/meros/fyr#main~2, base origin/HEAD).
	open := legacyRecord(t, `{"id":"c_m","key":"git:github.com/o/r#main~2","kind":"code","title":"main","branch":"main","phase":"working","phaseBy":"observed",
		"targets":["repo:github.com/o/r"],"workspace":{"repo":"github.com/o/r","path":"/w/r","branch":"main","base":"origin/HEAD","head":"h1"}}`)
	g := GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "main", Base: "origin/HEAD", Head: "h2", Ahead: 1, Unpushed: 1}
	rs, c := observe(t, []*Change{open}, g, ct0)
	if c == nil || !c.IsDone() || c.Done.Reason != DoneDefaultBranch {
		t.Fatalf("observing the default branch left its record open: %+v", c)
	}
	if _, again := observe(t, rs, g, ct0); again != nil {
		t.Fatalf("the default branch made a record again: %+v", again)
	}

	// The load pass does the same for a record nothing observes any more.
	stale := legacyRecord(t, `{"id":"c_s","key":"git:github.com/o/r#master","kind":"code","title":"master","branch":"master","phase":"working","phaseBy":"observed",
		"targets":["repo:github.com/o/r"],"workspace":{"repo":"github.com/o/r","path":"/w/r","branch":"master","base":"origin/HEAD"}}`)
	if !migrateRecordDone(stale, ct0) || !stale.IsDone() || stale.Done.Reason != DoneDefaultBranch {
		t.Fatalf("load pass left a default-branch record open: %+v", stale.Done)
	}
	// A feature branch in flight stays open.
	feat := legacyRecord(t, `{"id":"c_f","key":"git:github.com/o/r#perf/x","kind":"code","title":"perf/x","branch":"perf/x","phase":"working","phaseBy":"observed",
		"targets":["repo:github.com/o/r"],"workspace":{"repo":"github.com/o/r","path":"/w/r","branch":"perf/x","base":"origin/main"}}`)
	migrateRecordDone(feat, ct0)
	if feat.IsDone() {
		t.Fatalf("load pass closed a feature branch: %+v", feat.Done)
	}
}
