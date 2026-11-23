package usage

import (
	"net/url"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/store"
)

func TestParseQuery(t *testing.T) {
	q, err := ParseQuery(url.Values{"from": {"2026-11-01"}, "group": {"model, part"}, "project": {"p_1"}})
	if err != nil || q.From != "2026-11-01" || len(q.Group) != 2 || q.Group[1] != "part" || q.Where["project"] != "p_1" {
		t.Fatalf("%+v %v", q, err)
	}
	for _, v := range []url.Values{{"group": {"secret"}}, {"to": {"11/23"}}, {"data": {"x"}}} {
		if _, err := ParseQuery(v); err == nil {
			t.Errorf("%v: no error", v)
		}
	}
}

// Answer names threads, projects (with their link), providers, and each
// account by its providers' names.
func TestAnswerNames(t *testing.T) {
	m := New(open(t), "test", time.UTC)
	r := cli.UsageReport{Scope: cli.PerTurn, Models: map[string]cli.TokenCount{"m": tc(1, 1, 1)}}
	if _, err := m.Record("r", Dims{Thread: "t_1", Project: "p_1", Provider: "acme", Account: "a_1", MainModel: "m"}, r); err != nil {
		t.Fatal(err)
	}
	v, err := m.Answer(Query{Group: []string{"thread", "project", "provider", "account"}}, Lookup{
		Threads: func(ids []string) map[string]string { return map[string]string{"t_1": "Fix the map"} },
		Projects: func(ids []string) map[string]ProjectName {
			return map[string]ProjectName{"p_1": {Name: "acme/web", Link: "l_1"}}
		},
		Providers: func() map[string]string {
			return map[string]string{"acme": "Claude", "acme2": "Claude work", "other": "Lab"}
		},
		AccountOf: func(p string) (string, string) {
			if p == "other" {
				return "a_2", ""
			}
			return "a_1", ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	n := v.Names
	if n.Thread["t_1"] != "Fix the map" || n.Project["p_1"].Link != "l_1" || n.Provider["acme"] != "Claude" || n.Account["a_1"] != "Claude · Claude work" || len(n.Account) != 1 {
		t.Fatalf("%+v", n)
	}
	empty, err := m.Answer(Query{From: "2030-01-01"}, Lookup{})
	if err != nil || empty.Groups == nil || len(empty.Groups) != 0 {
		t.Fatalf("an empty answer: %+v %v", empty, err)
	}
}

// V0.usage-page: the hour key is the UTC hour of each row.
func TestRollupByHour(t *testing.T) {
	m := New(open(t), "test", time.FixedZone("X", 5*3600))
	r := cli.UsageReport{Scope: cli.PerTurn, Models: map[string]cli.TokenCount{"m": tc(1, 1, 1)}}
	for i, at := range []time.Time{time.Date(2026, 11, 23, 17, 5, 0, 0, time.UTC), time.Date(2026, 11, 23, 17, 59, 0, 0, time.UTC), time.Date(2026, 11, 23, 18, 0, 0, 0, time.UTC)} {
		if _, err := m.Record(string(rune('a'+i)), Dims{At: at, MainModel: "m"}, r); err != nil {
			t.Fatal(err)
		}
	}
	gs, err := m.Rollup([]string{"hour"}, store.UsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, g := range gs {
		got[g.Key["hour"]] = g.Input
	}
	if len(got) != 2 || got["2026-11-23T17"] != 2 || got["2026-11-23T18"] != 1 {
		t.Fatalf("%v", got)
	}
}
