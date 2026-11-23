package mcp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// V0.review-robust (1): a body over the cap is answered 413, a body that is
// not JSON 400.
func TestBodyOverCapIs413(t *testing.T) {
	s := newServer()
	for body, want := range map[string]int{
		`{"jsonrpc":"2.0","id":1,"method":"` + strings.Repeat("x", 5<<20) + `"}`: http.StatusRequestEntityTooLarge,
		`{not json`: http.StatusBadRequest,
	} {
		r := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(body)))
		r.Header.Set("Authorization", "Bearer tok")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("body of %d bytes: code %d, want %d", len(body), w.Code, want)
		}
	}
}

// V0.review-robust (1): the tokens of ended sessions do not stay in the map
// of who read the tool list.
func TestListedTokensOfEndedSessionsArePruned(t *testing.T) {
	var mu sync.Mutex
	live := map[string]bool{}
	s := New(func(tok string) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return "t", live[tok]
	})
	s.Add(Tool{Name: "echo", Schema: map[string]any{"type": "object"},
		Call: func(context.Context, string, map[string]any) (string, error) { return "", nil }})
	for i := range 50 {
		tok := "tok" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		mu.Lock()
		live = map[string]bool{tok: true} // the session before this one ended
		mu.Unlock()
		rpc(t, s, tok, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
		if !s.Current(tok) {
			t.Fatalf("%s read the list but is not current", tok)
		}
	}
	s.mu.Lock()
	n := len(s.listed)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d tokens kept after 50 sessions, want only the live one", n)
	}
}
