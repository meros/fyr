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
	"sort"
	"sync"
	"time"
)

// Cookie is the session cookie's name.
const Cookie = "fyr_session"

// How a device got its session.
const (
	HowLocal   = "local"   // a one-time link from the owner socket (fyr open)
	HowTailnet = "tailnet" // the owner's tailnet login through Serve's socket
)

const (
	onceTTL  = 60 * time.Second
	idleTTL  = 90 * 24 * time.Hour
	seenSave = time.Hour // lastSeen is written at most this often
	kvKey    = "auth.devices"
)

// Device is one browser that holds a session.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	How      string    `json:"how"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen"`
	Hash     string    `json:"hash"` // SHA-256 of the session, hex
}

// KV is where the devices are kept (the store's kv table).
type KV interface {
	GetKV(string) (string, error)
	SetKV(string, string) error
}

type once struct {
	how, name string
	until     time.Time
}

// Store is a front's devices and its one-time links.
type Store struct {
	kv  KV
	now func() time.Time

	mu      sync.Mutex
	devices map[string]*Device // by hash
	once    map[string]once    // by the link's token
	saved   map[string]time.Time
}

// Open loads the devices from kv. now is time.Now when nil.
func Open(kv KV, now func() time.Time) (*Store, error) {
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
func (s *Store) Once(how, name string) string {
	tok := random(32)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, o := range s.once {
		if now.After(o.until) {
			delete(s.once, k)
		}
	}
	s.once[tok] = once{how: how, name: name, until: now.Add(onceTTL)}
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
	if d, ok := s.Lookup(have); ok {
		return "", d, nil
	}
	name := o.name
	if agent != "" {
		name = agent + " · " + o.name
	}
	cookie, d = s.Mint(o.how, name)
	return cookie, d, nil
}

// Mint makes a new device and returns its session.
func (s *Store) Mint(how, name string) (string, *Device) {
	session := random(32)
	now := s.now()
	d := &Device{ID: "dev_" + random(6), Name: name, How: how, Created: now, LastSeen: now, Hash: hash(session)}
	s.mu.Lock()
	s.devices[d.Hash] = d
	s.saveLocked()
	s.mu.Unlock()
	return session, d
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
	if now.Sub(d.LastSeen) > idleTTL {
		delete(s.devices, h)
		s.saveLocked()
		return nil, false
	}
	d.LastSeen = now
	if now.Sub(s.saved[h]) > seenSave {
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
