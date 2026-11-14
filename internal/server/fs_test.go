package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.42: the folder picker's calls go through the same guard (the
// POST needs X-Fyr), list inside home, refuse paths outside with 400, list
// repos and recent folders, and make a folder.
func TestFolderEndpoints(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	os.MkdirAll(filepath.Join(home, "git", "fyr", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	st, _ := store.Open(filepath.Join(root, "db"))
	defer st.Close()
	s := &Server{Engine: engine.New(st), BuildID: "b1", FS: &hostfs.FS{Home: home, ScanRoots: []string{filepath.Join(home, "git")}, Recent: func() []string { return []string{filepath.Join(home, "git")} }}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	get := func(path string) (int, map[string]any) {
		r := do(t, "GET", hs.URL+path, "", nil, "")
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		return r.StatusCode, m
	}
	if code, m := get("/api/fs/list?path=" + url.QueryEscape("~/git")); code != 200 || m["path"] != filepath.Join(home, "git") || len(m["dirs"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, m)
	}
	for _, p := range []string{"~/..", filepath.Join(root, "etc"), "/"} {
		if code, _ := get("/api/fs/list?path=" + url.QueryEscape(p)); code != 400 {
			t.Fatalf("list %s: %d", p, code)
		}
	}
	if code, m := get("/api/fs/repos"); code != 200 || len(m["dirs"].([]any)) != 1 {
		t.Fatalf("repos: %d %v", code, m)
	}
	if code, m := get("/api/fs/recent"); code != 200 || len(m["dirs"].([]any)) != 1 {
		t.Fatalf("recent: %d %v", code, m)
	}
	body := `{"parent":"~/git","name":"nytt"}`
	if r := do(t, "POST", hs.URL+"/api/fs/mkdir", "", nil, body); r.StatusCode != 403 {
		t.Fatalf("mkdir without X-Fyr: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/fs/mkdir", "", map[string]string{"X-Fyr": "1"}, body); r.StatusCode != 200 {
		t.Fatalf("mkdir: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/fs/mkdir", "", map[string]string{"X-Fyr": "1"}, body); r.StatusCode != 400 {
		t.Fatalf("mkdir taken: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/fs/mkdir", "", map[string]string{"X-Fyr": "1"}, `{"parent":"~/..","name":"x"}`); r.StatusCode != 400 {
		t.Fatalf("mkdir outside: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/fs/list", "evil.example", nil, ""); r.StatusCode != 403 {
		t.Fatalf("foreign host: %d", r.StatusCode)
	}
}
