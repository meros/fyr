package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/meros/fyr/internal/api"
)

// V0.forwarded-ports: a front carries a browser's connection to a server
// that a thread's own process runs on this host's loopback. The host decides
// which ports: only those the thread's processes listen on.

// forwardDial reaches a loopback port; a test points it elsewhere.
var forwardDial = func(port int) (net.Conn, error) {
	var last error
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// threadPorts is the ports the thread's processes listen on; none when the
// host cannot tell.
func (s *Server) threadPorts(thread string) []int { return s.Engine.ThreadPorts(thread) }

// ports lists what a link in this thread may reach.
func (s *Server) ports(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	api.WriteJSON(w, 200, map[string]any{"ports": s.threadPorts(t.ID)})
}

// errNotThreadPort is why a port is refused.
var errNotThreadPort = errors.New("no process of this thread listens on that port")

// forward is one connection to a thread's server: the request's body is what
// the browser sends, the response's body what the server answers, both
// streamed until either side ends. The port is checked on every connection,
// so a forward ends with the server it was opened for.
func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	// Before anything is written: over HTTP/1.1 (the in-process link) the
	// server would otherwise read the request's body to its end before it
	// sends an answer, and the body of a connection has no end. HTTP/2 reads
	// and writes at once anyway.
	rc := http.NewResponseController(w)
	rc.EnableFullDuplex()
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port < 1 || port > 65535 || !slices.Contains(s.threadPorts(t.ID), port) {
		api.WriteJSON(w, 404, map[string]any{"error": errNotThreadPort.Error()})
		return
	}
	conn, err := forwardDial(port)
	if err != nil {
		api.WriteJSON(w, 502, map[string]any{"error": fmt.Sprintf("port %d does not answer: %v", port, err)})
		return
	}
	defer conn.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if rc.Flush() != nil {
		return
	}
	// The browser's side ended, or the request was cancelled: the server's
	// connection closes, which ends the copy below.
	go func() {
		io.Copy(conn, r.Body)
		conn.Close()
	}()
	buf := make([]byte, 32<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil || rc.Flush() != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
