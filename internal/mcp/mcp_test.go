package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
