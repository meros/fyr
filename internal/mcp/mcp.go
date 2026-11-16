// Package mcp is Fyr's MCP server for agents (SPEC 16.2 MCP binding,
// 0.5 V0.12). It speaks JSON-RPC over HTTP POST (the streamable HTTP
// transport, JSON responses only). Each request carries a per-session
// bearer token; the token decides which thread the call acts for.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
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
}

func New(auth func(token string) (string, bool)) *Server {
	return &Server{auth: auth, tools: map[string]Tool{}}
}

func (s *Server) Add(t Tool) { s.tools[t.Name] = t }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	thread, ok := s.auth(tok)
	if tok == "" || !ok {
		http.Error(w, "unknown session token", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
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
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fyr", "version": "0"},
			"instructions":    "Fyr runs this session. Use these tools for the actions they name instead of shell commands.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
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
		return map[string]any{"tools": list}, nil
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
