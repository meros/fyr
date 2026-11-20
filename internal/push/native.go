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
	nativeKeep = 20
	nativeTTL  = 12 * time.Hour
)

type nativeMsg struct {
	topic string
	at    time.Time
	body  []byte
}

// nativeQueue is one native device: what waits for it, and its listener.
type nativeQueue struct {
	waiting []nativeMsg
	listen  chan []byte // nil while the app is not listening
}

type natives struct {
	mu sync.Mutex
	q  map[string]*nativeQueue
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
	q := p.native.queue(endpoint)
	if q.listen != nil {
		close(q.listen)
	}
	ch := make(chan []byte, nativeKeep)
	cutoff := time.Now().Add(-nativeTTL)
	for _, m := range q.waiting {
		if m.at.After(cutoff) {
			ch <- m.body
		}
	}
	q.waiting = nil
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
	q := p.native.queue(endpoint)
	if q.listen != nil {
		select {
		case q.listen <- body:
			return 1
		default: // the app is not reading: keep it with the rest
		}
	}
	m := nativeMsg{topic: Topic(pl), at: time.Now(), body: body}
	if m.topic != "" {
		for i, w := range q.waiting {
			if w.topic == m.topic {
				q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
				break
			}
		}
	}
	q.waiting = append(q.waiting, m)
	if len(q.waiting) > nativeKeep {
		q.waiting = q.waiting[len(q.waiting)-nativeKeep:]
	}
	return 1
}

// dropNative forgets a native device's queue (it was unsubscribed).
func (p *Pusher) dropNative(endpoint string) {
	p.native.mu.Lock()
	defer p.native.mu.Unlock()
	if q := p.native.q[endpoint]; q != nil {
		if q.listen != nil {
			close(q.listen)
			q.listen = nil // its stop finds nothing more to close
		}
		delete(p.native.q, endpoint)
	}
}
