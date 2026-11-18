package server

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/lockwatch"
)

// SPEC 0.5 V0.147: every request but the long-lived ones runs with a
// deadline. Past it the client gets 503 instead of hanging; the handler's
// goroutine runs on, watched, so the stall dump names it.
const (
	// ReadDeadline is a GET's deadline.
	ReadDeadline = 10 * time.Second
	// WriteDeadline is any other method's: a command may wait for its
	// effect (a merge, a deploy's start).
	WriteDeadline = 60 * time.Second
)

// longLived are the routes with no deadline: the stream, the agents' MCP
// calls, files and previews of any size.
func longLived(r *http.Request) bool {
	p := r.URL.Path
	return p == "/api/stream" || p == "/mcp" || strings.HasPrefix(p, "/mcp/") ||
		strings.HasPrefix(p, "/api/attachments") || strings.HasPrefix(p, "/api/previews/") ||
		strings.HasPrefix(p, "/api/updates/")
}

// DeadlineBody is the 503's body past a deadline. Its code, host-stalled,
// tells a front that the host is stuck, not gone (V0.181): the front marks it
// offline and serves its cached state until it answers again.
const DeadlineBody = `{"ok":false,"error":"Fyr did not answer in time: it is busy or stuck; the stall is logged","code":"` + api.StalledCode + `"}`

// WriteDeadline503 answers as a request past its deadline does.
func WriteDeadline503(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, DeadlineBody)
}

// readDeadline and writeDeadline are the deadlines in use; a test shortens them.
var readDeadline, writeDeadline = ReadDeadline, WriteDeadline

// withDeadline wraps next as V0.147 says.
func withDeadline(next http.Handler) http.Handler {
	msg := DeadlineBody
	// The watch ends when the handler returns, not at the 503: a handler
	// still stuck past its deadline is what the dump must show.
	watched := func(limit time.Duration) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			done := lockwatch.Start(r.Method+" "+r.URL.Path, limit)
			defer done()
			next.ServeHTTP(w, r)
		})
	}
	read := http.TimeoutHandler(watched(readDeadline), readDeadline, msg)
	write := http.TimeoutHandler(watched(writeDeadline), writeDeadline, msg)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case longLived(r):
			next.ServeHTTP(w, r)
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
			read.ServeHTTP(w, r)
		default:
			write.ServeHTTP(w, r)
		}
	})
}
