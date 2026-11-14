package engine

import (
	"encoding/json"
	"sync"

	"github.com/meros/fyr/internal/store"
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

func (h *Hub) send(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
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

func (h *Hub) Thread(d DTO) { h.send("thread", d) }

func (h *Hub) Events(evs []store.Event) {
	for _, ev := range evs {
		h.send("event", ev)
	}
}
