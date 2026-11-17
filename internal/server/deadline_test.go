// SPEC 0.5 V0.147: a stalled request answers 503 at its deadline and stays
// watched until its handler returns; the stream has no deadline.
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/lockwatch"
)

func TestV147StalledRequestAnswers503(t *testing.T) {
	readDeadline = 100 * time.Millisecond
	defer func() { readDeadline = ReadDeadline }()
	release := make(chan struct{})
	returned := make(chan struct{})
	h := withDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stream" {
			time.Sleep(200 * time.Millisecond)
			w.Write([]byte("streamed"))
			return
		}
		defer close(returned)
		<-release
	}))
	start := time.Now()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/load", nil))
	if rec.Code != http.StatusServiceUnavailable || time.Since(start) > 2*time.Second {
		t.Fatalf("%d after %s", rec.Code, time.Since(start))
	}
	watched := func() bool {
		for _, o := range lockwatch.Holders() {
			if o.Lock == "GET /api/load" {
				return true
			}
		}
		return false
	}
	if !watched() {
		t.Fatal("the stuck handler is no longer watched after its 503")
	}
	close(release)
	<-returned
	deadline := time.Now().Add(2 * time.Second)
	for watched() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if watched() {
		t.Fatal("still watched after it returned")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/stream", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "streamed") {
		t.Fatalf("the stream got a deadline: %d %q", rec.Code, rec.Body.String())
	}
}
