package push

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Native devices (decision 0037, V0.mac-notify): Fyr.app's window is a
// WKWebView, which has no Web Push. The app is a subscription like any
// other (its own entry, what it hides, what it wants to be told about), but
// its endpoint is "native:<id>" and its pushes wait in a queue here, which
// the app reads over the owner socket and shows as macOS notifications.

// NativePrefix starts the endpoint of a native device.
const NativePrefix = "native:"

// IsNative reports whether endpoint is a native device's.
func IsNative(endpoint string) bool {
	return strings.HasPrefix(endpoint, NativePrefix) && len(endpoint) > len(NativePrefix)
}

// nativeKeep is how many pushes a native device that is not listening keeps,
// and nativeTTL how long, as a push service keeps a message for an offline
// device (webpushSend's TTL); the newest push of a thread replaces its older
// one (Topic).
const (
	nativeKeep     = 20
	nativeTTL      = 12 * time.Hour
	nativeQueueKey = "push.nativeQueue"
)

type nativeMsg struct {
	Topic string    `json:"topic,omitempty"`
	At    time.Time `json:"at"`
	Body  []byte    `json:"body"`
}

// nativeQueue is one native device: what waits for it, and its listener.
type nativeQueue struct {
	waiting []nativeMsg
	listen  chan []byte // nil while the app is not listening
}

type natives struct {
	mu     sync.Mutex
	loaded bool
	q      map[string]*nativeQueue
}

func (p *Pusher) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pusher) loadNativeLocked() {
	if p.native.loaded {
		return
	}
	p.native.loaded = true
	if p.DB == nil {
		return
	}
	raw, _ := p.DB.GetKV(nativeQueueKey)
	if raw == "" {
		return
	}
	var m map[string][]nativeMsg
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	cutoff := p.now().Add(-nativeTTL)
	for ep, ms := range m {
		var keep []nativeMsg
		for _, msg := range ms {
			if msg.At.After(cutoff) {
				keep = append(keep, msg)
			}
		}
		if len(keep) > nativeKeep {
			keep = keep[len(keep)-nativeKeep:]
		}
		if len(keep) > 0 {
			p.native.queue(ep).waiting = keep
		}
	}
}

func (p *Pusher) saveNativeLocked() {
	if p.DB == nil {
		return
	}
	cutoff := p.now().Add(-nativeTTL)
	out := map[string][]nativeMsg{}
	for ep, q := range p.native.q {
		var keep []nativeMsg
		for _, msg := range q.waiting {
			if msg.At.After(cutoff) {
				keep = append(keep, msg)
			}
		}
		q.waiting = keep
		if len(keep) > 0 {
			out[ep] = keep
		}
	}
	if len(out) == 0 {
		p.DB.SetKV(nativeQueueKey, "")
		return
	}
	if b, err := json.Marshal(out); err == nil {
		p.DB.SetKV(nativeQueueKey, string(b))
	}
}

func (n *natives) queue(endpoint string) *nativeQueue {
	if n.q == nil {
		n.q = map[string]*nativeQueue{}
	}
	q := n.q[endpoint]
	if q == nil {
		q = &nativeQueue{}
		n.q[endpoint] = q
	}
	return q
}

// NewNative stores a native device for user and returns its endpoint. An
// endpoint the app already holds and the store still knows is kept, so the
// device's settings survive the app's restarts.
func (p *Pusher) NewNative(user, endpoint, device string) (string, error) {
	if IsNative(endpoint) && p.Known(endpoint) {
		return endpoint, nil
	}
	var b [16]byte
	rand.Read(b[:])
	endpoint = NativePrefix + hex.EncodeToString(b[:])
	raw, _ := json.Marshal(Subscription{Endpoint: endpoint})
	return endpoint, p.DB.PutPushSub(user, endpoint, device, raw)
}

// Listen returns the pushes for a native device, those that waited first,
// until stop is called. A second listener replaces the first, whose channel
// closes.
func (p *Pusher) Listen(endpoint string) (<-chan []byte, func()) {
	p.native.mu.Lock()
	defer p.native.mu.Unlock()
	p.loadNativeLocked()
	q := p.native.queue(endpoint)
	if q.listen != nil {
		close(q.listen)
	}
	ch := make(chan []byte, nativeKeep)
	cutoff := p.now().Add(-nativeTTL)
	had := len(q.waiting) > 0
	for _, m := range q.waiting {
		if m.At.After(cutoff) {
			ch <- m.Body
		}
	}
	q.waiting = nil
	if had {
		p.saveNativeLocked()
	}
	q.listen = ch
	return ch, func() {
		p.native.mu.Lock()
		defer p.native.mu.Unlock()
		if q.listen == ch {
			close(ch)
			q.listen = nil
		}
	}
}

// deliverNative hands a push to a native device's listener, or keeps it
// until the app listens. It is 1, as a push service that took the message.
func (p *Pusher) deliverNative(endpoint string, pl Payload, body []byte) int {
	p.native.mu.Lock()
	defer p.native.mu.Unlock()
	p.loadNativeLocked()
	q := p.native.queue(endpoint)
	if q.listen != nil {
		select {
		case q.listen <- body:
			return 1
		default: // the app is not reading: keep it with the rest
		}
	}
	m := nativeMsg{Topic: Topic(pl), At: p.now(), Body: body}
	if m.Topic != "" {
		for i, w := range q.waiting {
			if w.Topic == m.Topic {
				q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
				break
			}
		}
	}
	q.waiting = append(q.waiting, m)
	if len(q.waiting) > nativeKeep {
		q.waiting = q.waiting[len(q.waiting)-nativeKeep:]
	}
	p.saveNativeLocked()
	return 1
}

// dropNative forgets a native device's queue (it was unsubscribed).
func (p *Pusher) dropNative(endpoint string) {
	p.native.mu.Lock()
	defer p.native.mu.Unlock()
	p.loadNativeLocked()
	if q := p.native.q[endpoint]; q != nil {
		if q.listen != nil {
			close(q.listen)
			q.listen = nil // its stop finds nothing more to close
		}
		had := len(q.waiting) > 0
		delete(p.native.q, endpoint)
		if had {
			p.saveNativeLocked()
		}
	}
}
