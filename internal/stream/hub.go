// Package stream is the live side of the host protocol (SPEC 0.5 V0.2,
// V0.38): a hub that fans frames out to subscribers, and the SSE stream that
// writes a subscription with threads as JSON Merge Patches. The host and the
// front (V0.51) both serve it.
package stream

import (
	"encoding/json"
	"sync"
)

// The frames a host sends besides the hello (docs/API-v0.md).
const (
	FrameThread  = "thread"
	FrameProject = "project"
	FrameDeploy  = "deploy"
	// FrameHostOrder is the owner's host order (V0.132), as a host keeps it.
	FrameHostOrder = "hostOrder"
	FrameEvent     = "event"
)

// Frame is one Server-Sent Event.
type Frame struct {
	Event string
	Data  []byte
}

// Hub fans out frames to stream subscribers. A subscriber that falls 512
// frames behind is dropped; its client reconnects and reloads the snapshot.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Frame]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan Frame]struct{}{}} }

func (h *Hub) Subscribe() chan Frame {
	ch := make(chan Frame, 512)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan Frame) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// DropAll closes every subscription: each client reconnects and reads a
// new hello (a client-only deploy's reload, V0.81).
func (h *Hub) DropAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

// Send encodes v and publishes it as one frame.
func (h *Hub) Send(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.Publish(event, b)
}

// Publish sends an encoded frame (a front relays its hosts' frames, V0.51).
func (h *Hub) Publish(event string, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- Frame{event, b}:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}
