package main

import (
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

// SPEC V0.130 (1): fyr open asks the owner socket for a one-time link.
func TestOnceURLAsksTheOwnerSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "owner.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/session/once" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"url":"http://fyr.localhost:7777/auth/once#abc"}`))
	}))
	u, err := onceURL(sock)
	if err != nil || u != "http://fyr.localhost:7777/auth/once#abc" {
		t.Fatalf("%q %v", u, err)
	}
	if _, err := onceURL(filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Fatal("no fyrd gave no error")
	}
}

func TestBrowserCommand(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "google-chrome" || name == "xdg-open" {
			return "/bin/" + name, nil
		}
		return "", &net.AddrError{}
	}
	if got := browserCommand("linux", true, "U", "", look); len(got) != 2 || got[0] != "/bin/google-chrome" || got[1] != "--app=U" {
		t.Fatalf("app: %v", got)
	}
	if got := browserCommand("linux", false, "U", "", look); got[len(got)-1] != "U" {
		t.Fatalf("plain: %v", got)
	}
	if got := browserCommand("linux", true, "U", "/opt/brave", look); got[0] != "/opt/brave" || got[1] != "--app=U" {
		t.Fatalf("FYR_BROWSER: %v", got)
	}
	if got := browserCommand("darwin", true, "U", "", look); got[0] != "open" {
		t.Fatalf("mac: %v", got)
	}
}
