package main

// SPEC V0.180 (#261): a refusal through the owner socket prints its error field, not
// the HTTP status line.

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/auth"
)

func TestOwnerCallPrintsTheErrorField(t *testing.T) {
	data, err := os.MkdirTemp("", "fyrcli")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(data)
	ln, err := net.Listen("unix", auth.SocketPath(data, auth.OwnerSocket))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"the install that made the invite refused (403): that invite is not open"}`))
	})}
	go srv.Serve(ln)
	defer srv.Close()
	_, err = ownerCall(data, "POST", "/pair/join", map[string]string{"code": "x"})
	if err == nil {
		t.Fatal("no error")
	}
	if got := err.Error(); got != "the install that made the invite refused (403): that invite is not open" {
		t.Errorf("printed %q", got)
	}
}

func TestOwnerCallWithoutAnErrorFieldNamesTheStatus(t *testing.T) {
	data, _ := os.MkdirTemp("", "fyrcli")
	defer os.RemoveAll(data)
	ln, err := net.Listen("unix", auth.SocketPath(data, auth.OwnerSocket))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})}
	go srv.Serve(ln)
	defer srv.Close()
	_, err = ownerCall(data, "GET", "/x", nil)
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("printed %v", err)
	}
}

// V0.owner-without-tailnet (#197): `fyr auth owner [LOGIN]` reads and sets the
// Tailscale owner login over the owner socket.
func TestAuthOwnerCommand(t *testing.T) {
	data, err := os.MkdirTemp("", "fyrcli")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(data)
	ln, err := net.Listen("unix", auth.SocketPath(data, auth.OwnerSocket))
	if err != nil {
		t.Fatal(err)
	}
	var saved string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/owner", func(w http.ResponseWriter, r *http.Request) {
		src := ""
		if saved != "" {
			src = "roster"
		}
		json.NewEncoder(w).Encode(map[string]any{"ownerLogin": saved, "source": src, "rosterOwner": saved})
	})
	mux.HandleFunc("POST /auth/owner", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Login string }
		json.NewDecoder(r.Body).Decode(&b)
		saved = b.Login
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "ownerLogin": saved, "source": "roster", "rosterOwner": saved})
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	if err := authCommand("auth", []string{"-data", data, "owner"}); err != nil {
		t.Fatalf("get empty: %v", err)
	}
	if err := authCommand("auth", []string{"-data", data, "owner", "owner@example.com"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if saved != "owner@example.com" {
		t.Fatalf("saved=%q", saved)
	}
	if err := authCommand("auth", []string{"-data", data, "owner"}); err != nil {
		t.Fatalf("get set: %v", err)
	}
	if err := authCommand("auth", []string{"-data", data, "owner", "a", "b"}); err == nil {
		t.Fatal("extra arg accepted")
	}
}
