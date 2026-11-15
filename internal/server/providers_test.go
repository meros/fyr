package server

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
)

type fakeProviders struct{ cmds []api.Cmd }

func (f *fakeProviders) List() any {
	return []map[string]any{{"id": "p1", "keySet": true, "keyLast4": "zz12"}}
}
func (f *fakeProviders) Found() any { return []map[string]any{{"id": "openrouter"}} }
func (f *fakeProviders) Command(c api.Cmd) api.Reply {
	f.cmds = append(f.cmds, c)
	return api.Reply{OK: true}
}

// SPEC 0.5 V0.50: the provider endpoints, provider.* commands go to the
// registry, and the state's profiles come from ProfileList without env.
func TestProviderEndpoints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fp := &fakeProviders{}
	s := &Server{Engine: engine.New(st), Web: fstest.MapFS{"index.html": {Data: []byte("x")}}, Providers: fp,
		ProfileList: func() []provider.Profile {
			return []provider.Profile{{ID: "p1", Name: "P", Env: []string{"ANTHROPIC_AUTH_TOKEN=sk-leak-1234"}, Key: func() (string, error) { return "sk-leak-5678", nil }}}
		}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	b, _ := io.ReadAll(do(t, "GET", hs.URL+"/api/providers", "", nil, "").Body)
	if !strings.Contains(string(b), `"keyLast4":"zz12"`) {
		t.Fatal(string(b))
	}
	b, _ = io.ReadAll(do(t, "GET", hs.URL+"/api/providers/found", "", nil, "").Body)
	if !strings.Contains(string(b), "openrouter") {
		t.Fatal(string(b))
	}
	r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, `{"commandId":"c1","action":"provider.setKey","args":{"id":"p1","key":"k"}}`)
	if r.StatusCode != 200 || len(fp.cmds) != 1 || fp.cmds[0].Action != "provider.setKey" {
		t.Fatalf("%d %+v", r.StatusCode, fp.cmds)
	}
	b, _ = io.ReadAll(do(t, "GET", hs.URL+"/api/state", "", nil, "").Body)
	if !strings.Contains(string(b), `"id":"p1"`) || strings.Contains(string(b), "sk-leak") {
		t.Fatalf("state: %s", b)
	}
	// #42: the profiles read, without env, replaces the client's state reload.
	b, _ = io.ReadAll(do(t, "GET", hs.URL+"/api/profiles", "", nil, "").Body)
	if !strings.HasPrefix(string(b), `[{"id":"p1"`) || strings.Contains(string(b), "sk-leak") {
		t.Fatalf("profiles: %s", b)
	}
}
