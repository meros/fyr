package usage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/store"
)

func tc(in, out, cr int64) cli.TokenCount {
	return cli.TokenCount{Input: in, Output: out, CacheRead: cr}
}

func cum(session string, main *cli.TokenCount, models map[string]cli.TokenCount) cli.UsageReport {
	return cli.UsageReport{Session: session, Scope: cli.Cumulative, Models: models, Main: main}
}

func ptr(c cli.TokenCount) *cli.TokenCount { return &c }

// V0.usage-ledger: the fold of decision 0041.
func TestFold(t *testing.T) {
	const big, small = "claude-big", "claude-small"
	cases := []struct {
		name      string
		base      map[string]cli.TokenCount
		r         cli.UsageReport
		wantDelta map[string]cli.TokenCount
	}{
		{"first report counts all", nil,
			cum("s", ptr(tc(10, 5, 100)), map[string]cli.TokenCount{big: tc(10, 5, 100), small: tc(3, 1, 0)}),
			map[string]cli.TokenCount{big: tc(10, 5, 100), small: tc(3, 1, 0)}},
		{"a later turn counts the difference", map[string]cli.TokenCount{big: tc(10, 5, 100), small: tc(3, 1, 0)},
			cum("s", ptr(tc(2, 7, 50)), map[string]cli.TokenCount{big: tc(12, 12, 150), small: tc(3, 1, 0)}),
			map[string]cli.TokenCount{big: tc(2, 7, 50)}},
		{"subagent tokens beyond the main share count", map[string]cli.TokenCount{big: tc(10, 5, 100)},
			cum("s", ptr(tc(1, 1, 10)), map[string]cli.TokenCount{big: tc(31, 26, 400)}),
			map[string]cli.TokenCount{big: tc(21, 21, 300)}},
		{"a resume that restored the counts goes on", map[string]cli.TokenCount{big: tc(1000, 500, 90000)},
			cum("s", ptr(tc(2, 40, 3000)), map[string]cli.TokenCount{big: tc(1002, 540, 93000)}),
			map[string]cli.TokenCount{big: tc(2, 40, 3000)}},
		{"a count that went down is a reset", map[string]cli.TokenCount{big: tc(1000, 500, 90000)},
			cum("s", ptr(tc(2, 40, 3000)), map[string]cli.TokenCount{big: tc(2, 40, 3000)}),
			map[string]cli.TokenCount{big: tc(2, 40, 3000)}},
		{"a copied session that outgrew its old baseline is a reset", map[string]cli.TokenCount{big: tc(5, 10, 200)},
			cum("s", ptr(tc(6, 30, 4000)), map[string]cli.TokenCount{big: tc(6, 30, 4000)}),
			map[string]cli.TokenCount{big: tc(6, 30, 4000)}},
		{"a reset counts every model from zero", map[string]cli.TokenCount{big: tc(5, 10, 200), small: tc(1, 1, 0)},
			cum("s", ptr(tc(6, 30, 4000)), map[string]cli.TokenCount{big: tc(6, 30, 4000), small: tc(2, 2, 0)}),
			map[string]cli.TokenCount{big: tc(6, 30, 4000), small: tc(2, 2, 0)}},
		{"a per-turn report is its own delta", map[string]cli.TokenCount{big: tc(9, 9, 9)},
			cli.UsageReport{Scope: cli.PerTurn, Models: map[string]cli.TokenCount{big: tc(1, 2, 3)}},
			map[string]cli.TokenCount{big: tc(1, 2, 3)}},
		{"an unchanged report counts nothing", map[string]cli.TokenCount{big: tc(10, 5, 100)},
			cum("s", ptr(cli.TokenCount{}), map[string]cli.TokenCount{big: tc(10, 5, 100)}),
			map[string]cli.TokenCount{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, _ := Fold(c.base, c.r, big)
			if len(d) != len(c.wantDelta) {
				t.Fatalf("delta %v, want %v", d, c.wantDelta)
			}
			for m, w := range c.wantDelta {
				if d[m] != w {
					t.Errorf("%s: %+v, want %+v", m, d[m], w)
				}
			}
		})
	}
}

func TestFoldPerTurnKeepsBaseline(t *testing.T) {
	base := map[string]cli.TokenCount{"m": tc(1, 1, 1)}
	_, next := Fold(base, cli.UsageReport{Scope: cli.PerTurn, Models: map[string]cli.TokenCount{"m": tc(5, 5, 5)}}, "m")
	if next["m"] != tc(1, 1, 1) {
		t.Fatalf("next %v", next)
	}
}

func TestSplit(t *testing.T) {
	delta := map[string]cli.TokenCount{"big": tc(30, 20, 400), "small": tc(3, 1, 0)}
	p := Split(delta, ptr(tc(10, 5, 100)), "big")
	if p["big"][PartMain] != tc(10, 5, 100) || p["big"][PartOther] != tc(20, 15, 300) {
		t.Errorf("big: %+v", p["big"])
	}
	if _, ok := p["small"][PartMain]; ok || p["small"][PartOther] != tc(3, 1, 0) {
		t.Errorf("small: %+v", p["small"])
	}
	// The main model by another name: the biggest model takes the main share.
	p = Split(delta, ptr(tc(10, 5, 100)), "opus")
	if p["big"][PartMain] != tc(10, 5, 100) {
		t.Errorf("fallback: %+v", p)
	}
	// No main share: all of it is main.
	p = Split(delta, nil, "big")
	if p["big"][PartMain] != tc(30, 20, 400) || len(p["big"]) != 1 {
		t.Errorf("no main: %+v", p)
	}
}

func open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sum(rows []store.UsageRow) cli.TokenCount {
	var c cli.TokenCount
	for _, r := range rows {
		c = c.Add(r.TokenCount)
	}
	return c
}

func TestRecordReplayAndRestart(t *testing.T) {
	st := open(t)
	m := New(st, "test", time.UTC)
	heard := 0
	m.OnRecord = func(string) { heard++ }
	d := Dims{At: time.Date(2026, 11, 23, 23, 30, 0, 0, time.UTC), Source: SourceTurn, Trigger: TriggerOwner, Thread: "t_1",
		Project: "p_1", Provider: "acme", Account: "a_1", Billing: "subscription", Adapter: "claude", MainModel: "big"}
	r1 := cum("s1", ptr(tc(10, 5, 100)), map[string]cli.TokenCount{"big": tc(10, 5, 100), "small": tc(3, 1, 0)})
	rows, err := m.Record("r1", d, r1)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %v err %v", rows, err)
	}
	if rows[0].Day != "2026-11-23" || rows[0].FyrVersion != "test" {
		t.Errorf("row %+v", rows[0])
	}
	// The runner delivers the same result again: nothing more.
	if rows, err := m.Record("r1", d, r1); err != nil || rows != nil {
		t.Fatalf("replay wrote %v %v", rows, err)
	}
	// fyrd restarts: a new meter reads the totals and the baseline from the store.
	m2 := New(st, "test", time.UTC)
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if got, _ := m2.ThreadTotal("t_1"); got != tc(13, 6, 100) {
		t.Fatalf("loaded total %+v", got)
	}
	r2 := cum("s1", ptr(tc(1, 2, 50)), map[string]cli.TokenCount{"big": tc(21, 17, 250), "small": tc(3, 1, 0)})
	rows, err = m2.Record("r2", d, r2)
	if err != nil {
		t.Fatal(err)
	}
	if got := sum(rows); got != tc(11, 12, 150) {
		t.Fatalf("second turn %+v", got)
	}
	var main, other cli.TokenCount
	for _, r := range rows {
		if r.Part == PartMain {
			main = main.Add(r.TokenCount)
		} else {
			other = other.Add(r.TokenCount)
		}
	}
	if main != tc(1, 2, 50) || other != tc(10, 10, 100) {
		t.Errorf("main %+v other %+v", main, other)
	}
	if got, _ := m2.ThreadTotal("t_1"); got != tc(24, 18, 250) {
		t.Errorf("total %+v", got)
	}
	if heard != 1 {
		t.Errorf("OnRecord heard %d", heard)
	}
	gs, err := m2.Rollup([]string{"model", "part"}, store.UsageFilter{From: "2026-11-23", To: "2026-11-23", Where: map[string]string{"account": "a_1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 3 {
		t.Fatalf("groups %+v", gs)
	}
	if _, err := m2.Rollup([]string{"nope"}, store.UsageFilter{}); err == nil {
		t.Error("an unknown group was accepted")
	}
}

func TestRecordNeedsID(t *testing.T) {
	m := New(open(t), "test", time.UTC)
	if _, err := m.Record("", Dims{}, cli.UsageReport{}); err == nil {
		t.Fatal("no error")
	}
}

func TestSeed(t *testing.T) {
	st := open(t)
	m := New(st, "test", time.UTC)
	d := Dims{Thread: "t_1", MainModel: "big"}
	if _, err := m.Record("r1", d, cum("parent", ptr(tc(10, 5, 100)), map[string]cli.TokenCount{"big": tc(10, 5, 100)})); err != nil {
		t.Fatal(err)
	}
	if err := m.Seed("child", "parent"); err != nil {
		t.Fatal(err)
	}
	rows, err := m.Record("r2", d, cum("child", ptr(tc(1, 1, 10)), map[string]cli.TokenCount{"big": tc(11, 6, 110)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := sum(rows); got != tc(1, 1, 10) {
		t.Fatalf("child counted %+v", got)
	}
}
