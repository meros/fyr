package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// oldestStore is a store in the oldest shape Open migrates (V0.versioned-store):
// written by a build of 16 to 23 Nov 2026, with no schema version, and with
// what the load passes of those builds repaired on every load still to do.
var oldestStore = map[string][][2]string{
	"threads": {
		{"t_old", `{"id":"t_old","title":"Old","cwd":"/w/app","createdAt":"2026-11-16T09:00:00Z","updatedAt":"2026-11-16T10:00:00Z",
			"changes":[
				{"id":"c_pr","key":"app:feat","kind":"code","title":"Feature","branch":"feat","targets":["repo:example.com/acme/app"],
				 "phase":"landed","phaseBy":"observed","updatedAt":"2026-11-16T10:00:00Z","landedAt":"2026-11-16T10:00:00Z",
				 "pr":{"url":"https://example.com/acme/app/pull/7","repo":"acme/app","number":7,"state":"MERGED","checks":"pass"}},
				{"id":"c_doc","key":"doc:notes","kind":"doc","title":"Notes","phase":"working","phaseBy":"agent","updatedAt":"2026-11-16T09:30:00Z"}],
			"rules":[{"id":"settle:done","purpose":"settle","when":{"all":[{"kind":"runEnded"},{"kind":"noDecisions"},{"kind":"idle"}]},
				"then":{"kind":"rule.settle"},"after":"disarm","createdBy":"owner","status":"armed","armedAt":"2026-11-16T09:00:00Z"}]}`},
		{"t_now", `{"id":"t_now","title":"Now","cwd":"/w/app","createdAt":"2026-11-22T09:00:00Z","updatedAt":"2026-11-22T09:00:00Z",
			"changes":[{"id":"c_open","key":"app:fix","kind":"code","title":"Fix","branch":"fix","phase":"working","phaseBy":"agent",
				"updatedAt":"2026-11-22T09:00:00Z","touchedAt":"2026-11-22T09:00:00Z"}]}`},
	},
	"views":    {{"owner/t_old", `{"user":"owner","entity":"t_old","seenAt":"2026-11-16T10:00:00Z"}`}},
	"projects": {{"p_app", `{"id":"p_app","name":"app","color":"teal","icon":{"kind":"monogram"},"folder":"/w/app"}`}},
	"changes": {
		{"r_landed", `{"id":"r_landed","key":"git:example.com/acme/app#old","kind":"code","title":"old","branch":"old","phase":"landed","phaseBy":"observed","updatedAt":"2026-11-16T11:00:00Z"}`},
		{"r_open", `{"id":"r_open","key":"git:example.com/acme/app#wip","kind":"code","title":"wip","branch":"wip","phase":"working","phaseBy":"observed","updatedAt":"2026-11-22T11:00:00Z","touchedAt":"2026-11-22T11:00:00Z"}`},
	},
}

// writeRaw makes a database at path with the schema and rows, as an older
// build left it: no migration runs.
func writeRaw(t *testing.T, path string, rows map[string][][2]string, kv map[string]string) {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	for table, rs := range rows {
		for _, r := range rs {
			q := `INSERT INTO ` + table + `(id,data) VALUES(?,?)`
			args := []any{r[0], r[1]}
			if table == "views" {
				user, entity, _ := strings.Cut(r[0], "/")
				q, args = `INSERT INTO views(user,entity,data) VALUES(?,?,?)`, []any{user, entity, r[1]}
			}
			if _, err := db.Exec(q, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	for k, v := range kv {
		if _, err := db.Exec(`INSERT INTO kv(k,v) VALUES(?,?)`, k, v); err != nil {
			t.Fatal(err)
		}
	}
}

// dump is every stored record and the schema version, to compare two opens.
func dump(t *testing.T, s *Store) string {
	t.Helper()
	var b strings.Builder
	for _, q := range []string{`SELECT id||' '||data FROM threads ORDER BY id`, `SELECT entity||' '||data FROM views ORDER BY entity`,
		`SELECT id||' '||data FROM projects ORDER BY id`, `SELECT id||' '||data FROM changes ORDER BY id`, `SELECT k||' '||v FROM kv ORDER BY k`} {
		if err := rowsOf(s.db, q, func(d string) error { b.WriteString(d + "\n"); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

func version(t *testing.T, s *Store) int {
	t.Helper()
	v, err := schemaVersion(s.db)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// V0.versioned-store: Open migrates a store of the oldest supported shape to
// the current one and records the version; a second Open changes nothing.
func TestOpenMigratesOldestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fyr.db")
	writeRaw(t, path, oldestStore, nil)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := version(t, s); v != SchemaVersion() {
		t.Fatalf("schema version %d, want %d", v, SchemaVersion())
	}
	ts, err := s.Threads()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*core.Thread{}
	for _, th := range ts {
		byID[th.ID] = th
	}
	old := byID["t_old"]
	pr, doc := core.FindChange(old, "c_pr"), core.FindChange(old, "c_doc")
	if pr.Done == nil || pr.Done.Reason != core.DoneBeforeV2 || pr.TouchedAt == nil {
		t.Fatalf("the landed change is not done: %+v", pr)
	}
	if doc.Phase != core.PhaseAbandoned || doc.Label != core.CodeOnlyReason || doc.Done == nil {
		t.Fatalf("the change that is not code did not end: %+v", doc)
	}
	if w, _ := json.Marshal(old.Rules); len(old.Rules) != 1 || strings.Contains(string(w), "noDecisions") || !strings.Contains(string(w), core.WhenWorkDone) {
		t.Fatalf("settle when done reads %s", w)
	}
	if c := core.FindChange(byID["t_now"], "c_open"); c.Done != nil || c.Phase != core.PhaseWorking {
		t.Fatalf("an open change moved: %+v", c)
	}
	rs, _ := s.Changes()
	for _, c := range rs {
		if (c.ID == "r_landed") != (c.Done != nil) || c.TouchedAt == nil {
			t.Fatalf("record %s: done %+v, touched %v", c.ID, c.Done, c.TouchedAt)
		}
	}
	first := dump(t, s)
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if again := dump(t, s); again != first {
		t.Fatalf("a second open changed the store:\n%s\nwas\n%s", again, first)
	}
}

// A new store is at the current version from its first open.
func TestOpenNewStoreIsCurrent(t *testing.T) {
	s, _ := open(t)
	if v := version(t, s); v != SchemaVersion() {
		t.Fatalf("schema version %d", v)
	}
}

// A store with no version that still holds a shape stored before
// OldestSupported is refused at open, by name, and left as it was.
func TestOpenRefusesOlderStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fyr.db")
	writeRaw(t, path, map[string][][2]string{
		"threads":  {{"t_33", `{"id":"t_33","title":"Before #33","settledAt":"2026-11-14T08:00:00Z"}`}},
		"projects": {{"p_55", `{"id":"p_55","name":"x","sources":[{"kind":"folder","path":"/w/x"}]}`}},
	}, nil)
	_, err := Open(path)
	if err == nil || !strings.Contains(err.Error(), OldestSupported) || !strings.Contains(err.Error(), "thread t_33 (settledAt)") || !strings.Contains(err.Error(), "project p_55 (sources)") {
		t.Fatalf("open: %v", err)
	}
	db, _ := sql.Open("sqlite", dsn(path))
	defer db.Close()
	if v, err := schemaVersion(db); err != nil || v != 0 {
		t.Fatalf("a refused store got version %d (%v)", v, err)
	}
}

// A migration that fails leaves the version it started from and nothing it
// wrote; the next open runs it again, and the one after does not.
func TestFailedMigrationKeepsVersion(t *testing.T) {
	saved := migrations
	t.Cleanup(func() { migrations = saved })
	runs := 0
	migrations = append(slices.Clone(saved), migration{"probe", func(tx *sql.Tx, _ time.Time) error {
		runs++
		if _, err := tx.Exec(`INSERT INTO kv(k,v) VALUES('probe',?)`, strconv.Itoa(runs)); err != nil {
			return err
		}
		if runs == 1 {
			return errors.New("crashed")
		}
		return nil
	}})
	path := filepath.Join(t.TempDir(), "fyr.db")
	writeRaw(t, path, oldestStore, nil)
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "crashed") {
		t.Fatalf("open: %v", err)
	}
	db, _ := sql.Open("sqlite", dsn(path))
	v, _ := schemaVersion(db)
	var probe string
	db.QueryRow(`SELECT v FROM kv WHERE k='probe'`).Scan(&probe)
	db.Close()
	if v != len(saved) || probe != "" {
		t.Fatalf("after the failed step: version %d, probe %q", v, probe)
	}
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if v := version(t, s); v != len(saved)+1 {
			t.Fatalf("version %d", v)
		}
		s.Close()
	}
	if runs != 2 {
		t.Fatalf("the step ran %d times, want 2", runs)
	}
}

// A store a newer build migrated opens as it is (a rollback to this build)
// and keeps its version.
func TestOpenNewerStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fyr.db")
	newer := strconv.Itoa(SchemaVersion() + 3)
	writeRaw(t, path, oldestStore, map[string]string{SchemaKey: newer})
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v := version(t, s); v != SchemaVersion()+3 {
		t.Fatalf("version %d, want %s", v, newer)
	}
	ts, _ := s.Threads()
	for _, th := range ts {
		if c := core.FindChange(th, "c_pr"); c != nil && c.Done != nil {
			t.Fatal("a migration ran on a newer store")
		}
	}
}

// V0.pr-twins-migration: a store already at version 1 (migrated before
// V0.thread-change-pr-twins) holding two changes on one PR heals on the next
// Open, once.
func TestOpenFoldsStoredPRTwinsAtVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fyr.db")
	change := func(id string) string {
		return `{"id":"` + id + `","key":"agent:` + id + `","kind":"code","title":"` + id + `","phase":"review","phaseBy":"agent",
			"updatedAt":"2026-11-27T09:00:00Z","touchedAt":"2026-11-27T09:00:00Z",
			"pr":{"url":"https://github.com/acme/app/pull/67","repo":"acme/app","number":67,"state":"OPEN"}}`
	}
	writeRaw(t, path, map[string][][2]string{"threads": {{"t_twin", `{"id":"t_twin","title":"Twin","cwd":"/w/app","createdAt":"2026-11-27T09:00:00Z","updatedAt":"2026-11-27T09:00:00Z",
		"changes":[` + change("a") + `,` + change("b") + `]}`}}}, map[string]string{SchemaKey: "1"})
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := version(t, s); v != SchemaVersion() {
		t.Fatalf("schema version %d, want %d", v, SchemaVersion())
	}
	ts, err := s.Threads()
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || len(ts[0].Changes) != 1 {
		t.Fatalf("twins not folded: %+v", ts)
	}
	if v := core.CheckThread(ts[0], nil, time.Now(), nil); slices.ContainsFunc(v, func(x core.Violation) bool { return x.ID == core.InvThreadChangeInvalid && strings.Contains(x.Evidence, "duplicate PR") }) {
		t.Fatalf("invariant still fires: %+v", v)
	}
}
