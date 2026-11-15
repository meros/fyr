package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fyr.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

var at = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

// SPEC I7 (#36): a command id has one receipt; the stored result comes back
// as written, and a second receipt for the same id fails its write.
func TestReceipts(t *testing.T) {
	s, _ := open(t)
	if r, err := s.Receipt("c1"); r != nil || err != nil {
		t.Fatalf("new id: %s %v", r, err)
	}
	if _, err := s.Write(func(tx *Tx) error { return tx.PutReceipt("c1", map[string]any{"ok": true, "threadId": "t_1"}, at) }); err != nil {
		t.Fatal(err)
	}
	r, err := s.Receipt("c1")
	var got struct {
		OK       bool   `json:"ok"`
		ThreadID string `json:"threadId"`
	}
	if err != nil || json.Unmarshal(r, &got) != nil || !got.OK || got.ThreadID != "t_1" {
		t.Fatalf("receipt: %s %v", r, err)
	}
	if _, err := s.Write(func(tx *Tx) error { return tx.PutReceipt("c1", map[string]any{"ok": false}, at) }); err == nil {
		t.Fatal("a second receipt for c1 was written")
	}
	if r, _ := s.Receipt("c1"); string(r) != `{"ok":true,"threadId":"t_1"}` {
		t.Fatalf("the first receipt changed: %s", r)
	}
}

// #36: a write is atomic: an error leaves no thread, event or receipt.
func TestWriteRollsBack(t *testing.T) {
	s, _ := open(t)
	_, err := s.Write(func(tx *Tx) error {
		tx.PutThread(&core.Thread{ID: "t_1"})
		tx.Append("t_1", "command", map[string]any{}, at)
		tx.PutReceipt("c1", map[string]any{"ok": true}, at)
		return errors.New("no")
	})
	if err == nil {
		t.Fatal("the error was lost")
	}
	ts, _ := s.Threads()
	evs, _ := s.EventsFor("t_1", 0, 10, false)
	r, _ := s.Receipt("c1")
	if len(ts) != 0 || len(evs) != 0 || r != nil {
		t.Fatalf("a failed write left %d threads, %d events, receipt %s", len(ts), len(evs), r)
	}
}

// #36: thread, view and project projections are replaced by id and read
// back after the database is opened again.
func TestProjections(t *testing.T) {
	s, path := open(t)
	put := func(fn func(*Tx) error) {
		t.Helper()
		if _, err := s.Write(fn); err != nil {
			t.Fatal(err)
		}
	}
	put(func(tx *Tx) error { return tx.PutThread(&core.Thread{ID: "t_1", Title: "first"}) })
	put(func(tx *Tx) error { return tx.PutThread(&core.Thread{ID: "t_1", Title: "second"}) })
	put(func(tx *Tx) error { return tx.PutThread(&core.Thread{ID: "t_2", Title: "other"}) })
	put(func(tx *Tx) error { return tx.PutView(&core.View{User: core.Owner, Entity: "t_1", SeenAt: at}) })
	put(func(tx *Tx) error { return tx.PutView(&core.View{User: "someone", Entity: "t_1"}) })
	put(func(tx *Tx) error { return tx.PutProject(&core.Project{ID: "p_1", Name: "Lasso"}) })
	put(func(tx *Tx) error { return tx.PutProject(&core.Project{ID: "p_2", Name: "Gone"}) })
	put(func(tx *Tx) error { return tx.DeleteProject("p_2") })
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts, _ := s.Threads()
	titles := map[string]string{}
	for _, x := range ts {
		titles[x.ID] = x.Title
	}
	if len(ts) != 2 || titles["t_1"] != "second" || titles["t_2"] != "other" {
		t.Fatalf("threads: %v", titles)
	}
	vs, _ := s.Views(core.Owner)
	if len(vs) != 1 || !vs["t_1"].SeenAt.Equal(at) {
		t.Fatalf("views: %+v", vs)
	}
	ps, _ := s.Projects()
	if len(ps) != 1 || ps[0].Name != "Lasso" {
		t.Fatalf("projects: %+v", ps)
	}
}

// #36: events of a subject come back in order, from the start, the tail or
// before a seq; Write returns what it appended.
func TestEvents(t *testing.T) {
	s, _ := open(t)
	var seqs []int64
	for i := range 5 {
		evs, err := s.Write(func(tx *Tx) error {
			tx.Append("other", "x", map[string]any{}, at)
			return tx.Append("t_1", "n", map[string]any{"i": i}, at.Add(time.Duration(i)*time.Second))
		})
		if err != nil || len(evs) != 2 || evs[1].Subject != "t_1" {
			t.Fatalf("write: %+v %v", evs, err)
		}
		seqs = append(seqs, evs[1].Seq)
	}
	first, _ := s.EventsFor("t_1", 0, 2, false)
	tail, _ := s.EventsFor("t_1", 0, 2, true)
	after, _ := s.EventsFor("t_1", seqs[2], 10, false)
	before, _ := s.EventsBefore("t_1", seqs[3], 2)
	check := func(name string, got []Event, want ...int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: %d events", name, len(got))
		}
		for i := range got {
			if got[i].Seq != want[i] || got[i].Subject != "t_1" {
				t.Fatalf("%s: %+v", name, got)
			}
		}
	}
	check("first", first, seqs[0], seqs[1])
	check("tail", tail, seqs[3], seqs[4])
	check("after", after, seqs[3], seqs[4])
	check("before", before, seqs[1], seqs[2])
	if !tail[1].At.Equal(at.Add(4*time.Second)) || string(tail[1].Data) != `{"i":4}` {
		t.Fatalf("event: %+v", tail[1])
	}
}

// #31: a thread stored before the one Actions component loads with its
// actions and runs: the thread's runs from offerRuns, the view's actions and
// runs moved to the live view, and a change's actions from "actions".
func TestLoadThreadBeforeActions(t *testing.T) {
	s, _ := open(t)
	old := `{"id":"t_old","title":"Old","liveView":true,
	"offers":[{"id":"e2e","on":"thread","label":"Run e2e","run":"script","argv":["make","e2e"],"by":"agent","digest":"d1"},
	          {"id":"ok","on":"view","label":"Looks good","run":"agent","prompt":"go on","in":"this","by":"agent","digest":"d2"}],
	"offerRuns":[{"action":"e2e","label":"Run e2e","by":"owner","at":"2026-11-14T10:00:00Z","ok":true},
	             {"on":"view","action":"ok","label":"Looks good","by":"owner","at":"2026-11-14T10:01:00Z","ok":true}],
	"changes":[{"id":"c_1","key":"tf:prod","kind":"infra","title":"Prod","phase":"review","phaseBy":"agent",
	  "actions":[{"id":"apply","label":"Apply","run":"script","argv":["terraform","apply"],"by":"agent","digest":"d3"}],
	  "runs":[{"action":"apply","label":"Apply","by":"owner","at":"2026-11-14T09:00:00Z","ok":true}]}]}`
	if _, err := s.db.Exec(`INSERT INTO threads(id,data) VALUES(?,?)`, "t_old", old); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Threads()
	if err != nil || len(ts) != 1 {
		t.Fatalf("load: %v %d", err, len(ts))
	}
	th := ts[0]
	if len(th.Offers) != 1 || th.Offers[0].ID != "e2e" || th.Offers[0].Digest != "d1" || len(th.Runs) != 1 || th.Runs[0].Action != "e2e" {
		t.Fatalf("thread actions: %+v %+v", th.Offers, th.Runs)
	}
	if th.Live == nil || len(th.Live.Offers) != 1 || th.Live.Offers[0].ID != "ok" || len(th.Live.Runs) != 1 || th.Live.Runs[0].Action != "ok" {
		t.Fatalf("view actions: %+v", th.Live)
	}
	c := th.Changes[0]
	if len(c.Offers) != 1 || c.Offers[0].ID != "apply" || c.Offers[0].Digest != "d3" || len(c.Runs) != 1 {
		t.Fatalf("change actions: %+v", c.Actions)
	}
	// Saved again, it is in the new shape and loads the same.
	if _, err := s.Write(func(tx *Tx) error { return tx.PutThread(th) }); err != nil {
		t.Fatal(err)
	}
	var raw string
	s.db.QueryRow(`SELECT data FROM threads WHERE id='t_old'`).Scan(&raw)
	ts, _ = s.Threads()
	if strings.Contains(raw, "offerRuns") || strings.Contains(raw, `"actions"`) || len(ts[0].Live.Offers) != 1 || len(ts[0].Changes[0].Offers) != 1 {
		t.Fatalf("saved again: %s", raw)
	}
}
