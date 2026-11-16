package front

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// SPEC 0.5 V0.81: a client-only deploy swaps the served client and its
// buildId without a restart; open streams close, so each page reconnects,
// reads the new buildId in hello and reloads (V0.13). A client without
// index.html is refused and the old one stays.
func TestSetClientSwapsWithoutRestart(t *testing.T) {
	f := New(Options{Name: "front", BuildID: "b1", Web: fstest.MapFS{"index.html": {Data: []byte("v1")}}}, nil)
	hs := httptest.NewServer(f.Handler())
	defer func() { hs.CloseClientConnections(); hs.Close() }()
	get := func(path string) string {
		r, err := http.Get(hs.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return string(b)
	}
	if b := get("/"); b != "v1" {
		t.Fatalf("index %q", b)
	}
	resp, err := http.Get(hs.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	st := sse{bufio.NewReader(resp.Body)}
	if ev, data := st.next(t); ev != "hello" || !strings.Contains(data, `"buildId":"b1"`) {
		t.Fatalf("hello %s %s", ev, data)
	}

	if err := f.SetClient("b2", fstest.MapFS{"other.html": {Data: []byte("x")}}); err == nil {
		t.Fatal("a client without index.html was accepted")
	}
	if b := get("/"); b != "v1" {
		t.Fatalf("a refused client replaced the old one: %q", b)
	}

	if err := f.SetClient("b2", fstest.MapFS{"index.html": {Data: []byte("v2")}}); err != nil {
		t.Fatal(err)
	}
	// The open stream ends: the page reconnects.
	if _, err := io.ReadAll(st.rd); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
	if b := get("/"); b != "v2" {
		t.Fatalf("index after the switch %q", b)
	}
	if b := get("/api/state"); !strings.Contains(b, `"buildId":"b2"`) {
		t.Fatalf("state %s", b)
	}
	r2, err := http.Get(hs.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if ev, data := (sse{bufio.NewReader(r2.Body)}).next(t); ev != "hello" || !strings.Contains(data, `"buildId":"b2"`) {
		t.Fatalf("hello after the switch %s %s", ev, data)
	}
}
