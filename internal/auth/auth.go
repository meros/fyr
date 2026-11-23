// Package auth holds a front's browser sessions (SPEC V0.130, decision
// 0028): every browser is a device with a session cookie, and the ways in
// (a one-time link from the owner socket, the tailnet through Serve's
// socket) only mint one. The front keeps only each session's hash.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/meros/fyr/internal/kvstore"
)

// Cookie is the session cookie's name.
const Cookie = "fyr_session"

// How a device got its session.
const (
	HowLocal   = "local"   // a one-time link from the owner socket (fyr open)
	HowTailnet = "tailnet" // the owner's tailnet login through Serve's socket
	HowLink    = "link"    // a one-time link from `fyr device add` or Add a device (decision 0030 3.4)
	HowCode    = "code"    // an approval code, typed on a signed-in device or to `fyr device approve`
)

const (
	onceTTL  = 60 * time.Second
	idleTTL  = 90 * 24 * time.Hour
	seenSave = time.Hour // lastSeen is written at most this often
	// V0.one-device-per-browser: a tailnet session nobody came back with is
	// handed to the same node and browser again for as long as it is unused, and
	// for reuseFor after its making in any case (a page's first requests arrive
	// together, all without the cookie). One that stays unused for unusedTTL is
	// removed.
	reuseFor  = time.Minute
	unusedTTL = time.Hour
	kvKey     = "auth.devices"
)

// Device is one browser that holds a session.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	How      string    `json:"how"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen"`
	Hash     string    `json:"hash"` // SHA-256 of the session, hex
	// Decision 0030 slice 2. All optional, so a V0.130 record is read as it is.
	Node    string `json:"node,omitempty"`    // the tailnet node it came from (its stable id)
	By      string `json:"by,omitempty"`      // the device that added or approved it
	Pending bool   `json:"pending,omitempty"` // has a session and a code, may do nothing until approved
	Code    string `json:"code,omitempty"`    // the approval code of a pending device
	// CodeUntil is when the code ends; the pending device goes with it.
	CodeUntil time.Time `json:"codeUntil,omitempty"`
}

type once struct {
	how, name, by string
	until         time.Time
}

// Store is a front's devices and its one-time links.
type Store struct {
	kv  kvstore.Table
	now func() time.Time

	mu      sync.Mutex
	devices map[string]*Device // by hash
	once    map[string]once    // by the link's token
	saved   map[string]time.Time

	// Decision 0030 slice 2: stored modes and what observe counts, the tailnet
	// nodes the owner approved or blocked, the audit trail, and the tries
	// against approval codes. See state.go.
	modes       map[string]*ModeState
	nodes       map[string]*NodeRec
	audit       []Audit
	wrong       []time.Time // recent wrong approval-code tries
	asked       []time.Time // recent approval attempts and code requests, for the rate limits
	onGone      []func(ids []string)
	onMode      []func(way, mode string)
	stepUp      time.Duration
	peerGate    func() (bool, string)
	peerLinksFn func() int
	onAdd       []func(d Device)

	enrolMu sync.Mutex        // one tailnet enrolment at a time: two requests of one page get one device
	handed  map[string]handed // the last session made for a node and browser, in memory only
}

// handed is a tailnet session as it was given out, kept so that the same
// browser can get it again while it has not come back with it.
type handed struct {
	session, hash string
	at            time.Time
}

// Open loads the devices from kv. now is time.Now when nil.
func Open(kv kvstore.Table, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	s := &Store{kv: kv, now: now, devices: map[string]*Device{}, once: map[string]once{}, saved: map[string]time.Time{}}
	raw, err := kv.GetKV(kvKey)
	if err != nil {
		return nil, err
	}
	if raw != "" {
		var list []*Device
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, err
		}
		for _, d := range list {
			s.devices[d.Hash] = d
			s.saved[d.Hash] = d.LastSeen
		}
	}
	if err := s.openState(); err != nil {
		return nil, err
	}
	s.DropUnused()
	return s, nil
}

func random(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(session string) string {
	sum := sha256.Sum256([]byte(session))
	return hex.EncodeToString(sum[:])
}

// Once makes a one-time link token for a device that will get its session
// the way how, valid for 60 s.
func (s *Store) Once(how, name string) string { return s.OnceFor(how, name, "", onceTTL) }

// OnceFor is Once for a link that lives ttl and adds a device on behalf of
// the device by ("" for the owner socket): `fyr device add` makes a 10 minute
// link, Add a device on a signed-in device the same (decision 0030 3.4).
func (s *Store) OnceFor(how, name, by string, ttl time.Duration) string {
	tok := random(16) // 128 bits
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, o := range s.once {
		if now.After(o.until) {
			delete(s.once, k)
		}
	}
	s.once[tok] = once{how: how, name: name, by: by, until: now.Add(ttl)}
	return tok
}

// ErrLink: the one-time link is unknown, used or too old.
var ErrLink = errors.New("that link no longer works: run fyr open again")

// Redeem uses a one-time link. A browser that holds a valid session (have)
// keeps it, and cookie is ""; else a new device is made and cookie is its
// session. agent names the browser (from its User-Agent).
func (s *Store) Redeem(tok, have, agent string) (cookie string, d *Device, err error) {
	s.mu.Lock()
	o, ok := s.once[tok]
	delete(s.once, tok)
	s.mu.Unlock()
	if !ok || s.now().After(o.until) {
		return "", nil, ErrLink
	}
	if d, ok := s.Lookup(have); ok && !d.Pending {
		return "", d, nil
	}
	name := o.name
	if agent != "" {
		name = agent + " · " + o.name
	}
	cookie, d = s.MintFor(o.how, name, "", o.by)
	return cookie, d, nil
}

// Mint makes a new device and returns its session.
func (s *Store) Mint(how, name string) (string, *Device) { return s.MintFor(how, name, "", "") }

// MintFor is Mint for a device that came from a tailnet node (stable id, ""
// for none) or was added by another device (its id, "" for none).
func (s *Store) MintFor(how, name, node, by string) (string, *Device) {
	session := random(32)
	now := s.now()
	d := &Device{ID: "dev_" + random(6), Name: name, How: how, Created: now, LastSeen: now, Hash: hash(session), Node: node, By: by}
	s.mu.Lock()
	s.devices[d.Hash] = d
	s.saveLocked()
	adds := s.onAdd
	cp := *d
	s.mu.Unlock()
	for _, f := range adds {
		f(cp)
	}
	return session, d
}

// mintNode is MintFor for the tailnet door: the node's browser gets the session
// it was last given while nobody has used that session, or within reuseFor of
// its making (V0.one-device-per-browser). A client that keeps no cookie (another
// install's probe, a script) so holds one device, not one for each request.
func (s *Store) mintNode(agent, node string) (string, *Device) {
	if node == "" {
		return s.MintFor(HowTailnet, agent, node, "")
	}
	s.enrolMu.Lock()
	defer s.enrolMu.Unlock()
	key := node + "\x00" + agent
	s.mu.Lock()
	if h, ok := s.handed[key]; ok {
		if d := s.devices[h.hash]; d != nil && !d.Pending && (d.LastSeen.Equal(d.Created) || s.now().Sub(h.at) < reuseFor) {
			cp := *d
			s.mu.Unlock()
			return h.session, &cp
		}
		delete(s.handed, key)
	}
	s.mu.Unlock()
	session, d := s.MintFor(HowTailnet, agent, node, "")
	s.mu.Lock()
	if s.handed == nil {
		s.handed = map[string]handed{}
	}
	s.handed[key] = handed{session: session, hash: d.Hash, at: d.Created}
	s.mu.Unlock()
	return session, d
}

// DropUnused removes the tailnet sessions that were made more than unusedTTL
// ago and never used: no request ever came back with the cookie, so no browser
// holds them (V0.one-device-per-browser). It returns how many it removed.
func (s *Store) DropUnused() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, n := s.now(), 0
	for h, d := range s.devices {
		if d.How == HowTailnet && !d.Pending && d.LastSeen.Equal(d.Created) && now.Sub(d.Created) > unusedTTL {
			delete(s.devices, h)
			delete(s.saved, h)
			n++
		}
	}
	if n > 0 {
		s.saveLocked()
		s.auditLocked("unused", "auto", fmt.Sprintf("%d tailnet sessions that no browser ever used were removed", n))
	}
	return n
}

// Lookup finds the device of a session and marks it seen. A session unseen
// for 90 days has ended.
func (s *Store) Lookup(session string) (*Device, bool) {
	if session == "" {
		return nil, false
	}
	h := hash(session)
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[h]
	if !ok {
		return nil, false
	}
	now := s.now()
	if d.Pending && now.After(d.CodeUntil) { // an unapproved device ends with its code
		delete(s.devices, h)
		s.saveLocked()
		return nil, false
	}
	if now.Sub(d.LastSeen) > idleTTL {
		delete(s.devices, h)
		s.saveLocked()
		return nil, false
	}
	first := d.LastSeen.Equal(d.Created) // the first use is written at once: it is what keeps the session (DropUnused)
	d.LastSeen = now
	if first || now.Sub(s.saved[h]) > seenSave {
		s.saveLocked()
	}
	c := *d
	return &c, true
}

// Devices lists the devices, newest first.
func (s *Store) Devices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

func (s *Store) saveLocked() {
	list := make([]*Device, 0, len(s.devices))
	for h, d := range s.devices {
		list = append(list, d)
		s.saved[h] = d.LastSeen
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	b, _ := json.Marshal(list)
	s.kv.SetKV(kvKey, string(b))
}

// PeerAllowed is the owner socket's rule (V0.130 (1)): fyrd's own user or
// root, by the peer's credentials, as tailscaled's LocalAPI does.
func PeerAllowed(peer, self int) bool { return peer == self || peer == 0 }
