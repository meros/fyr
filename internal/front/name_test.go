// #40: the proxy's error handler names the host while load replaces the
// host's state. The race detector needs CGO, so the test leans on the
// runtime's own check: reading a map while another goroutine writes it
// stops the process with "concurrent map read and map write".
package front

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLinkNameDuringLoad(t *testing.T) {
	f := New(Options{}, []HostConfig{{URL: "http://127.0.0.1:1"}})
	l := f.links[0]
	st := map[string]any{"host": "h1", "actions": []any{}, "profiles": []any{}, "deploy": map[string]any{}, "user": "u"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20000; i++ {
			f.load(l, st)
		}
	}()
	for i := 0; i < 20000; i++ {
		w := httptest.NewRecorder()
		l.proxy.ErrorHandler(w, httptest.NewRequest("GET", "/x", nil), errors.New("down"))
		if i > 0 && i%1000 == 0 && !strings.Contains(w.Body.String(), "h1: down") && !strings.Contains(w.Body.String(), "127.0.0.1:1: down") {
			t.Fatalf("error names no host: %s", w.Body.String())
		}
	}
	wg.Wait()
	w := httptest.NewRecorder()
	l.proxy.ErrorHandler(w, httptest.NewRequest("GET", "/x", nil), errors.New("down"))
	if !strings.Contains(w.Body.String(), "h1: down") {
		t.Fatalf("after load: %s", w.Body.String())
	}
}
