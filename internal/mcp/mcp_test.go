package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newServer() *Server {
	s := New(func(token string) (string, bool) {
		if token == "tok" {
			return "thread-1", true
		}
		return "", false
	})
	s.Add(Tool{Name: "echo", Description: "Echo", Schema: map[string]any{"type": "object"},
		Call: func(_ context.Context, thread string, args map[string]any) (string, error) {
			return thread + ":" + args["text"].(string), nil
		}})
	return s
}

func rpc(t *testing.T, h http.Handler, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// SPEC 0.5 V0.12: a call with an unknown token is refused.
func TestUnknownTokenRefused(t *testing.T) {
	w, _ := rpc(t, newServer(), "bad", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if w.Code != 401 {
		t.Fatalf("code %d", w.Code)
	}
	w, _ = rpc(t, newServer(), "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if w.Code != 401 {
		t.Fatalf("no token: code %d", w.Code)
	}
}

// SPEC 0.5 V0.12: initialize, tools/list and tools/call bound to the token's thread.
func TestInitializeListCall(t *testing.T) {
	s := newServer()
	_, out := rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18"}})
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || res["capabilities"].(map[string]any)["tools"] == nil {
		t.Fatalf("initialize %v", out)
	}
	w, _ := rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if w.Code != 202 {
		t.Fatalf("notification code %d", w.Code)
	}
	_, out = rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "echo" {
		t.Fatalf("tools %v", tools)
	}
	_, out = rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "echo", "arguments": map[string]any{"text": "hi"}}})
	c := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if c["text"] != "thread-1:hi" {
		t.Fatalf("call %v", out)
	}
	_, out = rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "nope"}})
	if out["error"] == nil {
		t.Fatalf("unknown tool: %v", out)
	}
}

// SPEC V0.129 (2), decision 0028 slice 0: a browser always sends Origin on a
// cross-origin POST and an agent's MCP client sends none, so a call that
// carries one is refused, even with a valid token (MCP transports: servers
// MUST validate Origin).
func TestOriginRefused(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	for _, origin := range []string{"https://evil.example", "http://127.0.0.1:7777", "null"} {
		r := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer tok")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		newServer().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("Origin %q: code %d", origin, w.Code)
		}
	}
	if w, _ := rpc(t, newServer(), "tok", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}); w.Code != 200 {
		t.Fatalf("no Origin: code %d", w.Code)
	}
}

// V0.tool-reload: a GET opens the server stream with one
// tools/list_changed; while it is open the token counts as current, and
// Close ends it.
func TestStreamSendsListChanged(t *testing.T) {
	s := newServer()
	hs := httptest.NewServer(s)
	defer hs.Close()
	_, out := rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	caps := out["result"].(map[string]any)["capabilities"].(map[string]any)["tools"].(map[string]any)
	if caps["listChanged"] != true {
		t.Fatalf("initialize does not offer listChanged: %v", out)
	}
	if s.Current("tok") {
		t.Fatal("current before it read the tools")
	}

	req, _ := http.NewRequest("GET", hs.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("GET: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	if !bytes.Contains(buf[:n], []byte(`"method":"notifications/tools/list_changed"`)) {
		t.Fatalf("first event %q", buf[:n])
	}
	wait := func(want bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if s.Current("tok") == want {
				return
			}
		}
		t.Fatalf("Current = %v, want %v", !want, want)
	}
	wait(true)
	s.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
	wait(false)

	// An unknown token gets no stream.
	req, _ = http.NewRequest("GET", hs.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer bad")
	req.Header.Set("Accept", "text/event-stream")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 401 {
		t.Fatalf("bad token: %v %v", r, err)
	}
}

// V0.tool-reload: the hash names the listed tools; a token that read the
// list is current until the list changes.
func TestToolHash(t *testing.T) {
	s := newServer()
	h := s.Hash()
	if h == "" || h != newServer().Hash() {
		t.Fatal("the same tools hash differently")
	}
	rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if !s.Current("tok") {
		t.Fatal("not current after tools/list")
	}
	s.Retire("gone", "call echo")
	if s.Hash() != h {
		t.Fatal("a retired tool changed the hash")
	}
	s.Add(Tool{Name: "echo", Description: "Echo, changed", Schema: map[string]any{"type": "object"}})
	if s.Hash() == h || s.Current("tok") {
		t.Fatal("a changed description kept the hash")
	}
}

// V0.one-tool-vocabulary: a removed tool is not listed, and a call of it is a
// tool error that names its replacement.
func TestRetiredToolNamesItsReplacement(t *testing.T) {
	s := newServer()
	s.Retire("old_echo", "call echo instead.")
	_, r := rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if strings.Contains(fmt.Sprint(r), "old_echo") {
		t.Fatalf("listed: %v", r)
	}
	_, r = rpc(t, s, "tok", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "old_echo", "arguments": map[string]any{}}})
	if got := fmt.Sprint(r); !strings.Contains(got, "isError:true") || !strings.Contains(got, "Fyr removed the tool old_echo: call echo instead.") {
		t.Fatalf("call: %v", got)
	}
	if s.Has("old_echo") || !s.Has("echo") {
		t.Fatal("Has")
	}
}
