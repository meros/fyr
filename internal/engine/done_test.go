// SPEC V0.98: done changes on the host: Done on a record, the board visit, the load pass.
package engine

import (
	"database/sql"
	"path/filepath"
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

// SPEC V0.98 (7): the load pass marks landed changes and landed records
// done (once, when store.Open migrates a store: V0.versioned-store).
func TestLoadMarksLandedDone(t *testing.T) {
	th := &core.Thread{ID: "t_old", Cwd: "/w", Changes: []core.Change{
		{ID: "c1", Key: "doc", Kind: core.KindDoc, Title: "Doc", Phase: core.PhaseLanded, PhaseBy: core.ByAgent},
		{ID: "c2", Key: "wip", Kind: core.KindCode, Title: "Wip", Phase: core.PhaseWorking, PhaseBy: core.ByAgent},
	}}
	rec := &core.Change{ID: "r1", Key: "git:github.com/o/r#b", Kind: core.KindCode, Title: "b", Branch: "b", Phase: core.PhaseLanded, PhaseBy: core.ByObserved, UpdatedAt: time.Now()}
	st := unversioned(t, func(tx *store.Tx) error {
		if err := tx.PutChange(rec); err != nil {
			return err
		}
		return tx.PutThread(th)
	})
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

// V0.review-correctness (3): the owner's Done on a host record sent twice at once
// (a retry while the first is in flight) applies once: the record's reduce
// and the receipt are one transaction under the engine's lock, so every
// send gets the first one's reply and the record logs one mark.
func TestOwnerDoneOnRecordDoubleSend(t *testing.T) {
	e, st, _ := bare(t)
	e.Git = &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	dir := t.TempDir()
	a := threadIn(t, e, dir)
	e.observeGit(a, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"})
	id := shownIn(e, a)[0].ID
	before, _ := st.EventsFor(id, 0, 1000, false)
	const n = 8
	replies := make(chan api.Reply, n)
	for range n {
		go func() {
			replies <- e.Command(api.Cmd{CommandID: "end-1", Action: core.KindChangeEnd, ThreadID: a, Args: core.Args{"change": id, "reason": "dead"}})
		}()
	}
	for range n {
		if r := <-replies; !r.OK {
			t.Fatalf("a send of the same command failed: %+v", r)
		}
	}
	after, _ := st.EventsFor(id, 0, 1000, false)
	if got := len(after) - len(before); got != 1 {
		t.Fatalf("the record logged %d events for one command: %+v", got, after[len(before):])
	}
	if r, _ := st.Receipt("end-1"); r == nil {
		t.Fatal("no receipt")
	}
}

// V0.review-correctness (3): a host that stops after the record's reduce never has
// the record changed without the receipt: they are one transaction. The
// receipt's insert fails here (a trigger stands in for the crash): the mark
// is not stored either, and the same command sent again applies once.
func TestOwnerDoneOnRecordReceiptWithTheRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fyr.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	e.Git = &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	dir := t.TempDir()
	a := threadIn(t, e, dir)
	e.observeGit(a, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"})
	id := shownIn(e, a)[0].ID
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER crash BEFORE INSERT ON receipts BEGIN SELECT RAISE(ABORT, 'crash'); END`); err != nil {
		t.Fatal(err)
	}
	stored := func() bool {
		rs, _ := st.Changes()
		for _, c := range rs {
			if c.ID == id {
				return c.IsDone()
			}
		}
		t.Fatal("record not stored")
		return false
	}
	e.Command(api.Cmd{CommandID: "done-x", Action: core.KindChangeDone, ThreadID: a, Args: core.Args{"change": id}})
	if r, _ := st.Receipt("done-x"); r == nil && (stored() || e.records[id].IsDone()) {
		t.Fatal("the record is done without the command's receipt")
	}
	if _, err := db.Exec(`DROP TRIGGER crash`); err != nil {
		t.Fatal(err)
	}
	before, _ := st.EventsFor(id, 0, 1000, false)
	if r := e.Command(api.Cmd{CommandID: "done-x", Action: core.KindChangeDone, ThreadID: a, Args: core.Args{"change": id}}); !r.OK {
		t.Fatalf("the resend: %+v", r)
	}
	if !stored() || !e.records[id].IsDone() {
		t.Fatal("the resend did not mark the record done")
	}
	if r, _ := st.Receipt("done-x"); r == nil {
		t.Fatal("no receipt after the resend")
	}
	after, _ := st.EventsFor(id, 0, 1000, false)
	if got := len(after) - len(before); got != 1 {
		t.Fatalf("the resend logged %d events", got)
	}
}
