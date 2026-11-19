package engine

import (
	"encoding/json"
	"log"
	"time"

	"github.com/meros/fyr/internal/stream"
)

// hostOrderKey is the host kv key of the owner's host order (V0.132).
const hostOrderKey = "hostOrder"

// HostOrder is the owner's host order and when it was set; the newest wins.
type HostOrder struct {
	Names []string `json:"names"`
	// IDs: the same hosts by install id (V0.host-ids); missing from an order a
	// front before it set.
	IDs []string  `json:"ids,omitempty"`
	At  time.Time `json:"at"`
}

// loadHostOrder reads the stored order. Call it with e.mu held.
func (e *Engine) loadHostOrder() {
	raw, _ := e.st.GetKV(hostOrderKey)
	if raw == "" {
		return
	}
	var o HostOrder
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		log.Printf("host order: %v", err)
		return
	}
	e.hostOrder = o
}

// HostOrder is the order this host keeps (empty: none set yet).
func (e *Engine) HostOrder() HostOrder {
	e.mu.Lock()
	defer e.mu.Unlock()
	o := e.hostOrder
	o.Names = append([]string(nil), o.Names...)
	o.IDs = append([]string(nil), o.IDs...)
	if len(o.IDs) == 0 {
		o.IDs = nil
	}
	return o
}

// setHostOrder keeps o when it is newer than the order kept, and tells the
// fronts on the stream; an older one changes nothing. It returns the order kept.
func (e *Engine) setHostOrder(o HostOrder) (HostOrder, error) {
	e.mu.Lock()
	if !o.At.After(e.hostOrder.At) {
		cur := e.hostOrder
		e.mu.Unlock()
		return cur, nil
	}
	e.mu.Unlock()
	b, _ := json.Marshal(o)
	if err := e.st.SetKV(hostOrderKey, string(b)); err != nil {
		return HostOrder{}, err
	}
	e.mu.Lock()
	e.hostOrder = o
	e.mu.Unlock()
	if e.Hub != nil {
		e.Hub.Send(stream.FrameHostOrder, o)
	}
	return o, nil
}
