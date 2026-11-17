// Package roster is the set of installs that know each other and the devices
// they have signed in (decision 0030 3.6, V0.158). It is a grow-only set of
// statements, each signed by a member install with its Ed25519 install key:
// `install-add`, `device-add`, `device-revoke`. Merge is union. An install
// is a member when it is this install, or when a member signed its add; a
// statement signed by a non-member is kept but counts for nothing, so every
// install that holds the same statements computes the same members whatever the
// order they arrived in. A revoke wins for ever. There is no owner root key and
// no CA: a handful of pinned keys need no chain.
package roster

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/meros/fyr/internal/identity"
)

// Statement kinds.
const (
	KindInstallAdd   = "install-add"
	KindDeviceAdd    = "device-add"
	KindDeviceRevoke = "device-revoke"
)

// Statement is one signed fact. Sig covers every other field.
type Statement struct {
	V    int    `json:"v"`
	Kind string `json:"kind"`
	// By is the id of the install that signed it.
	By string `json:"by"`
	// Subject is the install id (install-add) or the device id.
	Subject string    `json:"subject"`
	At      time.Time `json:"at"`
	// install-add: the member's public key, its name and where to reach its peer
	// port (a MagicDNS name or an address, and the port). Hints, not trust: the
	// handshake must prove the key.
	Key  []byte `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	// device-add: what the device is called and how it got in.
	Device string `json:"device,omitempty"`
	How    string `json:"how,omitempty"`
	Sig    []byte `json:"sig"`
}

// ID is the statement's identity: a hash of what it says and who signed it.
func (s Statement) ID() string {
	c := s
	c.Sig = nil
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

func (s Statement) signBytes() []byte {
	c := s
	c.Sig = nil
	b, _ := json.Marshal(c)
	return b
}

// Member is an install of the roster.
type Member struct {
	ID   string
	Key  ed25519.PublicKey
	Name string
	Host string
	Port int
}

// Roster holds the statements of one install.
type Roster struct {
	self *identity.Key
	path string
	now  func() time.Time

	mu       sync.Mutex
	stmts    map[string]Statement
	members  map[string]Member
	revoked  map[string]bool
	devices  map[string]Statement
	onChange []func()
}

// Open reads the roster in dir (roster.json), or starts one: the install is its
// own first member. self is this install's key; name, host and port say how it
// is reached (the port may be 0 until its listener is up: SetAddress).
func Open(dir string, self *identity.Key, name string, now func() time.Time) (*Roster, error) {
	if now == nil {
		now = time.Now
	}
	r := &Roster{self: self, path: filepath.Join(dir, "roster.json"), now: now, stmts: map[string]Statement{}}
	if b, err := os.ReadFile(r.path); err == nil {
		var list []Statement
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("%s: %w", r.path, err)
		}
		for _, s := range list {
			r.stmts[s.ID()] = s
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	r.mu.Lock()
	r.recomputeLocked()
	if r.members[self.ID()].ID == "" { // the first member is this install
		r.addLocked(r.sign(Statement{Kind: KindInstallAdd, Subject: self.ID(), Key: self.Public, Name: name}))
		r.recomputeLocked()
		r.saveLocked()
	}
	r.mu.Unlock()
	return r, nil
}

func (r *Roster) sign(s Statement) Statement {
	s.V, s.By, s.At = 1, r.self.ID(), r.now().UTC().Truncate(time.Second)
	s.Sig = ed25519.Sign(r.self.Private, s.signBytes())
	return s
}

func (r *Roster) addLocked(s Statement) bool {
	id := s.ID()
	if _, ok := r.stmts[id]; ok {
		return false
	}
	r.stmts[id] = s
	return true
}

// recomputeLocked finds the members: this install, and every install a member
// added, to a fixpoint; and the devices.
func (r *Roster) recomputeLocked() {
	members := map[string]Member{}
	selfID := r.self.ID()
	// This install is a member by its own word (its first statement), the root of
	// what it trusts.
	var adds []Statement
	for _, s := range r.stmts {
		if s.Kind == KindInstallAdd {
			adds = append(adds, s)
		}
	}
	sort.Slice(adds, func(i, j int) bool { return adds[i].At.Before(adds[j].At) })
	for _, s := range adds {
		if s.Subject == selfID && s.By == selfID && ed25519.PublicKey(s.Key).Equal(r.self.Public) && verifyWith(r.self.Public, s) {
			members[selfID] = memberOf(s)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, s := range adds {
			if _, in := members[s.Subject]; in {
				continue
			}
			signer, ok := members[s.By]
			if !ok || !verifyWith(signer.Key, s) || identity.IDOf(s.Key) != s.Subject {
				continue
			}
			members[s.Subject] = memberOf(s)
			changed = true
		}
	}
	// A later add of a member updates where to reach it (the newest signed by a
	// member wins), never its key.
	for _, s := range adds {
		m, in := members[s.Subject]
		if !in || !ed25519.PublicKey(s.Key).Equal(m.Key) {
			continue
		}
		if signer, ok := members[s.By]; ok && verifyWith(signer.Key, s) && s.Port != 0 {
			m.Name, m.Host, m.Port = s.Name, s.Host, s.Port
			members[s.Subject] = m
		}
	}
	revoked := map[string]bool{}
	devices := map[string]Statement{}
	for _, s := range r.stmts {
		if s.Kind != KindDeviceAdd && s.Kind != KindDeviceRevoke {
			continue
		}
		signer, ok := members[s.By]
		if !ok || !verifyWith(signer.Key, s) {
			continue
		}
		if s.Kind == KindDeviceRevoke {
			revoked[s.Subject] = true
		} else {
			devices[s.Subject] = s
		}
	}
	r.members, r.revoked, r.devices = members, revoked, devices
}

func memberOf(s Statement) Member {
	return Member{ID: s.Subject, Key: ed25519.PublicKey(s.Key), Name: s.Name, Host: s.Host, Port: s.Port}
}

func verifyWith(pub ed25519.PublicKey, s Statement) bool {
	return len(pub) == ed25519.PublicKeySize && len(s.Sig) == ed25519.SignatureSize && ed25519.Verify(pub, s.signBytes(), s.Sig)
}

func (r *Roster) saveLocked() {
	list := make([]Statement, 0, len(r.stmts))
	for _, s := range r.stmts {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].At.Equal(list[j].At) {
			return list[i].At.Before(list[j].At)
		}
		return list[i].ID() < list[j].ID()
	})
	b, _ := json.MarshalIndent(list, "", " ")
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, r.path)
	}
}

func (r *Roster) changed() {
	r.mu.Lock()
	fs := append([]func(){}, r.onChange...)
	r.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// OnChange registers f, called after the roster changed (a statement was added
// here or merged in).
func (r *Roster) OnChange(f func()) {
	r.mu.Lock()
	r.onChange = append(r.onChange, f)
	r.mu.Unlock()
}

// Self is this install's id.
func (r *Roster) Self() string { return r.self.ID() }

// Members lists the members, this install first, then by name.
func (r *Roster) Members() []Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Member, 0, len(r.members))
	for _, m := range r.members {
		out = append(out, m)
	}
	self := r.self.ID()
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ID == self) != (out[j].ID == self) {
			return out[i].ID == self
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Member is the member with that id.
func (r *Roster) Member(id string) (Member, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[id]
	return m, ok
}

// KeyMember is the member whose key this is.
func (r *Roster) KeyMember(pub ed25519.PublicKey) (Member, bool) {
	return r.Member(identity.IDOf(pub))
}

// Add records another install as a member, signed by this one: the result of
// pairing (the same tailnet user, a vouch is just sync, an invite). key is its
// public key; the id is derived from it.
func (r *Roster) Add(key ed25519.PublicKey, name, host string, port int) Statement {
	r.mu.Lock()
	s := r.sign(Statement{Kind: KindInstallAdd, Subject: identity.IDOf(key), Key: key, Name: name, Host: host, Port: port})
	if r.addLocked(s) {
		r.recomputeLocked()
		r.saveLocked()
		r.mu.Unlock()
		r.changed()
		return s
	}
	r.mu.Unlock()
	return s
}

// SetAddress says where this install's peer port is reached; other installs
// learn it with the next sync.
func (r *Roster) SetAddress(name, host string, port int) {
	r.mu.Lock()
	m := r.members[r.self.ID()]
	if m.Name == name && m.Host == host && m.Port == port {
		r.mu.Unlock()
		return
	}
	s := r.sign(Statement{Kind: KindInstallAdd, Subject: r.self.ID(), Key: r.self.Public, Name: name, Host: host, Port: port})
	r.addLocked(s)
	r.recomputeLocked()
	r.saveLocked()
	r.mu.Unlock()
	r.changed()
}

// DeviceAdd records a signed-in device so every member knows it.
func (r *Roster) DeviceAdd(id, name, how string) {
	r.mu.Lock()
	if _, ok := r.devices[id]; ok || r.revoked[id] {
		r.mu.Unlock()
		return
	}
	r.addLocked(r.sign(Statement{Kind: KindDeviceAdd, Subject: id, Device: name, How: how}))
	r.recomputeLocked()
	r.saveLocked()
	r.mu.Unlock()
	r.changed()
}

// DeviceRevoke records that a device is revoked; every member refuses it from
// then on, and a revoke is never undone.
func (r *Roster) DeviceRevoke(id string) {
	r.mu.Lock()
	if r.revoked[id] {
		r.mu.Unlock()
		return
	}
	r.addLocked(r.sign(Statement{Kind: KindDeviceRevoke, Subject: id}))
	r.recomputeLocked()
	r.saveLocked()
	r.mu.Unlock()
	r.changed()
}

// DeviceRevoked: a member revoked the device.
func (r *Roster) DeviceRevoked(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revoked[id]
}

// Devices lists the devices a member signed in that nobody revoked.
func (r *Roster) Devices() []Statement {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Statement
	for id, s := range r.devices {
		if !r.revoked[id] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Statements is everything held, for sync.
func (r *Roster) Statements() []Statement {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Statement, 0, len(r.stmts))
	for _, s := range r.stmts {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Merge takes statements from another member. A malformed one (a bad
// signature, a wrong version) is dropped; one from a signer that is not a member
// yet is kept, since a later statement can make the signer one. It reports
// whether anything new was kept.
func (r *Roster) Merge(list []Statement) bool {
	r.mu.Lock()
	added := false
	for _, s := range list {
		if len(r.stmts) > 20000 { // a flood from a stranger fills no disk
			break
		}
		if s.V != 1 || len(s.Sig) != ed25519.SignatureSize || s.By == "" || len(list) > 100000 {
			continue
		}
		switch s.Kind {
		case KindInstallAdd:
			if len(s.Key) != ed25519.PublicKeySize || identity.IDOf(s.Key) != s.Subject {
				continue
			}
		case KindDeviceAdd, KindDeviceRevoke:
			if s.Subject == "" {
				continue
			}
		default:
			continue
		}
		if r.addLocked(s) {
			added = true
		}
	}
	if added {
		r.recomputeLocked()
		r.saveLocked()
	}
	r.mu.Unlock()
	if added {
		r.changed()
	}
	return added
}
