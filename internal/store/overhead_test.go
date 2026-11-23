package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// V0.usage-overhead: the overhead sums by the turn's dimensions; the fixed
// context is the calibrated tokens times the requests, written on a cold
// request and read on a warm one, and counted as uncalibrated until then.
func TestUsageOverhead(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	for _, r := range []struct{ id, thread string }{{"r1", "t_1"}, {"r2", "t_2"}} {
		if err := st.RecordUsage(r.id, "", at, func([]byte) ([]UsageRow, []byte, error) {
			return []UsageRow{{ID: r.id + "/m", Report: r.id, At: at, Day: "2026-11-23", Thread: r.thread, Model: "m", Part: "main", TokenCount: cli.TokenCount{Input: 1}}}, nil, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.RecordOverhead("r1", []OverheadRow{{Part: "resident", Kind: "cacheRead", Tokens: 50, Method: "x"}}, &StaticFacts{Key: "k", Model: "m", Requests: 4, Cold: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordOverhead("r1", []OverheadRow{{Part: "resident", Kind: "cacheRead", Tokens: 50, Method: "x"}}, nil); err != nil {
		t.Fatal(err) // again: nothing more
	}
	if err := st.RecordOverhead("r2", []OverheadRow{{Part: "forced", Kind: "output", Tokens: 7, Method: "x"}}, &StaticFacts{Key: "k", Model: "m", Requests: 2}); err != nil {
		t.Fatal(err)
	}
	o, err := st.UsageOverhead(UsageFilter{})
	if err != nil || o.Uncalibrated != 6 || o.Parts["resident"]["cacheRead"] != 50 || o.Parts["forced"]["output"] != 7 || o.Parts["static"] != nil {
		t.Fatalf("before calibration %+v %v", o, err)
	}
	if err := st.PutCalibration("k", "m", 1000, at); err != nil {
		t.Fatal(err)
	}
	o, _ = st.UsageOverhead(UsageFilter{})
	if o.Uncalibrated != 0 || o.Parts["static"]["cacheRead"] != 5000 || o.Parts["static"]["cacheWrite"] != 1000 {
		t.Fatalf("after %+v", o)
	}
	o, _ = st.UsageOverhead(UsageFilter{Where: map[string]string{"thread": "t_2"}})
	if o.Parts["static"]["cacheRead"] != 2000 || o.Parts["resident"] != nil {
		t.Fatalf("filtered %+v", o)
	}
	if _, err := st.UsageOverhead(UsageFilter{Where: map[string]string{"model": "m"}}); err == nil {
		t.Error("a model filter was taken")
	}
}
