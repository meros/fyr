package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/usage"
)

// V0.usage-page: GET /api/usage sums the ledger by the asked keys over a
// day range and names what it counts; a bad key is a 400.
func TestUsageEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := usage.New(st, "test", time.UTC)
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for i, prov := range []string{"acme", "acme", "lab"} {
		r := cli.UsageReport{Scope: cli.PerTurn, Models: map[string]cli.TokenCount{"claude-big": {Input: 1, Output: 10, CacheRead: 100}}}
		if _, err := m.Record(string(rune('a'+i)), usage.Dims{At: at.Add(time.Duration(i) * 24 * time.Hour), Provider: prov, Account: "a_" + prov, MainModel: "claude-big"}, r); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Engine: engine.New(st), Usage: func(q usage.Query) (usage.View, error) {
		return m.Answer(q, usage.Lookup{Providers: func() map[string]string { return map[string]string{"acme": "Claude", "lab": "Lab"} },
			AccountOf: func(p string) (string, string) { return "a_" + p, "subscription" }})
	}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	var v usage.View
	if err := json.NewDecoder(do(t, "GET", hs.URL+"/api/usage?group=provider,account&from=2026-11-20&to=2026-11-21", "", nil, "").Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if len(v.Groups) != 1 || v.Groups[0].Key["provider"] != "acme" || v.Groups[0].Output != 20 || v.Groups[0].Reports != 2 {
		t.Fatalf("groups %+v", v.Groups)
	}
	if v.Names.Provider["acme"] != "Claude" || v.Names.Account["a_acme"] != "Claude" {
		t.Fatalf("names %+v", v.Names)
	}
	for _, bad := range []string{"?group=nope", "?from=yesterday", "?secret=1"} {
		if r := do(t, "GET", hs.URL+"/api/usage"+bad, "", nil, ""); r.StatusCode != 400 {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}
}
