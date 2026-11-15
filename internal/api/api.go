// Package api holds the wire shapes of the host protocol (docs/API-v0.md):
// commands and their replies. The engine, the server, the front and the
// provider registry share them; api imports only core.
package api

import "github.com/meros/fyr/internal/core"

// Cmd is one command from any surface.
type Cmd struct {
	CommandID string    `json:"commandId"`
	Action    string    `json:"action"`
	ThreadID  string    `json:"threadId,omitempty"`
	Args      core.Args `json:"args,omitempty"`
}

type Reply struct {
	OK       bool   `json:"ok"`
	ThreadID string `json:"threadId,omitempty"`
	Error    string `json:"error,omitempty"`
	Status   int    `json:"-"`
	Result   any    `json:"result,omitempty"`
}
