package front

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/store"
)

// V0.mac-notify: Fyr.app stores itself as a device on the owner socket and
// reads its pushes there, one JSON line each.
func TestNativeDeviceOnTheOwnerSocket(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := &push.Pusher{DB: st, Subject: "mailto:x@example.com", Send: func(push.Subscription, []byte, push.Keys) (int, error) { return 201, nil }}
	if err := p.Init(); err != nil {
		t.Fatal(err)
	}
	f := New(Options{Name: "desk", Push: p}, nil)
	srv := httptest.NewServer(f.OwnerHandler("http://fyr.localhost:7777"))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/push/native", "application/json", strings.NewReader(`{"device":"Fyr.app on desk"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		OK       bool
		Endpoint string
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if !got.OK || !push.IsNative(got.Endpoint) {
		t.Fatalf("POST /push/native: %+v", got)
	}

	if r, _ := http.Get(srv.URL + "/push/native/stream?endpoint=native:unknown"); r.StatusCode != 404 {
		t.Fatalf("an unknown device's stream: %d", r.StatusCode)
	}

	p.Notify(core.Owner, push.Payload{Title: "waited", ThreadID: "t1", State: "asking"})
	s, err := http.Get(srv.URL + "/push/native/stream?endpoint=" + got.Endpoint)
	if err != nil || s.StatusCode != 200 {
		t.Fatalf("stream: %v %v", s, err)
	}
	defer s.Body.Close()
	lines := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(s.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func() push.Payload {
		t.Helper()
		select {
		case l := <-lines:
			var pl push.Payload
			if err := json.Unmarshal([]byte(l), &pl); err != nil {
				t.Fatalf("line %q: %v", l, err)
			}
			return pl
		case <-time.After(2 * time.Second):
			t.Fatal("no line")
		}
		return push.Payload{}
	}
	if pl := next(); pl.Title != "waited" {
		t.Fatalf("the waiting push: %+v", pl)
	}
	p.Notify(core.Owner, push.Payload{Title: "live", ThreadID: "t2", State: "failed"})
	if pl := next(); pl.Title != "live" {
		t.Fatalf("the live push: %+v", pl)
	}
}
