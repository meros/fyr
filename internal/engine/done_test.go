// SPEC V0.98: done changes on the host: Done on a record, the board visit, the load pass.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// SPEC V0.98 (4): Done on a host record the board shows goes down the
// record's path, with a receipt; the threads that show it see it done.
func TestOwnerDoneOnRecordThroughCommand(t *testing.T) {
	e, st, _ := bare(t)
	e.Git = &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	dir := t.TempDir()
	a := threadIn(t, e, dir)
	e.observeGit(a, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"})
	cs := shownIn(e, a)
	if len(cs) != 1 {
		t.Fatalf("setup: %+v", cs)
	}
	id := cs[0].ID
	r := e.Command(api.Cmd{CommandID: "done-1", Action: core.KindChangeDone, ThreadID: a, Args: core.Args{"change": id}})
	if !r.OK {
		t.Fatalf("change.done on a record: %+v", r)
	}
	if c := e.records[id]; c == nil || !c.IsDone() || c.Done.By != core.ByOwner {
		t.Fatalf("record after Done: %+v", c)
	}
	if cs := shownIn(e, a); !cs[0].IsDone() {
		t.Fatalf("the thread shows the record open: %+v", cs[0])
	}
	if rs, _ := st.Changes(); len(rs) != 1 || !rs[0].IsDone() {
		t.Fatal("the mark is not stored")
	}
	if r2 := e.Command(api.Cmd{CommandID: "done-1", Action: core.KindChangeDone, ThreadID: a, Args: core.Args{"change": id}}); !r2.OK {
		t.Fatalf("the receipt does not replay: %+v", r2)
	}
}

// SPEC V0.98 (3): opening the board is a visit: the host returns the time of
// the visit before and stores this one.
func TestBoardVisit(t *testing.T) {
	e, _, now := bare(t)
	if prev := e.BoardVisit(); !prev.IsZero() {
		t.Fatalf("first visit: %v", prev)
	}
	first := *now
	*now = now.Add(time.Hour)
	if prev := e.BoardVisit(); !prev.Equal(first) {
		t.Fatalf("second visit: %v, want %v", prev, first)
	}
}

// SPEC V0.98 (7): the load pass marks landed changes and landed records done.
func TestLoadMarksLandedDone(t *testing.T) {
	_, st, _ := bare(t)
	th := &core.Thread{ID: "t_old", Cwd: "/w", Changes: []core.Change{
		{ID: "c1", Key: "doc", Kind: core.KindDoc, Title: "Doc", Phase: core.PhaseLanded, PhaseBy: core.ByAgent},
		{ID: "c2", Key: "wip", Kind: core.KindCode, Title: "Wip", Phase: core.PhaseWorking, PhaseBy: core.ByAgent},
	}}
	rec := &core.Change{ID: "r1", Key: "git:github.com/o/r#b", Kind: core.KindCode, Title: "b", Branch: "b", Phase: core.PhaseLanded, PhaseBy: core.ByObserved, UpdatedAt: time.Now()}
	if _, err := st.Write(func(tx *store.Tx) error {
		if err := tx.PutChange(rec); err != nil {
			return err
		}
		return tx.PutThread(th)
	}); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	got := e.threads["t_old"]
	if c := core.FindChange(got, "c1"); !c.IsDone() || c.Done.Reason != core.DoneBeforeV2 {
		t.Fatalf("landed doc: %+v", c.Done)
	}
	if c := core.FindChange(got, "c2"); c.IsDone() {
		t.Fatal("working change marked done")
	}
	if !e.records["r1"].IsDone() {
		t.Fatal("landed record not done")
	}
	ts, _ := st.Threads()
	for _, x := range ts {
		if x.ID == "t_old" && !core.FindChange(x, "c1").IsDone() {
			t.Fatal("the pass is not stored")
		}
	}
}
