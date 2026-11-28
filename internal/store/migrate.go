package store

// The store's schema version (V0.versioned-store): the key-value table holds
// the number of migrations a store has run, and Open runs the ones after it
// in order, each once. A migration and the version it reaches commit in one
// transaction, so a migration that fails leaves the version it started from
// and the next Open runs it again.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
)

// SchemaKey is the key of the schema version in the key-value table.
const SchemaKey = "store.schema"

// OldestSupported names the oldest build whose store Open migrates. A store
// that no build has versioned yet and that still holds a shape stored
// before it is refused (checkOldShapes).
const OldestSupported = "a build of 16 Nov 2026 (V0.100)"

// migration is one step of the schema: run changes the records in tx and
// stores what it changed. It must be safe to run on a store it already ran
// on, which happens only when the commit of its version failed.
type migration struct {
	name string
	run  func(tx *sql.Tx, now time.Time) error
}

// migrations is the schema, in order; a store at version n has run the
// first n. A new step goes at the end, and a step is never changed or
// removed while a store may still be below it.
var migrations = []migration{
	{"the load passes of V0.98 (7), V0.105 (2), V0.changes-code-only (4) and V0.structure-cost (2)", loadPasses},
	{"two changes on one PR fold into one (V0.thread-change-pr-twins)", foldPRTwins},
}

// SchemaVersion is the version Open brings a store to.
func SchemaVersion() int { return len(migrations) }

// migrate brings the store to SchemaVersion. A store without a version was
// written before V0.versioned-store (or is new): it is at version 0, once
// checkOldShapes finds no shape older than OldestSupported in it.
func (s *Store) migrate(now time.Time) error {
	v, err := schemaVersion(s.db)
	if err != nil {
		return err
	}
	if v > len(migrations) {
		// A newer build migrated this store and this one is older, as after
		// a rollback (deploy's launcher). Every step so far adds or repairs
		// records in the shape this build reads, so it opens as it is.
		log.Printf("store: schema %d is newer than this build's %d; opening it as it is", v, len(migrations))
		return nil
	}
	if v == 0 {
		if err := checkOldShapes(s.db); err != nil {
			return err
		}
	}
	for i := v; i < len(migrations); i++ {
		if err := s.step(i, now); err != nil {
			return fmt.Errorf("store: migration %d (%s): %w", i+1, migrations[i].name, err)
		}
	}
	return nil
}

// step runs migration i and records version i+1 in one transaction. It
// reads the version again inside it, so a second process opening the same
// file does not run the step twice.
func (s *Store) step(i int, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op after Commit
	v, err := schemaVersion(tx)
	if err != nil {
		return err
	}
	if v > i {
		return nil
	}
	if err := migrations[i].run(tx, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, SchemaKey, strconv.Itoa(i+1)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("store: schema %d: %s", i+1, migrations[i].name)
	return nil
}

type querier interface {
	Query(q string, args ...any) (*sql.Rows, error)
	QueryRow(q string, args ...any) *sql.Row
}

func schemaVersion(q querier) (int, error) {
	var raw string
	err := q.QueryRow(`SELECT v FROM kv WHERE k=?`, SchemaKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("store: schema version %q is not a number", raw)
	}
	return v, nil
}

// rowsOf calls fn with each row's data column of the query.
func rowsOf(q querier, query string, fn func(data string) error) error {
	rows, err := q.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return err
		}
		if err := fn(d); err != nil {
			return err
		}
	}
	return rows.Err()
}

// loadPasses is migration 1: the passes the engine ran on every load until
// V0.versioned-store (core.Migrate, core.MigrateRecord), now once.
func loadPasses(sqlTx *sql.Tx, now time.Time) error {
	tx := &Tx{tx: sqlTx}
	var ts []*core.Thread
	err := rowsOf(sqlTx, `SELECT data FROM threads`, func(d string) error {
		t := &core.Thread{}
		if err := json.Unmarshal([]byte(d), t); err != nil {
			return err
		}
		ts = append(ts, t)
		return nil
	})
	if err != nil {
		return err
	}
	var rs []*core.Change
	err = rowsOf(sqlTx, `SELECT data FROM changes`, func(d string) error {
		c := &core.Change{}
		if err := json.Unmarshal([]byte(d), c); err != nil {
			return err
		}
		rs = append(rs, c)
		return nil
	})
	if err != nil {
		return err
	}
	for _, t := range ts {
		if core.Migrate(t, now) {
			if err := tx.PutThread(t); err != nil {
				return err
			}
		}
	}
	for _, c := range rs {
		if core.MigrateRecord(c, now) {
			if err := tx.PutChange(c); err != nil {
				return err
			}
		}
	}
	return nil
}

// foldPRTwins is migration 2: V0.thread-change-pr-twins put its repair in
// the load passes, which a store already at version 1 never ran again.
func foldPRTwins(sqlTx *sql.Tx, now time.Time) error {
	tx := &Tx{tx: sqlTx}
	var ts []*core.Thread
	err := rowsOf(sqlTx, `SELECT data FROM threads`, func(d string) error {
		t := &core.Thread{}
		if err := json.Unmarshal([]byte(d), t); err != nil {
			return err
		}
		ts = append(ts, t)
		return nil
	})
	if err != nil {
		return err
	}
	for _, t := range ts {
		if core.FoldSamePR(t, now) {
			if err := tx.PutThread(t); err != nil {
				return err
			}
		}
	}
	return nil
}

// The keys only a build before OldestSupported wrote, which no build reads
// now: on a thread (#31, #32, #33), on a thread's change (#31, #46), under
// a thread's settle (#46), on a view (#46) and on a project (V0.28, V0.55).
var (
	oldThreadKeys  = []string{"offerRuns", "pr", "dismissedPRs", "settleWhenMerged", "settledAt", "settleWhenDone", "settleWhen", "liveView"}
	oldChangeKeys  = []string{"actions", "deployWatch"}
	oldSettleKeys  = []string{"when"}
	oldViewKeys    = []string{"snoozeUntil", "snoozedAt"}
	oldProjectKeys = []string{"sources", "auto"}
)

// checkOldShapes refuses a store that still holds a record in a shape
// stored before OldestSupported: this build would read it without that
// part, silently. It names the records and what to do.
func checkOldShapes(q querier) error {
	var found []string
	note := func(kind, id string, keys []string) {
		if len(keys) > 0 && len(found) < 10 {
			found = append(found, fmt.Sprintf("%s %s (%s)", kind, id, strings.Join(keys, ", ")))
		}
	}
	err := rowsOf(q, `SELECT data FROM threads`, func(d string) error {
		var t struct {
			ID      string                     `json:"id"`
			Settle  map[string]json.RawMessage `json:"settle"`
			Changes []map[string]json.RawMessage
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal([]byte(d), &top); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(d), &t); err != nil {
			return err
		}
		keys := has(top, oldThreadKeys)
		keys = append(keys, prefixed("settle.", has(t.Settle, oldSettleKeys))...)
		for _, c := range t.Changes {
			keys = append(keys, prefixed("changes[].", has(c, oldChangeKeys))...)
		}
		note("thread", t.ID, keys)
		return nil
	})
	if err != nil {
		return err
	}
	err = rowsOf(q, `SELECT data FROM views`, func(d string) error {
		var top map[string]json.RawMessage
		if err := json.Unmarshal([]byte(d), &top); err != nil {
			return err
		}
		var entity string
		json.Unmarshal(top["entity"], &entity)
		note("view of", entity, has(top, oldViewKeys))
		return nil
	})
	if err != nil {
		return err
	}
	err = rowsOf(q, `SELECT data FROM projects`, func(d string) error {
		var top map[string]json.RawMessage
		if err := json.Unmarshal([]byte(d), &top); err != nil {
			return err
		}
		var id string
		json.Unmarshal(top["id"], &id)
		note("project", id, has(top, oldProjectKeys))
		return nil
	})
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("store: this database holds records stored by a Fyr build older than %s, which this build no longer reads: %s. "+
		"Keep running a build from before V0.versioned-store on it, or move fyr.db aside to start with an empty one",
		OldestSupported, strings.Join(found, "; "))
}

// has lists the keys of m that are in keys and not null.
func has(m map[string]json.RawMessage, keys []string) []string {
	var out []string
	for _, k := range keys {
		if v, ok := m[k]; ok && string(v) != "null" {
			out = append(out, k)
		}
	}
	return out
}

func prefixed(p string, keys []string) []string {
	for i := range keys {
		keys[i] = p + keys[i]
	}
	return keys
}
