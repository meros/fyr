// Package store keeps Fyr's state in one SQLite file: an append-only event
// log, JSON projections of threads and per-user views, and command receipts.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/meros/fyr/internal/core"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

// Event is one line of the log. Subject is the entity id.
type Event struct {
	Seq     int64           `json:"seq"`
	At      time.Time       `json:"at"`
	Subject string          `json:"subject"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
}

const schema = `
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL, subject TEXT NOT NULL, type TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_subject ON events(subject, seq);
CREATE TABLE IF NOT EXISTS threads(id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS views(user TEXT NOT NULL, entity TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(user, entity));
CREATE TABLE IF NOT EXISTS receipts(id TEXT PRIMARY KEY, at TEXT NOT NULL, result TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS kv(k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY, data TEXT NOT NULL);
DROP TABLE IF EXISTS workspaces; -- V0.55: workspaces are gone (decision 0017)
CREATE TABLE IF NOT EXISTS providers(id TEXT PRIMARY KEY, pos INTEGER NOT NULL, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS effects(id TEXT PRIMARY KEY, thread TEXT NOT NULL, state TEXT NOT NULL, data TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS changes(id TEXT PRIMARY KEY, data TEXT NOT NULL); -- #35: host change records
CREATE TABLE IF NOT EXISTS push_subs(endpoint TEXT PRIMARY KEY, user TEXT NOT NULL, device TEXT NOT NULL, sub TEXT NOT NULL, created TEXT NOT NULL);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; the engine serialises commands anyway
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Tx is one atomic write: thread and view projections, events and a receipt.
type Tx struct {
	tx     *sql.Tx
	Events []Event
}

func (s *Store) Write(fn func(*Tx) error) ([]Event, error) {
	sqlTx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	tx := &Tx{tx: sqlTx}
	if err := fn(tx); err != nil {
		sqlTx.Rollback()
		return nil, err
	}
	return tx.Events, sqlTx.Commit()
}

func (tx *Tx) PutThread(t *core.Thread) error {
	b, _ := json.Marshal(t)
	_, err := tx.tx.Exec(`INSERT INTO threads(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, t.ID, string(b))
	return err
}

// PutChange stores a host change record (#35).
func (tx *Tx) PutChange(c *core.Change) error {
	b, _ := json.Marshal(c)
	_, err := tx.tx.Exec(`INSERT INTO changes(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, c.ID, string(b))
	return err
}

// DeleteChange removes a host change record.
func (tx *Tx) DeleteChange(id string) error {
	_, err := tx.tx.Exec(`DELETE FROM changes WHERE id=?`, id)
	return err
}

// Changes are the host change records.
func (s *Store) Changes() ([]*core.Change, error) {
	rows, err := s.db.Query(`SELECT data FROM changes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Change
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		c := &core.Change{}
		if err := json.Unmarshal([]byte(d), c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (tx *Tx) PutView(v *core.View) error {
	b, _ := json.Marshal(v)
	_, err := tx.tx.Exec(`INSERT INTO views(user,entity,data) VALUES(?,?,?) ON CONFLICT(user,entity) DO UPDATE SET data=excluded.data`, v.User, v.Entity, string(b))
	return err
}

func (tx *Tx) Append(subject, typ string, data any, at time.Time) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	r, err := tx.tx.Exec(`INSERT INTO events(at,subject,type,data) VALUES(?,?,?,?)`, at.UTC().Format(time.RFC3339Nano), subject, typ, string(b))
	if err != nil {
		return err
	}
	seq, _ := r.LastInsertId()
	tx.Events = append(tx.Events, Event{Seq: seq, At: at, Subject: subject, Type: typ, Data: b})
	return nil
}

func (tx *Tx) PutReceipt(id string, result any, at time.Time) error {
	b, _ := json.Marshal(result)
	_, err := tx.tx.Exec(`INSERT INTO receipts(id,at,result) VALUES(?,?,?)`, id, at.UTC().Format(time.RFC3339Nano), string(b))
	return err
}

// Effects that do work outside Fyr run at most once (decision 0020 (3)):
// the reduce that returns one stores it as pending in its transaction; the
// runner claims it (started) before the work, stores the result (finished),
// and the transaction that applies the result marks it done. A deploy hands
// its result to the next daemon (later).
const (
	EffectPending  = "new"
	EffectStarted  = "started"
	EffectFinished = "finished"
	EffectLater    = "later"
	EffectDone     = "done"
)

// StoredEffect is one effect row.
type StoredEffect struct {
	ID, Thread, State string
	Data, Result      json.RawMessage
}

// PutEffect stores a new effect as pending.
func (tx *Tx) PutEffect(id, thread string, data any, at time.Time) error {
	b, _ := json.Marshal(data)
	_, err := tx.tx.Exec(`INSERT INTO effects(id,thread,state,data,at) VALUES(?,?,?,?,?)`, id, thread, EffectPending, string(b), at.UTC().Format(time.RFC3339Nano))
	return err
}

// EffectDone marks an effect's result as applied.
func (tx *Tx) EffectDone(id string) error {
	_, err := tx.tx.Exec(`UPDATE effects SET state=? WHERE id=?`, EffectDone, id)
	return err
}

// MoveEffect moves an effect from one state to another and reports whether
// it was in from: a claim that two runners cannot both win.
func (s *Store) MoveEffect(id, from, to string, result any) (bool, error) {
	res := ""
	if result != nil {
		b, _ := json.Marshal(result)
		res = string(b)
	}
	r, err := s.db.Exec(`UPDATE effects SET state=?, result=CASE WHEN ?='' THEN result ELSE ? END WHERE id=? AND state=?`, to, res, res, id, from)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// OpenEffects are the effects not done, oldest first.
func (s *Store) OpenEffects() ([]StoredEffect, error) {
	rows, err := s.db.Query(`SELECT id,thread,state,data,result FROM effects WHERE state<>? ORDER BY at, rowid`, EffectDone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredEffect
	for rows.Next() {
		var e StoredEffect
		var d, r string
		if err := rows.Scan(&e.ID, &e.Thread, &e.State, &d, &r); err != nil {
			return nil, err
		}
		e.Data, e.Result = json.RawMessage(d), json.RawMessage(r)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Receipt returns the stored result of a command id, or nil when it is new.
func (s *Store) Receipt(id string) (json.RawMessage, error) {
	var r string
	err := s.db.QueryRow(`SELECT result FROM receipts WHERE id=?`, id).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(r), err
}

func (s *Store) Threads() ([]*core.Thread, error) {
	rows, err := s.db.Query(`SELECT data FROM threads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Thread
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		t, err := core.DecodeThread([]byte(d)) // #31: also the shape stored before
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Views(user string) (map[string]*core.View, error) {
	rows, err := s.db.Query(`SELECT data FROM views WHERE user=?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*core.View{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		v, err := core.DecodeView([]byte(d)) // #46: also the snooze stored before Rules
		if err != nil {
			return nil, err
		}
		out[v.Entity] = v
	}
	return out, rows.Err()
}

// EventsFor returns the events of one subject after seq, oldest first, at
// most limit. With tail set it returns the newest limit events instead.
func (s *Store) EventsFor(subject string, after int64, limit int, tail bool) ([]Event, error) {
	q := `SELECT seq,at,subject,type,data FROM events WHERE subject=? AND seq>? ORDER BY seq LIMIT ?`
	if tail {
		q = `SELECT * FROM (SELECT seq,at,subject,type,data FROM events WHERE subject=? AND seq>? ORDER BY seq DESC LIMIT ?) ORDER BY seq`
	}
	rows, err := s.db.Query(q, subject, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at, d string
		if err := rows.Scan(&e.Seq, &at, &e.Subject, &e.Type, &d); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		e.Data = json.RawMessage(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventsBefore returns the newest limit events of a subject with seq < before, oldest first.
func (s *Store) EventsBefore(subject string, before int64, limit int) ([]Event, error) {
	rows, err := s.db.Query(`SELECT * FROM (SELECT seq,at,subject,type,data FROM events WHERE subject=? AND seq<? ORDER BY seq DESC LIMIT ?) ORDER BY seq`, subject, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at, d string
		if err := rows.Scan(&e.Seq, &at, &e.Subject, &e.Type, &d); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		e.Data = json.RawMessage(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetKV(k string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetKV stores a value; an empty value deletes the key.
func (s *Store) SetKV(k, v string) error {
	if v == "" {
		_, err := s.db.Exec(`DELETE FROM kv WHERE k=?`, k)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

// Snapshot writes a consistent copy of the database to path (VACUUM INTO).
func (s *Store) Snapshot(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// PushSub is one stored Web Push subscription (SPEC 0.5 V0.15).
type PushSub struct {
	Endpoint, User, Device string
	Sub                    json.RawMessage
}

// PutPushSub stores or replaces a subscription by its endpoint.
func (s *Store) PutPushSub(user, endpoint, device string, sub []byte) error {
	_, err := s.db.Exec(`INSERT INTO push_subs(endpoint,user,device,sub,created) VALUES(?,?,?,?,?)
ON CONFLICT(endpoint) DO UPDATE SET user=excluded.user, device=excluded.device, sub=excluded.sub`,
		endpoint, user, device, string(sub), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) DeletePushSub(endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subs WHERE endpoint=?`, endpoint)
	return err
}

func (s *Store) PushSubs(user string) ([]PushSub, error) {
	rows, err := s.db.Query(`SELECT endpoint,user,device,sub FROM push_subs WHERE user=? ORDER BY created`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSub
	for rows.Next() {
		var p PushSub
		var sub string
		if err := rows.Scan(&p.Endpoint, &p.User, &p.Device, &sub); err != nil {
			return nil, err
		}
		p.Sub = json.RawMessage(sub)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) HasPushSub(endpoint string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM push_subs WHERE endpoint=?`, endpoint).Scan(&n)
	return n > 0, err
}

func (tx *Tx) PutProject(p *core.Project) error {
	b, _ := json.Marshal(p)
	_, err := tx.tx.Exec(`INSERT INTO projects(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, p.ID, string(b))
	return err
}

func (s *Store) Projects() ([]*core.Project, error) {
	rows, err := s.db.Query(`SELECT data FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Project
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		p := &core.Project{}
		if err := json.Unmarshal([]byte(d), p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (tx *Tx) DeleteProject(id string) error {
	_, err := tx.tx.Exec(`DELETE FROM projects WHERE id=?`, id)
	return err
}

// OldSource is a source of a project stored before V0.55 (decision 0017);
// only the migration at start reads it.
type OldSource struct {
	Kind string `json:"kind"` // folder, repo
	Host string `json:"host,omitempty"`
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
	Main bool   `json:"main,omitempty"`
}

// OldProjectSources returns, by project id, the sources of the projects
// still stored with them (before V0.55).
func (s *Store) OldProjectSources() (map[string][]OldSource, error) {
	rows, err := s.db.Query(`SELECT data FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]OldSource{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		var p struct {
			ID      string      `json:"id"`
			Sources []OldSource `json:"sources"`
		}
		if json.Unmarshal([]byte(d), &p) == nil && p.Sources != nil {
			out[p.ID] = p.Sources
		}
	}
	return out, rows.Err()
}

// PutProvider stores a provider's settings as JSON at a position (SPEC 0.5
// V0.50). The caller passes data without secrets; the table is additive.
func (s *Store) PutProvider(id string, pos int, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO providers(id,pos,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET pos=excluded.pos, data=excluded.data`, id, pos, string(data))
	return err
}

// Providers returns the stored providers' JSON in position order.
func (s *Store) Providers() ([][]byte, error) {
	rows, err := s.db.Query(`SELECT data FROM providers ORDER BY pos, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, []byte(d))
	}
	return out, rows.Err()
}

func (s *Store) DeleteProvider(id string) error {
	_, err := s.db.Exec(`DELETE FROM providers WHERE id=?`, id)
	return err
}
