// Package push sends Web Push notifications to the owner's devices
// (SPEC 0.5 V0.15): VAPID keys made once and kept in the database,
// subscriptions per user, and pruning of subscriptions the push service
// has dropped.
package push

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"slices"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/meros/fyr/internal/store"
)

type Keys struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

type SubKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// Subscription is the browser's PushSubscription.toJSON().
type Subscription struct {
	Endpoint string  `json:"endpoint"`
	Keys     SubKeys `json:"keys"`
	// Hidden is what this device never shows (V0.207).
	Hidden *Hidden `json:"hidden,omitempty"`
}

// HiddenProject is a project by its host's name and that host's own id. An
// empty Host matches any host.
type HiddenProject struct {
	Host string `json:"host"`
	ID   string `json:"id"`
}

// Hidden is the hosts and projects one device has hidden (V0.207).
type Hidden struct {
	Hosts    []string        `json:"hosts"`
	Projects []HiddenProject `json:"projects"`
}

// Origin says which host and project a push is about.
type Origin struct {
	Host      string
	ProjectID string
}

// Hides reports whether a device with this list leaves out a push from o.
func (h *Hidden) Hides(o Origin) bool {
	if h == nil {
		return false
	}
	if slices.Contains(h.Hosts, o.Host) {
		return true
	}
	return o.ProjectID != "" && slices.ContainsFunc(h.Projects, func(p HiddenProject) bool {
		return p.ID == o.ProjectID && (p.Host == "" || p.Host == o.Host)
	})
}

// Payload is what the service worker shows.
type Payload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	ThreadID string `json:"threadId"`
	State    string `json:"state"`
	// Origin is the thread's host and project; nil for a push about no
	// thread, which every device gets. It is not sent.
	Origin *Origin `json:"-"`
}

// Sender delivers one encrypted message and returns the HTTP status.
type Sender func(sub Subscription, body []byte, keys Keys) (int, error)

type Pusher struct {
	DB      *store.Store
	Subject string // VAPID subject, mailto: or https:
	Send    Sender
	keys    Keys
}

// Init loads the VAPID key pair, or makes and stores one.
func (p *Pusher) Init() error {
	raw, err := p.DB.GetKV("vapid")
	if err != nil {
		return err
	}
	if raw != "" {
		return json.Unmarshal([]byte(raw), &p.keys)
	}
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	p.keys = Keys{Public: pub, Private: priv}
	b, _ := json.Marshal(p.keys)
	return p.DB.SetKV("vapid", string(b))
}

func (p *Pusher) PublicKey() string { return p.keys.Public }

func (p *Pusher) Subscribe(user string, sub Subscription, device string) error {
	if sub.Endpoint == "" || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		return fmt.Errorf("subscription needs endpoint, keys.p256dh and keys.auth")
	}
	b, _ := json.Marshal(sub)
	return p.DB.PutPushSub(user, sub.Endpoint, device, b)
}

func (p *Pusher) Unsubscribe(endpoint string) error { return p.DB.DeletePushSub(endpoint) }

// SetHidden stores what the device behind endpoint hides (V0.207). It reports
// false when no such subscription is stored.
func (p *Pusher) SetHidden(endpoint string, h Hidden) (bool, error) {
	raw, ok, err := p.DB.PushSubData(endpoint)
	if err != nil || !ok {
		return false, err
	}
	var s Subscription
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, err
	}
	s.Hidden = nil
	if len(h.Hosts) > 0 || len(h.Projects) > 0 {
		s.Hidden = &h
	}
	b, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	return true, p.DB.SetPushSubData(endpoint, b)
}

// Known reports whether a subscription is still stored for any user.
func (p *Pusher) Known(endpoint string) bool {
	ok, err := p.DB.HasPushSub(endpoint)
	return err == nil && ok
}

func (p *Pusher) Subscriptions(user string) ([]Subscription, error) {
	rows, err := p.DB.PushSubs(user)
	if err != nil {
		return nil, err
	}
	out := make([]Subscription, 0, len(rows))
	for _, r := range rows {
		var s Subscription
		if json.Unmarshal(r.Sub, &s) == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

// Notify sends to every subscription of a user that does not hide the
// payload's origin and returns how many accepted it. A 404 or 410 answer
// deletes the subscription.
func (p *Pusher) Notify(user string, pl Payload) int {
	subs, err := p.Subscriptions(user)
	if err != nil {
		log.Printf("push: %v", err)
		return 0
	}
	body, _ := json.Marshal(pl)
	send := p.Send
	if send == nil {
		send = p.webpushSend
	}
	ok := 0
	for _, s := range subs {
		if pl.Origin != nil && s.Hidden.Hides(*pl.Origin) {
			continue
		}
		code, err := send(s, body, p.keys)
		switch {
		case err != nil:
			log.Printf("push %s: %v", short(s.Endpoint), err)
		case code == 404 || code == 410:
			p.Unsubscribe(s.Endpoint)
			log.Printf("push %s: gone (%d), removed", short(s.Endpoint), code)
		case code >= 200 && code < 300:
			ok++
		default:
			log.Printf("push %s: status %d", short(s.Endpoint), code)
		}
	}
	return ok
}

func (p *Pusher) webpushSend(s Subscription, body []byte, k Keys) (int, error) {
	resp, err := webpush.SendNotification(body, &webpush.Subscription{Endpoint: s.Endpoint,
		Keys: webpush.Keys{P256dh: s.Keys.P256dh, Auth: s.Keys.Auth}},
		&webpush.Options{Subscriber: p.Subject, VAPIDPublicKey: k.Public, VAPIDPrivateKey: k.Private,
			TTL: int((12 * time.Hour).Seconds()), Urgency: webpush.UrgencyHigh})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func short(s string) string {
	if len(s) > 48 {
		return s[:48] + "…"
	}
	return s
}

// PayloadFor builds the notification for a thread entering a state. The
// body starts with the inbox meta word, so the state reads first even when
// the title is long (design/mobile rule 12).
func PayloadFor(threadID, title, state, meta, detail string) Payload {
	body := detail
	if meta != "" {
		body = meta + " · " + detail
	}
	r := []rune(body)
	if len(r) > 180 {
		body = string(r[:179]) + "…"
	}
	return Payload{Title: title, Body: body, ThreadID: threadID, State: state}
}
