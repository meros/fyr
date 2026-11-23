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

	"github.com/meros/fyr/internal/httpguard"
)

// Instructions is the initialize answer's instructions: the key rules, for
// every agent of the session (V0.prompt-fits-setup). The appended system
// prompt reaches only the main agent; a subagent sees the tools and this.
const Instructions = `Fyr runs this session and shows it to the owner on desktop and phone. Use these tools for the actions they name instead of shell commands. The key rules, for subagents too:
- Asks are asynchronous: ask, run_request and change_request return at once, and the answer arrives later as a message that starts with [Fyr]. Go on with what does not depend on it, or end your turn; never do the same thing another way meanwhile.
- Never merge a pull request yourself (no gh pr merge): ask with change_request {change, ask: "merge"}. Never run sudo: call run_request {command, why, root: true}.
- A question or a choice for the owner goes on a card with ask, and a command the owner must run with run_request: never in your message's text.
- Fyr already watches pull requests and CI: read change_list or arm watch instead of polling gh.`

// Tool is one MCP tool. Call returns text for the model, or an error that
// the model sees as a tool error.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Call        func(ctx context.Context, thread string, args map[string]any) (string, error)
}

type Server struct {
	auth  func(token string) (thread string, ok bool)
	tools map[string]Tool
	// retired: a removed tool's name → the line that names its replacement
	// (V0.one-tool-vocabulary), for an agent that still calls it.
	retired map[string]string

	mu sync.Mutex
	// listed: token → the hash of the tool list it last read;
	// streams: token → its open server streams.
	listed  map[string]string
	streams map[string]int
	done    chan struct{}
	closed  bool
}

func New(auth func(token string) (string, bool)) *Server {
	return &Server{auth: auth, tools: map[string]Tool{}, retired: map[string]string{}, listed: map[string]string{}, streams: map[string]int{}, done: make(chan struct{})}
}

// Add registers a tool. Call it before serving: the list is the build's.
func (s *Server) Add(t Tool) { s.tools[t.Name] = t }

// Retire names a tool that was removed and what to call instead
// (V0.one-tool-vocabulary). It is not listed; a call of it is a tool error
// that says so, for an agent that read the old list.
func (s *Server) Retire(name, instead string) { s.retired[name] = instead }

// Has reports a listed tool of this name.
func (s *Server) Has(name string) bool {
	_, ok := s.tools[name]
	return ok
}

// Descriptions are every listed tool's description and schema, as JSON
// text, by name: what an agent reads of the tools.
func (s *Server) Descriptions() map[string]string {
	out := map[string]string{}
	for n, t := range s.tools {
		b, _ := json.Marshal(map[string]any{"description": t.Description, "inputSchema": t.Schema})
		out[n] = string(b)
	}
	return out
}

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
	if !httpguard.DecodeJSON(w, r, &req, 4<<20, "bad JSON-RPC") {
		return
	}
	if len(req.ID) == 0 { // a notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result, rpcErr := s.dispatch(r.Context(), thread, req)
	if req.Method == "tools/list" && rpcErr == nil {
		s.noteListed(tok)
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

// noteListed records that tok read this build's tools. A token first seen
// here is a new session, so it is also when the tokens of ended sessions,
// which auth no longer knows, are dropped: the map would otherwise grow by
// one entry per session for fyrd's lifetime.
func (s *Server) noteListed(tok string) {
	h := s.Hash()
	s.mu.Lock()
	_, known := s.listed[tok]
	s.listed[tok] = h
	var others []string
	if !known {
		for t := range s.listed {
			if t != tok {
				others = append(others, t)
			}
		}
	}
	s.mu.Unlock()
	var gone []string
	for _, t := range others { // auth outside s.mu: it may take its own locks
		if _, ok := s.auth(t); !ok {
			gone = append(gone, t)
		}
	}
	if len(gone) == 0 {
		return
	}
	s.mu.Lock()
	for _, t := range gone {
		delete(s.listed, t)
	}
	s.mu.Unlock()
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
			"instructions":    Instructions,
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
		if instead, gone := s.retired[p.Name]; !ok && gone {
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": "Fyr removed the tool " + p.Name + ": " + instead}}, "isError": true}, nil
		}
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
