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
	// KindOwnerLogin says whose installs these are: the tailnet login of the
	// owner, from an install that knows it (an untagged node). A tagged install
	// has no login of its own and takes this one (decision 0030 3.4).
	KindOwnerLogin = "owner-login"
	// KindInstallRemove ends an install for ever (decision 0030 3.6): what it signed
	// counts for nothing, and the members only it vouched for are signed for again by
	// the member that removed it. KindKeyRotate moves an install to a new key, signed
	// by the old one, so a peer that was offline follows it.
	KindInstallRemove = "install-remove"
	KindKeyRotate     = "key-rotate"
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
	// AddedBy is the install that signed the add that made it a member: the one
	// that paired with it, or that vouched for it (this install itself for its
	// own first statement).
	AddedBy string
}

// Roster holds the statements of one install.
type Roster struct {
	self *identity.Key
	path string
	now  func() time.Time

	mu          sync.Mutex
	stmts       map[string]Statement
	members     map[string]Member
	revoked     map[string]bool
	devices     map[string]Statement
	owner       string
	removed     map[string]bool
	successor   map[string]string
	selfRemoved bool
	onChange    []func()
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

// recomputeLocked finds the members and what they said. This install is a member
// by its own word, the root of what it trusts; an install a member added is a
// member; a rotation moves a member to its new key; a removal by a member ends
// an install for ever, and what the removed one signed counts for nothing, so the
// members it alone vouched for drop out until a live member signs for them again.
// The sets only grow, so every install that holds the same statements computes the
// same members whatever the order they arrived in.
func (r *Roster) recomputeLocked() {
	selfID := r.self.ID()
	var adds, removes, rotates []Statement
	for _, s := range r.stmts {
		switch s.Kind {
		case KindInstallAdd:
			adds = append(adds, s)
		case KindInstallRemove:
			removes = append(removes, s)
		case KindKeyRotate:
			rotates = append(rotates, s)
		}
	}
	byTime := func(l []Statement) {
		sort.Slice(l, func(i, j int) bool { // by time, then by id: every install orders them alike
			if !l[i].At.Equal(l[j].At) {
				return l[i].At.Before(l[j].At)
			}
			return l[i].ID() < l[j].ID()
		})
	}
	byTime(adds)
	byTime(removes)
	byTime(rotates)
	// the keys installs signed their own first statement with
	genesis := map[string]ed25519.PublicKey{}
	for _, s := range adds {
		if s.By == s.Subject && identity.IDOf(s.Key) == s.Subject && verifyWith(ed25519.PublicKey(s.Key), s) {
			genesis[s.Subject] = ed25519.PublicKey(s.Key)
		}
	}
	removed := map[string]bool{} // R: the installs a member removed, and what rotated from them
	var members map[string]Member
	var retired map[string]string // an old id → the id its key rotated to
	for {
		members = map[string]Member{}
		retired = map[string]string{}
		if g, ok := genesis[selfID]; ok && g.Equal(r.self.Public) && !removed[selfID] {
			for _, s := range adds {
				if s.Subject == selfID && s.By == selfID {
					members[selfID] = memberOf(s)
				}
			}
		}
		// This install's own earlier keys: a rotation to this key, signed by the old
		// one, keeps what the old key signed.
		for _, s := range rotates {
			if ed25519.PublicKey(s.Key).Equal(r.self.Public) && !removed[s.Subject] {
				if old, ok := genesis[s.Subject]; ok && s.By == s.Subject && verifyWith(old, s) {
					for _, a := range adds {
						if a.Subject == s.Subject && a.By == s.Subject {
							members[s.Subject] = memberOf(a)
						}
					}
					retired[s.Subject] = selfID
				}
			}
		}
		for changed := true; changed; {
			changed = false
			for _, s := range adds {
				if _, in := members[s.Subject]; in || removed[s.Subject] {
					continue
				}
				signer, ok := members[s.By]
				if !ok || removed[s.By] || !verifyWith(signer.Key, s) || identity.IDOf(s.Key) != s.Subject {
					continue
				}
				members[s.Subject] = memberOf(s)
				changed = true
			}
			for _, s := range rotates {
				old, ok := members[s.Subject]
				newID := identity.IDOf(s.Key)
				if !ok || removed[s.Subject] || s.By != s.Subject || len(s.Key) != ed25519.PublicKeySize || !verifyWith(old.Key, s) {
					continue
				}
				if _, in := members[newID]; in {
					continue
				}
				m := old
				m.ID, m.Key, m.AddedBy = newID, ed25519.PublicKey(s.Key), s.By
				members[newID] = m
				retired[s.Subject] = newID
				changed = true
			}
		}
		grew := false
		for _, s := range removes {
			signer, ok := members[s.By]
			if !ok || removed[s.By] || !verifyWith(signer.Key, s) || removed[s.Subject] {
				continue
			}
			for id := s.Subject; id != ""; id = retired[id] { // an install is removed with every key it rotated to
				removed[id] = true
			}
			grew = true
		}
		if !grew {
			break
		}
	}
	// A later add of a member updates where to reach it: what the member itself last
	// said wins; a hint from another member (who paired with it) is used until it has
	// said. Never its key.
	own := map[string]bool{}
	for _, s := range adds {
		m, in := members[s.Subject]
		if !in || !ed25519.PublicKey(s.Key).Equal(m.Key) || s.Port == 0 {
			continue
		}
		signer, ok := members[s.By]
		if !ok || removed[s.By] || !verifyWith(signer.Key, s) {
			continue
		}
		self := s.By == s.Subject
		if own[s.Subject] && !self {
			continue
		}
		m.Name, m.Host, m.Port = s.Name, s.Host, s.Port
		members[s.Subject] = m
		own[s.Subject] = own[s.Subject] || self
	}
	// what signers said: any member that was one when it signed (a rotated key too)
	signers := map[string]Member{}
	for id, m := range members {
		if !removed[id] {
			signers[id] = m
		}
	}
	r.selfRemoved = removed[selfID]
	r.removed = removed
	r.successor = retired
	for id := range retired {
		delete(members, id) // the old key is no longer a member: its link is refused
	}
	for id := range removed {
		delete(members, id)
	}
	revoked := map[string]bool{}
	devices := map[string]Statement{}
	for _, s := range r.stmts {
		if s.Kind != KindDeviceAdd && s.Kind != KindDeviceRevoke {
			continue
		}
		signer, ok := signers[s.By]
		if !ok || !verifyWith(signer.Key, s) {
			continue
		}
		if s.Kind == KindDeviceRevoke {
			revoked[s.Subject] = true
		} else {
			devices[s.Subject] = s
		}
	}
	var ownerAt time.Time
	var ownerID string
	r.owner = ""
	for _, s := range r.stmts {
		if s.Kind != KindOwnerLogin || s.Subject == "" {
			continue
		}
		signer, ok := signers[s.By]
		if !ok || !verifyWith(signer.Key, s) {
			continue
		}
		if r.owner == "" || s.At.After(ownerAt) || (s.At.Equal(ownerAt) && s.ID() > ownerID) {
			r.owner, ownerAt, ownerID = s.Subject, s.At, s.ID()
		}
	}
	r.members, r.revoked, r.devices = members, revoked, devices
}

func memberOf(s Statement) Member {
	return Member{ID: s.Subject, Key: ed25519.PublicKey(s.Key), Name: s.Name, Host: s.Host, Port: s.Port, AddedBy: s.By}
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
	// where a signer's key can be found: a member, or an install-add that carries it (the
	// ones already held and the ones in this batch)
	keyOf := func(id string) ed25519.PublicKey {
		if m, ok := r.members[id]; ok {
			return m.Key
		}
		for _, s := range r.stmts {
			if s.Kind == KindInstallAdd && s.Subject == id && len(s.Key) == ed25519.PublicKeySize {
				return ed25519.PublicKey(s.Key)
			}
		}
		for _, s := range list {
			if s.Kind == KindInstallAdd && s.Subject == id && len(s.Key) == ed25519.PublicKeySize && identity.IDOf(s.Key) == id {
				return ed25519.PublicKey(s.Key)
			}
		}
		return nil
	}
	foreign := 0 // statements held whose signer is not a member: a stranger's flood must not fill the roster
	for _, s := range r.stmts {
		if _, ok := r.members[s.By]; !ok {
			foreign++
		}
	}
	for _, s := range list {
		if s.V != 1 || len(s.Sig) != ed25519.SignatureSize || s.By == "" || len(list) > 100000 {
			continue
		}
		switch s.Kind {
		case KindInstallAdd:
			if len(s.Key) != ed25519.PublicKeySize || identity.IDOf(s.Key) != s.Subject {
				continue
			}
		case KindInstallRemove:
			if s.Subject == "" {
				continue
			}
		case KindKeyRotate:
			if s.Subject == "" || s.By != s.Subject || len(s.Key) != ed25519.PublicKeySize {
				continue
			}
		case KindDeviceAdd, KindDeviceRevoke, KindOwnerLogin:
			if s.Subject == "" {
				continue
			}
		default:
			continue
		}
		// the signature must hold under a key this roster can name
		k := keyOf(s.By)
		if k == nil || !verifyWith(k, s) {
			continue
		}
		_, byMember := r.members[s.By]
		if !byMember {
			if foreign >= maxForeign { // a removal or revoke by a member is never dropped by this
				continue
			}
			foreign++
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

// maxForeign bounds the statements held from signers that are not members (they
// may become members through a later statement, so they are kept, within reason).
const maxForeign = 2000

// OwnerLogin is the owner's tailnet login as the roster carries it, "" when no
// member has said.
func (r *Roster) OwnerLogin() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.owner
}

// SetOwnerLogin records the owner's login (an install that knows it says so
// once; the other installs, a tagged one among them, learn it by sync).
func (r *Roster) SetOwnerLogin(login string) {
	r.mu.Lock()
	if login == "" || r.owner == login {
		r.mu.Unlock()
		return
	}
	r.addLocked(r.sign(Statement{Kind: KindOwnerLogin, Subject: login}))
	r.recomputeLocked()
	r.saveLocked()
	r.mu.Unlock()
	r.changed()
}

// SelfRemoved: a member removed this install. It serves nothing and pairs with no
// one until the owner pairs it again (with a new key).
func (r *Roster) SelfRemoved() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selfRemoved
}

// Removed: the install (or a key it rotated from) was removed.
func (r *Roster) Removed(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removed[id]
}

// Successor is the id the install rotated to, "" when it did not.
func (r *Roster) Successor(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.successor[id]
}

// Remove ends an install for ever: a statement signed by this install, and the
// members the removed one alone vouched for signed for again by this one in the
// same batch, so the owner keeps them. A device the removed install revoked stays
// revoked (a revoke is protective); a device it added is not kept.
func (r *Roster) Remove(id string) error {
	r.mu.Lock()
	if id == r.self.ID() {
		r.mu.Unlock()
		return errors.New("this install cannot remove itself: run the removal on another member")
	}
	if _, in := r.members[id]; !in {
		r.mu.Unlock()
		return errors.New("no such install in the roster")
	}
	before := map[string]Member{}
	for k, m := range r.members {
		before[k] = m
	}
	var oldRevokes []string
	for _, s := range r.stmts {
		if s.Kind == KindDeviceRevoke && (s.By == id || r.successor[id] == s.By) {
			oldRevokes = append(oldRevokes, s.Subject)
		}
	}
	r.addLocked(r.sign(Statement{Kind: KindInstallRemove, Subject: id}))
	r.recomputeLocked()
	for k, m := range before { // the members that fell with it, kept by this one
		if _, still := r.members[k]; !still && !r.removed[k] && k != id {
			r.addLocked(r.sign(Statement{Kind: KindInstallAdd, Subject: k, Key: m.Key, Name: m.Name, Host: m.Host, Port: m.Port}))
		}
	}
	r.recomputeLocked()
	for _, dev := range oldRevokes {
		r.addLocked(r.sign(Statement{Kind: KindDeviceRevoke, Subject: dev}))
	}
	r.recomputeLocked()
	r.saveLocked()
	r.mu.Unlock()
	r.changed()
	return nil
}

// Rotate records that this install moves to newKey: a statement signed by the old
// key (this install's current one). After the restart on the new key the roster is
// opened with it; the old key is refused from then on, and a peer that was offline
// follows the new one by this statement.
func (r *Roster) Rotate(newKey ed25519.PublicKey) Statement {
	r.mu.Lock()
	s := r.sign(Statement{Kind: KindKeyRotate, Subject: r.self.ID(), Key: newKey})
	r.addLocked(s)
	r.recomputeLocked()
	r.saveLocked()
	r.mu.Unlock()
	r.changed()
	return s
}

// CheckRotation reports whether stmt is a rotation of the install whose key is
// old: signed by that key, about that install, to the key `to`.
func CheckRotation(stmt Statement, old, to ed25519.PublicKey) bool {
	return stmt.Kind == KindKeyRotate && stmt.By == stmt.Subject && stmt.Subject == identity.IDOf(old) &&
		ed25519.PublicKey(stmt.Key).Equal(to) && verifyWith(old, stmt)
}

// RotationChain is the rotations that lead to this install's key, oldest first:
// what it shows in its certificate, so a peer that was offline (and knows only an
// older key) follows it.
func (r *Roster) RotationChain() []Statement {
	r.mu.Lock()
	defer r.mu.Unlock()
	var chain []Statement
	cur := r.self.Public
	for i := 0; i < 16; i++ { // a bounded walk back
		var found *Statement
		for _, s := range r.stmts {
			if s.Kind == KindKeyRotate && ed25519.PublicKey(s.Key).Equal(cur) {
				c := s
				found = &c
				break
			}
		}
		if found == nil {
			break
		}
		chain = append([]Statement{*found}, chain...)
		// the key before: from that install's own first statement
		var prev ed25519.PublicKey
		for _, s := range r.stmts {
			if s.Kind == KindInstallAdd && s.Subject == found.Subject && s.By == s.Subject {
				prev = ed25519.PublicKey(s.Key)
			}
		}
		if prev == nil {
			break
		}
		cur = prev
	}
	return chain
}

// ChainFrom follows a chain of rotations from the key `from`: each statement is
// signed by the key before it. It returns the key the chain ends at, false when
// a link does not hold.
func ChainFrom(chain []Statement, from ed25519.PublicKey) (ed25519.PublicKey, bool) {
	cur := from
	for _, s := range chain {
		next := ed25519.PublicKey(s.Key)
		if !CheckRotation(s, cur, next) {
			return nil, false
		}
		cur = next
	}
	return cur, len(chain) > 0
}

// HasRotationTo reports whether the roster file in dir holds a rotation to the key
// `to`, without opening it as an install: a restart after `fyr rotate-key` takes
// the waiting key into use only when the roster already says so.
func HasRotationTo(dir string, to ed25519.PublicKey) bool {
	b, err := os.ReadFile(filepath.Join(dir, "roster.json"))
	if err != nil {
		return false
	}
	var list []Statement
	if json.Unmarshal(b, &list) != nil {
		return false
	}
	for _, s := range list {
		if s.Kind == KindKeyRotate && ed25519.PublicKey(s.Key).Equal(to) {
			return true
		}
	}
	return false
}
