package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/store"
)

type fakeShooter struct{ calls int }

func (f *fakeShooter) Shoot(_ context.Context, url string, w, h int, full bool) ([]byte, error) {
	f.calls++
	return []byte("\x89PNG shot of " + url), nil
}

func showEngine(t *testing.T) (*Engine, string, func(args map[string]any) (string, bool)) {
	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "db"))
	t.Cleanup(func() { st.Close() })
	e := New(st)
	e.Attach = &attach.Store{Dir: filepath.Join(dir, "att")}
	e.Shooter = &fakeShooter{}
	r := e.Command(api.Cmd{CommandID: "c", Action: "thread.create", Args: core.Args{"cwd": dir, "text": "x"}})
	srv := mcp.New(func(string) (string, bool) { return r.ThreadID, true })
	e.Tools(srv)
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	call := func(args map[string]any) (string, bool) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "show", "arguments": args}})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		var out struct {
			Result struct {
				Content []struct{ Text string }
				IsError bool
			}
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Result.Content[0].Text, out.Result.IsError
	}
	return e, r.ThreadID, call
}

func previews(t *testing.T, e *Engine, id string) []map[string]any {
	evs, _ := e.Events(id, 0, 100)
	var out []map[string]any
	for _, ev := range evs {
		if ev.Type == "preview" {
			var m map[string]any
			json.Unmarshal(ev.Data, &m)
			out = append(out, m)
		}
	}
	return out
}

// SPEC 0.5 V0.19: show {path} copies an image and appends one preview event;
// the reply is short text with the path, never the pixels.
func TestShowImage(t *testing.T) {
	e, id, call := showEngine(t)
	img := filepath.Join(t.TempDir(), "chart.png")
	os.WriteFile(img, []byte("\x89PNG data"), 0o644)
	text, isErr := call(map[string]any{"path": img, "title": "Chart"})
	if isErr || !strings.Contains(text, "Shown") || !strings.Contains(text, ".png") || len(text) > 300 {
		t.Fatalf("%v %q", isErr, text)
	}
	p := previews(t, e, id)
	if len(p) != 1 || p[0]["kind"] != "image" || p[0]["title"] != "Chart" || p[0]["attachmentId"] == "" {
		t.Fatalf("%+v", p)
	}
	if _, isErr := call(map[string]any{"path": "/etc"}); !isErr {
		t.Fatal("a directory was accepted")
	}
	txt := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(txt, []byte("x"), 0o644)
	if _, isErr := call(map[string]any{"path": txt}); !isErr {
		t.Fatal("a non-image was accepted as an image")
	}
	if _, isErr := call(map[string]any{"path": img, "html": "<b>x</b>"}); !isErr {
		t.Fatal("two sources at once were accepted")
	}
}

// SPEC 0.5 V0.19: an HTML fragment is wrapped with the theme; identical
// HTML is stored once.
func TestShowHTML(t *testing.T) {
	e, id, call := showEngine(t)
	call(map[string]any{"html": "<h1>Hej å</h1>", "title": "Mock"})
	call(map[string]any{"html": "<h1>Hej å</h1>", "title": "Mock again"})
	p := previews(t, e, id)
	if len(p) != 2 || p[0]["kind"] != "html" || p[0]["attachmentId"] != p[1]["attachmentId"] {
		t.Fatalf("%+v", p)
	}
	m, err := e.Attach.Get(p[0]["attachmentId"].(string))
	if err != nil || m.MediaType != "text/html" {
		t.Fatalf("%+v %v", m, err)
	}
	b, _ := os.ReadFile(m.Path)
	page := string(b)
	if !strings.Contains(page, "<!doctype html>") || !strings.Contains(page, "--fg") || !strings.Contains(page, "prefers-color-scheme") || !strings.Contains(page, "<h1>Hej å</h1>") {
		t.Fatalf("not wrapped: %s", page)
	}
	if _, isErr := call(map[string]any{"html": strings.Repeat("x", 600<<10)}); !isErr {
		t.Fatal("over 512 kB accepted")
	}
}

// SPEC 0.5 V0.47: show {path} takes a folder or an HTML page with its
// files: stored once, entry inside the folder, links out of it named.
func TestShowFolder(t *testing.T) {
	e, id, call := showEngine(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "models"), 0o755)
	os.WriteFile(filepath.Join(root, "index.html"), []byte(`<script type="module" src="app.js"></script>`), 0o644)
	os.WriteFile(filepath.Join(root, "app.js"), []byte("import 'https://cdn.jsdelivr.net/npm/three'"), 0o644)
	os.WriteFile(filepath.Join(root, "models", "duck.glb"), []byte("glTF"), 0o644)
	os.WriteFile(filepath.Join(root, "viewer.html"), []byte("<p>two</p>"), 0o644)
	os.Symlink("/etc/passwd", filepath.Join(root, "passwd"))
	text, isErr := call(map[string]any{"path": root, "title": "Duck"})
	if isErr || !strings.Contains(text, "Shown") || !strings.Contains(text, "4 files") || !strings.Contains(text, "passwd") {
		t.Fatalf("%v %q", isErr, text)
	}
	call(map[string]any{"path": root + "/", "title": "Again"})
	text, isErr = call(map[string]any{"path": filepath.Join(root, "viewer.html")})
	if isErr {
		t.Fatal(text)
	}
	p := previews(t, e, id)
	if len(p) != 3 || p[0]["kind"] != "html" || p[0]["attachmentId"] != p[1]["attachmentId"] || p[2]["attachmentId"] == p[0]["attachmentId"] {
		t.Fatalf("%+v", p)
	}
	if _, ok := p[0]["height"]; ok {
		t.Fatalf("no size given, but a height was set: %+v", p[0])
	}
	m, _ := e.Attach.Get(p[0]["attachmentId"].(string))
	if m.Entry != "index.html" {
		t.Fatalf("%+v", m)
	}
	if m2, _ := e.Attach.Get(p[2]["attachmentId"].(string)); m2.Entry != "viewer.html" {
		t.Fatalf("%+v", m2)
	}
	if f, err := e.Attach.TreePath(m, "models/duck.glb"); err != nil {
		t.Fatal(err)
	} else if b, _ := os.ReadFile(f); string(b) != "glTF" {
		t.Fatalf("%q", b)
	}
	for _, entry := range []string{"../index.html", "/etc/passwd", "missing.html"} {
		if _, isErr := call(map[string]any{"path": root, "entry": entry}); !isErr {
			t.Fatalf("entry %q accepted", entry)
		}
	}
	if _, isErr := call(map[string]any{"path": t.TempDir()}); !isErr {
		t.Fatal("a folder without index.html was accepted")
	}
	// An HTML page in a folder over the limits is shown alone.
	big := t.TempDir()
	for i := 0; i < 501; i++ {
		os.WriteFile(filepath.Join(big, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(big, "page.html"), []byte("<p>alone</p>"), 0o644)
	text, isErr = call(map[string]any{"path": filepath.Join(big, "page.html")})
	if isErr || !strings.Contains(text, "alone") {
		t.Fatalf("%v %q", isErr, text)
	}
	if _, isErr := call(map[string]any{"path": big}); !isErr {
		t.Fatal("a folder over 500 files was accepted")
	}
}

// SPEC 0.5 V0.19: show {url} takes a screenshot at a named size; a rate limit
// of 10 a minute per thread.
func TestShowURL(t *testing.T) {
	e, id, call := showEngine(t)
	text, isErr := call(map[string]any{"url": "http://127.0.0.1:7777/", "size": "phone"})
	if isErr || !strings.Contains(text, "390×844") {
		t.Fatalf("%v %q", isErr, text)
	}
	if p := previews(t, e, id); len(p) != 1 || p[0]["kind"] != "image" || p[0]["width"] != 390.0 {
		t.Fatalf("%+v", p)
	}
	if _, isErr := call(map[string]any{"url": "file:///etc/passwd"}); !isErr {
		t.Fatal("a file URL was accepted")
	}
	for i := 0; i < 9; i++ {
		call(map[string]any{"url": "http://127.0.0.1/", "size": "1024x600"})
	}
	if text, isErr := call(map[string]any{"url": "http://127.0.0.1/"}); !isErr || !strings.Contains(text, "limit") {
		t.Fatalf("rate limit: %v %q", isErr, text)
	}
}

// SPEC 16.2: every tool schema is valid JSON Schema; one invalid schema
// made the CLI drop all Fyr tools (found live after the V0.19 deploy).
func TestToolSchemasValid(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	e := New(st)
	srv := mcp.New(func(string) (string, bool) { return "t", true })
	e.Tools(srv)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var out struct {
		Result struct {
			Tools []struct {
				Name        string
				InputSchema map[string]any
			}
		}
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Result.Tools) < 4 {
		t.Fatalf("tools %d", len(out.Result.Tools))
	}
	for _, tl := range out.Result.Tools {
		s := tl.InputSchema
		if s["type"] != "object" {
			t.Errorf("%s: type %v", tl.Name, s["type"])
		}
		if r, ok := s["required"]; ok {
			if _, isList := r.([]any); !isList {
				t.Errorf("%s: required must be an array, got %v", tl.Name, r)
			}
		}
		if _, ok := s["properties"].(map[string]any); !ok {
			t.Errorf("%s: properties must be an object", tl.Name)
		}
	}
}
