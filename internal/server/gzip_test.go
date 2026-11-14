package server

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"strconv"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// SPEC 0.5 V0.38: text responses are gzipped when the client accepts it, the
// event stream too (flushed per event); other bodies pass unchanged.
func TestGzipJSONAndStream(t *testing.T) {
	hs, _ := newServer(t)
	req, _ := http.NewRequest("GET", hs.URL+"/api/state", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r, err := http.DefaultTransport.RoundTrip(req) // no transparent decoding
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(r.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("state not gzipped: %v", r.Header)
	}
	zr, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if !strings.Contains(string(b), `"buildId":"b1"`) {
		t.Fatalf("body %s", b)
	}

	// Without Accept-Encoding: plain.
	plain, _ := http.NewRequest("GET", hs.URL+"/api/state", nil)
	p, _ := http.DefaultTransport.RoundTrip(plain)
	if p.Header.Get("Content-Encoding") != "" {
		t.Fatal("gzipped without Accept-Encoding")
	}

	// The stream: the hello event arrives at once through gzip.
	sreq, _ := http.NewRequest("GET", hs.URL+"/api/stream", nil)
	sreq.Header.Set("Accept-Encoding", "gzip")
	s, err := http.DefaultTransport.RoundTrip(sreq)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Body.Close()
	if s.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("stream not gzipped: %v", s.Header)
	}
	done := make(chan string, 1)
	go func() {
		zs, err := gzip.NewReader(s.Body)
		if err != nil {
			done <- "err " + err.Error()
			return
		}
		line, _ := bufio.NewReader(zs).ReadString('\n')
		done <- line
	}()
	select {
	case l := <-done:
		if !strings.HasPrefix(l, "event: hello") {
			t.Fatalf("first line %q", l)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the hello event was not flushed through gzip")
	}
}

// SPEC 0.5 V0.38: GET /api/threads/{id}/events/{seq} returns one event whole.
func TestFullEvent(t *testing.T) {
	hs, e := newServer(t)
	r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	var rep struct{ ThreadID string }
	json.NewDecoder(r.Body).Decode(&rep)
	evs, _ := e.Events(rep.ThreadID, 0, 10)
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	g := do(t, "GET", hs.URL+"/api/threads/"+rep.ThreadID+"/events/"+strconv.FormatInt(evs[0].Seq, 10), "", nil, "")
	var got struct{ Seq int64 }
	json.NewDecoder(g.Body).Decode(&got)
	if g.StatusCode != 200 || got.Seq != evs[0].Seq {
		t.Fatalf("status %d seq %d", g.StatusCode, got.Seq)
	}
	if n := do(t, "GET", hs.URL+"/api/threads/"+rep.ThreadID+"/events/999999", "", nil, ""); n.StatusCode != 404 {
		t.Fatalf("missing event: %d", n.StatusCode)
	}
}
