package server

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.44: GET /api/quota lists the statuses; the state's profiles
// carry only the quota kind, never paths or variable names.
func TestQuotaEndpointAndProfileKind(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{Engine: engine.New(st), Web: fstest.MapFS{"index.html": {Data: []byte("x")}},
		Profiles: []agent.Profile{{ID: "ds", Name: "DeepSeek", Quota: &quota.Spec{Kind: "deepseek", KeyFile: "/run/secrets/deepseek", KeyEnv: "SECRET_VAR"}}},
		Quota: func(context.Context) any {
			return []quota.Status{{Profile: "ds", Kind: "deepseek", Error: "No API key: set keyFile or keyEnv"}}
		}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	b, _ := io.ReadAll(do(t, "GET", hs.URL+"/api/quota", "", nil, "").Body)
	if !strings.Contains(string(b), `"profile":"ds"`) || !strings.Contains(string(b), `"kind":"deepseek"`) {
		t.Fatalf("quota: %s", b)
	}
	b, _ = io.ReadAll(do(t, "GET", hs.URL+"/api/state", "", nil, "").Body)
	if !strings.Contains(string(b), `"quota":{"kind":"deepseek"}`) || strings.Contains(string(b), "secrets") || strings.Contains(string(b), "SECRET_VAR") {
		t.Fatalf("state profiles: %s", b)
	}
	hs2, _ := newServer(t)
	b, _ = io.ReadAll(do(t, "GET", hs2.URL+"/api/quota", "", nil, "").Body)
	if strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("no quota service: %s", b)
	}
}
