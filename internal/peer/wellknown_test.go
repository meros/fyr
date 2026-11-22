package peer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// V0.123: the answer names the install, its role, build and owner.
func TestWellKnownAnswer(t *testing.T) {
	s := httptest.NewServer(WellKnownHandler(func() WellKnown {
		return WellKnown{Fyr: 1, Name: "box", Role: "both", Build: "b1", Owner: "me@x", ID: "k1", Peer: 7780}
	}))
	defer s.Close()
	resp, err := http.Get(s.URL + "/.well-known/fyr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var w map[string]any
	json.NewDecoder(resp.Body).Decode(&w)
	if w["fyr"] != float64(1) || w["name"] != "box" || w["owner"] != "me@x" || w["role"] != "both" || w["build"] != "b1" || w["id"] != "k1" || w["peer"] != float64(7780) {
		t.Fatalf("%v", w)
	}
}

// A Mac's Tailscale Serve proxies to TCP only, so its port answers the
// well-known and nothing else, and refuses another tailnet user.
func TestOnlyWellKnownServesNothingElse(t *testing.T) {
	h := OnlyWellKnown("owner@example.com", func() WellKnown {
		return WellKnown{Fyr: 1, Name: "air", Owner: "owner@example.com", Host: "http://100.64.0.2:7778"}
	})
	do := func(method, path, login string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if login != "" {
			r.Header.Set("Tailscale-User-Login", login)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := do("GET", "/.well-known/fyr", "owner@example.com")
	var got WellKnown
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Name != "air" || got.Host != "http://100.64.0.2:7778" {
		t.Fatalf("well-known %d %s", w.Code, w.Body)
	}
	for _, c := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/api/state"}, {"POST", "/.well-known/fyr"}, {"GET", "/mcp"}} {
		if w := do(c.method, c.path, "owner@example.com"); w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	if w := do("GET", "/.well-known/fyr", "someone@example.com"); w.Code != http.StatusForbidden {
		t.Fatalf("another user: %d", w.Code)
	}
}

// V0.owner-without-tailnet: when owner is empty, OnlyWellKnown reads the owner
// dynamically from self().Owner (the roster) and refuses Tailscale logins until set.
func TestOnlyWellKnownDynamicOwner(t *testing.T) {
	owner := ""
	h := OnlyWellKnown("", func() WellKnown {
		return WellKnown{Fyr: 1, Name: "air", Owner: owner}
	})
	req := func(login string) int {
		r := httptest.NewRequest("GET", "/.well-known/fyr", nil)
		if login != "" {
			r.Header.Set("Tailscale-User-Login", login)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := req("owner@example.com"); code != http.StatusForbidden {
		t.Fatalf("unset owner accepted login: %d", code)
	}
	owner = "owner@example.com"
	if code := req("owner@example.com"); code != http.StatusOK {
		t.Fatalf("roster owner refused: %d", code)
	}
	if code := req("other@example.com"); code != http.StatusForbidden {
		t.Fatalf("other login accepted: %d", code)
	}
}
