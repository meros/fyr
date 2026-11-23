package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// V0.usage-ledger (decision 0041): the usage ledger is a measurement kept
// outside the one path (SPEC 3, "Outside the one path", item 4). One meter
// writes it; no reducer reads it.

const usageSchema = `
CREATE TABLE IF NOT EXISTS usage(
  id TEXT PRIMARY KEY, report TEXT NOT NULL, at TEXT NOT NULL, day TEXT NOT NULL,
  source TEXT NOT NULL, trigger TEXT NOT NULL, thread TEXT NOT NULL, project TEXT NOT NULL,
  provider TEXT NOT NULL, account TEXT NOT NULL, billing TEXT NOT NULL, adapter TEXT NOT NULL,
  session TEXT NOT NULL, model TEXT NOT NULL, effort TEXT NOT NULL, part TEXT NOT NULL,
  input INTEGER NOT NULL, output INTEGER NOT NULL, cache_read INTEGER NOT NULL,
  cache_write_5m INTEGER NOT NULL, cache_write_1h INTEGER NOT NULL, thinking INTEGER NOT NULL,
  web_search INTEGER NOT NULL, web_fetch INTEGER NOT NULL, fyr_version TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS usage_day ON usage(day);
CREATE INDEX IF NOT EXISTS usage_thread ON usage(thread, at);
CREATE TABLE IF NOT EXISTS usage_seen(report TEXT PRIMARY KEY, at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS usage_baseline(session TEXT PRIMARY KEY, data TEXT NOT NULL, at TEXT NOT NULL);
`

// UsageRow is one row of the ledger: one report, one model, one part.
type UsageRow struct {
	ID, Report                                                 string
	At                                                         time.Time
	Day                                                        string // the host's local day
	Source, Trigger, Thread, Project, Provider, Account        string
	Billing, Adapter, Session, Model, Effort, Part, FyrVersion string
	cli.TokenCount
}

// ErrUsageSeen: the report was recorded before (a replayed result).
var ErrUsageSeen = errors.New("usage report already recorded")

// RecordUsage writes one report in one transaction: it refuses a report id
// it has seen, reads the session's baseline, lets fold make the rows and
// the next baseline, and stores both. session "" keeps no baseline.
func (s *Store) RecordUsage(report, session string, at time.Time, fold func(base []byte) ([]UsageRow, []byte, error)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO usage_seen(report,at) VALUES(?,?) ON CONFLICT(report) DO NOTHING`, report, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUsageSeen
	}
	var base []byte
	if session != "" {
		var b string
		switch err := tx.QueryRow(`SELECT data FROM usage_baseline WHERE session=?`, session).Scan(&b); {
		case err == nil:
			base = []byte(b)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
	}
	rows, next, err := fold(base)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO usage(id,report,at,day,source,trigger,thread,project,provider,account,billing,adapter,session,model,effort,part,
			input,output,cache_read,cache_write_5m,cache_write_1h,thinking,web_search,web_fetch,fyr_version)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.Report, r.At.UTC().Format(time.RFC3339Nano), r.Day, r.Source, r.Trigger, r.Thread, r.Project, r.Provider, r.Account,
			r.Billing, r.Adapter, r.Session, r.Model, r.Effort, r.Part,
			r.Input, r.Output, r.CacheRead, r.CacheWrite5m, r.CacheWrite1h, r.Thinking, r.WebSearch, r.WebFetch, r.FyrVersion); err != nil {
			return err
		}
	}
	if session != "" && next != nil {
		if _, err := tx.Exec(`INSERT INTO usage_baseline(session,data,at) VALUES(?,?,?) ON CONFLICT(session) DO UPDATE SET data=excluded.data, at=excluded.at`,
			session, string(next), at.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UsageBaseline reads a session's baseline; nil when it has none.
func (s *Store) UsageBaseline(session string) ([]byte, error) {
	var b string
	err := s.db.QueryRow(`SELECT data FROM usage_baseline WHERE session=?`, session).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return []byte(b), err
}

// SeedUsageBaseline gives a new session its parent's baseline (a fork that
// starts from the parent's counts); a session that has one keeps it.
func (s *Store) SeedUsageBaseline(session string, data []byte, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO usage_baseline(session,data,at) VALUES(?,?,?) ON CONFLICT(session) DO NOTHING`,
		session, string(data), at.UTC().Format(time.RFC3339Nano))
	return err
}

// UsageColumns are the ledger's group keys, by the name the API uses.
var UsageColumns = map[string]string{
	"day": "day", "source": "source", "trigger": "trigger", "thread": "thread", "project": "project",
	"provider": "provider", "account": "account", "billing": "billing", "adapter": "adapter",
	"model": "model", "effort": "effort", "part": "part", "session": "session",
}

// UsageFilter narrows a rollup: a day range (inclusive, "" for open) and
// column = value pairs, by API name.
type UsageFilter struct {
	From, To string
	Where    map[string]string
}

// UsageGroup is one rollup row: the group's key values, by API name, and
// its sums.
type UsageGroup struct {
	Key     map[string]string `json:"key"`
	Reports int64             `json:"reports"`
	cli.TokenCount
}

// UsageRollup sums the ledger by the given keys (API names).
func (s *Store) UsageRollup(group []string, f UsageFilter) ([]UsageGroup, error) {
	var cols []string
	for _, g := range group {
		c, ok := UsageColumns[g]
		if !ok {
			return nil, fmt.Errorf("unknown usage group %q", g)
		}
		cols = append(cols, c)
	}
	var where []string
	var args []any
	if f.From != "" {
		where, args = append(where, "day >= ?"), append(args, f.From)
	}
	if f.To != "" {
		where, args = append(where, "day <= ?"), append(args, f.To)
	}
	for k, v := range f.Where {
		c, ok := UsageColumns[k]
		if !ok {
			return nil, fmt.Errorf("unknown usage filter %q", k)
		}
		where, args = append(where, c+" = ?"), append(args, v)
	}
	sel := `COUNT(DISTINCT report), SUM(input), SUM(output), SUM(cache_read), SUM(cache_write_5m), SUM(cache_write_1h), SUM(thinking), SUM(web_search), SUM(web_fetch)`
	q := "SELECT "
	if len(cols) > 0 {
		q += strings.Join(cols, ", ") + ", "
	}
	q += sel + " FROM usage"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if len(cols) > 0 {
		q += " GROUP BY " + strings.Join(cols, ", ")
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageGroup
	for rows.Next() {
		keys := make([]string, len(cols))
		var g UsageGroup
		dst := make([]any, 0, len(cols)+9)
		for i := range keys {
			dst = append(dst, &keys[i])
		}
		var sums [8]sql.NullInt64
		dst = append(dst, &g.Reports)
		for i := range sums {
			dst = append(dst, &sums[i])
		}
		if err := rows.Scan(dst...); err != nil {
			return nil, err
		}
		g.TokenCount = cli.TokenCount{Input: sums[0].Int64, Output: sums[1].Int64, CacheRead: sums[2].Int64, CacheWrite5m: sums[3].Int64,
			CacheWrite1h: sums[4].Int64, Thinking: sums[5].Int64, WebSearch: sums[6].Int64, WebFetch: sums[7].Int64}
		if g.Reports == 0 && len(cols) == 0 {
			continue // no rows: no group
		}
		g.Key = map[string]string{}
		for i, k := range group {
			g.Key[k] = keys[i]
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
