// Package mcp is Fyr's MCP server for agents (SPEC 16.2 MCP binding,
// 0.5 V0.12). It speaks JSON-RPC over HTTP POST (the streamable HTTP
// transport, JSON responses only). Each request carries a per-session
// bearer token; the token decides which thread the call acts for.
//
// V0.tool-reload: a GET opens the transport's server stream. Fyr sends one
// notifications/tools/list_changed on it when it opens and nothing else
// but keepalives. When fyrd restarts for an update, the stream breaks; the
// agent's CLI connects to the new fyrd, initializes again and reads the
// new build's tool list, so a process that outlives fyrd (V0.90) gets the
// new tools without a restart.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tool is one MCP tool. Call returns text for the model, or an error that
// the model sees as a tool error.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Call        func(ctx context.Context, thread string, args map[string]any) (string, error)
	// Hidden: an alias kept for one release (V0.101): not listed, still
	// callable.
	Hidden bool
}

type Server struct {
	auth  func(token string) (thread string, ok bool)
	tools map[string]Tool

	mu sync.Mutex
	// listed: token → the hash of the tool list it last read;
	// streams: token → its open server streams.
	listed  map[string]string
	streams map[string]int
	done    chan struct{}
	closed  bool
}

func New(auth func(token string) (string, bool)) *Server {
	return &Server{auth: auth, tools: map[string]Tool{}, listed: map[string]string{}, streams: map[string]int{}, done: make(chan struct{})}
}

// Add registers a tool. Call it before serving: the list is the build's.
func (s *Server) Add(t Tool) { s.tools[t.Name] = t }

// Keepalive is how often an open server stream gets a comment line, so a
// proxy or the CLI does not take a quiet stream for a dead one.
var Keepalive = 25 * time.Second

// list is the listed tools, sorted by name.
func (s *Server) list() []any {
	names := make([]string, 0, len(s.tools))
	for n := range s.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	list := []any{}
	for _, n := range names {
		t := s.tools[n]
		if t.Hidden {
			continue
		}
		list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
	}
	return list
}

// Hash names this build's tool list (V0.tool-reload): names, descriptions
// and schemas. Two builds with the same tools have the same hash.
func (s *Server) Hash() string {
	b, _ := json.Marshal(s.list()) // map keys marshal sorted
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Current reports whether the process with this token has this build's
// tools: it read them from this fyrd, or it holds a server stream open,
// which told it to read them when it opened.
func (s *Server) Current(token string) bool {
	h := s.Hash()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[token] > 0 || s.listed[token] == h
}

// Close ends the open server streams, for shutdown: each CLI sees its
// stream end and connects again to the next fyrd.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// V0.129 (2), decision 0028: an agent's MCP client sends no Origin; a
	// browser always does on a cross-origin POST. The MCP transport spec
	// asks servers to validate it, against DNS rebinding and CSRF.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "Fyr's MCP endpoint serves agents, not web pages", http.StatusForbidden)
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	thread, ok := s.auth(tok)
	if tok == "" || !ok {
		http.Error(w, "unknown session token", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.stream(w, r, tok)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		http.Error(w, "bad JSON-RPC", http.StatusBadRequest)
		return
	}
	if len(req.ID) == 0 { // a notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result, rpcErr := s.dispatch(r.Context(), thread, req)
	if req.Method == "tools/list" && rpcErr == nil {
		s.mu.Lock()
		s.listed[tok] = s.Hash()
		s.mu.Unlock()
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// stream is the transport's server stream (V0.tool-reload): one
// tools/list_changed when it opens, then keepalives until the client leaves
// or the server closes.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, tok string) {
	rc := http.NewResponseController(w)
	// V0.mcp-stream-count: the stream counts before its first event goes out,
	// so a client that has read it is never told it is not connected.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.streams[tok]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.streams[tok]--; s.streams[tok] <= 0 {
			delete(s.streams, tok)
		}
		s.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")); err != nil {
		return
	}
	if rc.Flush() != nil {
		return
	}
	tick := time.NewTicker(Keepalive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-tick.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

func rpcError(code int, msg string) map[string]any {
	return map[string]any{"code": code, "message": msg}
}

func (s *Server) dispatch(ctx context.Context, thread string, req request) (any, map[string]any) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "fyr", "version": "0"},
			"instructions":    "Fyr runs this session. Use these tools for the actions they name instead of shell commands.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.list()}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, rpcError(-32602, "bad params")
		}
		t, ok := s.tools[p.Name]
		if !ok {
			return nil, rpcError(-32602, "unknown tool "+p.Name)
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		text, err := t.Call(ctx, thread, p.Arguments)
		if err != nil {
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}, nil
	}
	return nil, rpcError(-32601, "method not found: "+req.Method)
}
