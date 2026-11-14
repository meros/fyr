package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	r := e.Command(Cmd{CommandID: "c", Action: "thread.create", Args: core.Args{"cwd": dir, "text": "x"}})
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
