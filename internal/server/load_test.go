package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.80: GET /api/load is this host's load, named, as a list of one.
func TestV080LoadEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proc := t.TempDir()
	os.WriteFile(filepath.Join(proc, "loadavg"), []byte("3.50 2.00 1.00 1/10 1\n"), 0o644)
	s := &Server{Engine: engine.New(st), Host: "laptop", Load: &cgroup.Sampler{Proc: proc, Cores: 8}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	var got []map[string]any
	if err := json.NewDecoder(do(t, "GET", hs.URL+"/api/load", "", nil, "").Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["host"] != "laptop" || got[0]["cores"] != 8.0 || got[0]["cgroups"] != false {
		t.Fatalf("%v", got)
	}
	if l := got[0]["load"].([]any); l[0] != 3.5 {
		t.Fatalf("load %v", l)
	}
	if th, ok := got[0]["threads"].([]any); !ok || len(th) != 0 {
		t.Fatalf("threads %v", got[0]["threads"])
	}
}
