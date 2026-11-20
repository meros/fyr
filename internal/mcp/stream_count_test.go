package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// firstWrite records whether the server counted the stream when its first
// event was written.
type firstWrite struct {
	*httptest.ResponseRecorder
	s       *Server
	seen    bool
	counted bool
	cancel  context.CancelFunc
}

func (w *firstWrite) Write(b []byte) (int, error) {
	if !w.seen {
		w.seen, w.counted = true, w.s.Current("tok")
		w.cancel()
	}
	return w.ResponseRecorder.Write(b)
}

// V0.mcp-stream-count: the stream counts as open before its first event goes
// out. Found as a flake on home-server: TestMCPStreamThroughTheMainPort read
// the event, then saw Current false because the count came after the flush.
func TestStreamCountsBeforeItsFirstEvent(t *testing.T) {
	s := New(func(tok string) (string, bool) { return "t1", tok == "tok" })
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/mcp", nil).WithContext(ctx)
	w := &firstWrite{ResponseRecorder: httptest.NewRecorder(), s: s, cancel: cancel}
	s.stream(w, r, "tok")
	if !w.seen || !w.counted {
		t.Fatalf("first event written while the stream did not count (seen %v)", w.seen)
	}
	if s.Current("tok") {
		t.Fatal("the stream still counts after it ended")
	}
}

var _ http.Flusher = (*firstWrite)(nil)
