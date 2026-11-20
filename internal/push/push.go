// Package push sends Web Push notifications to the owner's devices
// (SPEC 0.5 V0.15): VAPID keys made once and kept in the database,
// subscriptions per user, and pruning of subscriptions the push service
// has dropped.
package push

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
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
	// NoFinished: this device gets no push for a thread that finished
	// (V0.notification-polish); what needs the owner still arrives.
	NoFinished bool `json:"noFinished,omitempty"`
}

// Prefs is what one device chose to be told about (V0.notification-polish).
type Prefs struct {
	// Finished: also a thread that finished and is unread.
	Finished bool `json:"finished"`
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
	// Host is the install id of the thread's host (decision 0031 F15,
	// V0.client-refs): with it the tap opens <host>/<threadId>, the one
	// thread of that host, though another host has a thread with the id.
	// "" from a host that reports no id; the tap then opens by id alone.
	Host  string `json:"host,omitempty"`
	State string `json:"state"`
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
	native  natives // V0.mac-notify: the native devices' queues
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

func (p *Pusher) Unsubscribe(endpoint string) error {
	if IsNative(endpoint) {
		p.dropNative(endpoint)
	}
	return p.DB.DeletePushSub(endpoint)
}

// SetHidden stores what the device behind endpoint hides (V0.207). It reports
// false when no such subscription is stored.
func (p *Pusher) SetHidden(endpoint string, h Hidden) (bool, error) {
	return p.edit(endpoint, func(s *Subscription) {
		s.Hidden = nil
		if len(h.Hosts) > 0 || len(h.Projects) > 0 {
			s.Hidden = &h
		}
	})
}

// SetPrefs stores what the device behind endpoint wants to be told about
// (V0.notification-polish). It reports false when no such subscription is
// stored.
func (p *Pusher) SetPrefs(endpoint string, pr Prefs) (bool, error) {
	return p.edit(endpoint, func(s *Subscription) { s.NoFinished = !pr.Finished })
}

// edit changes one stored subscription.
func (p *Pusher) edit(endpoint string, change func(*Subscription)) (bool, error) {
	raw, ok, err := p.DB.PushSubData(endpoint)
	if err != nil || !ok {
		return false, err
	}
	var s Subscription
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, err
	}
	change(&s)
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

// Notify sends to every subscription of a user that wants the payload and
// returns how many accepted it: not one that hides the payload's origin
// (V0.207), and not a finished thread to a device that turned those off
// (V0.notification-polish). A 404 or 410 answer deletes the subscription.
func (p *Pusher) Notify(user string, pl Payload) int {
	subs, err := p.Subscriptions(user)
	if err != nil {
		log.Printf("push: %v", err)
		return 0
	}
	ok := 0
	for _, s := range subs {
		if pl.Origin != nil && s.Hidden.Hides(*pl.Origin) {
			continue
		}
		if pl.State == "done" && s.NoFinished {
			continue
		}
		ok += p.deliver(s, pl)
	}
	return ok
}

// NotifyOne sends to the one subscription of a user with this endpoint
// (V0.notification-polish: the test goes to the device that asked) and
// returns how many accepted it.
func (p *Pusher) NotifyOne(user, endpoint string, pl Payload) int {
	subs, err := p.Subscriptions(user)
	if err != nil {
		log.Printf("push: %v", err)
		return 0
	}
	for _, s := range subs {
		if s.Endpoint == endpoint {
			return p.deliver(s, pl)
		}
	}
	return 0
}

// deliver sends pl to one subscription: 1 when the push service took it.
func (p *Pusher) deliver(s Subscription, pl Payload) int {
	body, _ := json.Marshal(pl)
	if IsNative(s.Endpoint) { // V0.mac-notify: the app reads it over the owner socket
		return p.deliverNative(s.Endpoint, pl, body)
	}
	var code int
	var err error
	if p.Send != nil {
		code, err = p.Send(s, body, p.keys)
	} else {
		code, err = p.webpushSend(s, body, p.keys, pl)
	}
	switch {
	case err != nil:
		log.Printf("push %s: %v", short(s.Endpoint), err)
	case code == 404 || code == 410:
		p.Unsubscribe(s.Endpoint)
		log.Printf("push %s: gone (%d), removed", short(s.Endpoint), code)
	case code >= 200 && code < 300:
		return 1
	default:
		log.Printf("push %s: status %d", short(s.Endpoint), code)
	}
	return 0
}

// Urgency is how hard the push service tries to reach a sleeping device
// (RFC 8030 5.3, V0.notification-polish): a finished thread waits for a
// device in low-battery mode; everything else wakes it.
func Urgency(pl Payload) webpush.Urgency {
	if pl.State == "done" {
		return webpush.UrgencyNormal
	}
	return webpush.UrgencyHigh
}

// Topic names a thread to the push service (RFC 8030 5.4), so a device that
// was offline gets the thread's newest push only, not every one sent while
// it was away. "" for a push about no thread. A topic is at most 32
// characters of the base64url alphabet, so it is a hash of the id.
func Topic(pl Payload) string {
	if pl.ThreadID == "" {
		return ""
	}
	id := pl.ThreadID
	if pl.Host != "" {
		id = pl.Host + "/" + id // two hosts' threads with one id are two topics
	}
	h := sha256.Sum256([]byte(id))
	return base64.RawURLEncoding.EncodeToString(h[:])[:32]
}

func (p *Pusher) webpushSend(s Subscription, body []byte, k Keys, pl Payload) (int, error) {
	resp, err := webpush.SendNotification(body, &webpush.Subscription{Endpoint: s.Endpoint,
		Keys: webpush.Keys{P256dh: s.Keys.P256dh, Auth: s.Keys.Auth}},
		&webpush.Options{Subscriber: p.Subject, VAPIDPublicKey: k.Public, VAPIDPrivateKey: k.Private,
			TTL: int((12 * time.Hour).Seconds()), Urgency: Urgency(pl), Topic: Topic(pl)})
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
// the title is long (design/mobile rule 12). V0.notification-polish: the
// text is one plain line (Plain), and a part that is empty leaves no
// separator behind.
func PayloadFor(threadID, title, state, meta, detail string) Payload {
	var parts []string
	for _, x := range []string{meta, Plain(detail)} {
		if x != "" {
			parts = append(parts, x)
		}
	}
	body := strings.Join(parts, " · ")
	r := []rune(body)
	if len(r) > 180 {
		body = string(r[:179]) + "…"
	}
	return Payload{Title: title, Body: body, ThreadID: threadID, State: state}
}

var plainMarks = strings.NewReplacer("**", "", "`", "")

// Plain is s as a notification shows it: one line, single spaces, without
// the marks of bold and code, which a notification would print as they are.
func Plain(s string) string {
	return strings.Join(strings.Fields(plainMarks.Replace(s)), " ")
}
