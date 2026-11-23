package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
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

// SPEC V0.remove-t3 (#68): a thread stored with t3Id and source loads without
// error, keeps its thread data and Claude session, and saves without those fields.
func TestStoreDropsT3FieldsOnSave(t *testing.T) {
	s, _ := open(t)
	old := `{"id":"t_old","title":"From T3","cwd":"/src/app","profile":"claudeAlt","sessionId":"sess-42","source":"t3","t3Id":"t3_legacy"}`
	if _, err := s.db.Exec(`INSERT INTO threads(id,data) VALUES('t_old',?)`, old); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Threads()
	if err != nil {
		t.Fatalf("load old thread: %v", err)
	}
	if len(ts) != 1 || ts[0].ID != "t_old" || ts[0].Title != "From T3" || ts[0].SessionID != "sess-42" {
		t.Fatalf("unexpected thread: %+v", ts)
	}
	if _, err := s.Write(func(tx *Tx) error { return tx.PutThread(ts[0]) }); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRow(`SELECT data FROM threads WHERE id='t_old'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"t3Id", "source"} {
		if _, ok := top[k]; ok {
			t.Fatalf("saved again with %q: %s", k, raw)
		}
	}
}
