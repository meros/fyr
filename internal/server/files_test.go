package server

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// V0.message-links-files: the stat call names the thread's files among the
// asked paths, and the file route serves one in a sandbox and nothing from
// outside the thread's folders.
func TestThreadFiles(t *testing.T) {
	hs, e := newServer(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(root, "acme")
	os.MkdirAll(filepath.Join(dir, "web"), 0o755)
	os.WriteFile(filepath.Join(dir, "web", "page.html"), []byte("<script>alert(1)</script>\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=1\n"), 0o644)
	r := e.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": dir, "text": "x", "profile": "claude"}})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	id := r.ThreadID
	base := hs.URL + "/api/threads/" + id + "/files"

	resp := do(t, "POST", base+"/stat", "", map[string]string{"X-Fyr": "1"}, `{"paths":["web/page.html",".env","/etc/passwd","web/none.ts"]}`)
	var got struct {
		Files []struct {
			Path, Abs, Kind string
			Size            int64
		}
	}
	json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != 200 || len(got.Files) != 1 || got.Files[0].Path != "web/page.html" || got.Files[0].Abs != filepath.Join(dir, "web", "page.html") || got.Files[0].Kind != "text" || got.Files[0].Size != 26 {
		t.Fatalf("stat: %d %+v", resp.StatusCode, got)
	}
	many := `{"paths":["` + strings.Repeat(`a","`, maxStatPaths) + `a"]}`
	if resp := do(t, "POST", base+"/stat", "", map[string]string{"X-Fyr": "1"}, many); resp.StatusCode != 400 {
		t.Fatalf("too many paths: %d", resp.StatusCode)
	}

	resp = do(t, "GET", base+"?path="+url.QueryEscape("web/page.html"), "", nil, "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "<script>alert(1)</script>\n" {
		t.Fatalf("file: %d %q", resp.StatusCode, body)
	}
	// An HTML file is text in a sandbox, never a page of Fyr's origin.
	for k, want := range map[string]string{"Content-Type": "text/plain; charset=utf-8", "Content-Security-Policy": "sandbox", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store"} {
		if resp.Header.Get(k) != want {
			t.Errorf("%s: %q, want %q", k, resp.Header.Get(k), want)
		}
	}
	if d := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(d, "inline; ") {
		t.Errorf("disposition: %q", d)
	}
	resp = do(t, "GET", base+"?download=1&path="+url.QueryEscape("web/page.html"), "", nil, "")
	if d := resp.Header.Get("Content-Disposition"); d != "attachment; filename*=UTF-8''page.html" {
		t.Errorf("download disposition: %q", d)
	}
	for _, p := range []string{".env", "/etc/passwd", "../" + filepath.Base(dir) + "/../x", ""} {
		if resp := do(t, "GET", base+"?path="+url.QueryEscape(p), "", nil, ""); resp.StatusCode != 404 {
			t.Errorf("%q: %d", p, resp.StatusCode)
		}
	}
	if resp := do(t, "GET", hs.URL+"/api/threads/t_none/files?path=x", "", nil, ""); resp.StatusCode != 404 {
		t.Errorf("no thread: %d", resp.StatusCode)
	}
}
