package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meros/fyr/internal/httpguard"
	"github.com/meros/fyr/internal/mcp"
)

// V0.tool-reload: the agents' server stream gets through the main port's
// guard, deadlines and gzip as a stream: the first event arrives while it
// stays open.
func TestMCPStreamThroughTheMainPort(t *testing.T) {
	m := mcp.New(func(tok string) (string, bool) { return "t1", tok == "tok" })
	s := &Server{MCP: m}
	hs := httptest.NewServer(httpguard.Gzip(s.Handler()))
	defer hs.Close()
	req, _ := http.NewRequest("GET", hs.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("GET /mcp: %d, encoding %q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	if !bytes.Contains(buf[:n], []byte("notifications/tools/list_changed")) {
		t.Fatalf("first event %q", buf[:n])
	}
	if !m.Current("tok") {
		t.Fatal("the open stream does not count")
	}
	m.Close()
}
