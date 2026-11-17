package auth

// Decision 0030 slice 2 (V0.157): what a front stores about who may sign in,
// besides the devices: a mode for each way in (observe or enforce), what
// observe counted, the tailnet nodes the owner approved or blocked, an audit
// trail, and the approval codes of devices that wait. A release never changes
// a mode (5.1): it moves by rule 3 (three clean days), by a signed-in device
// or by the owner socket, and a mode set back by hand stays.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The ways in a mode is stored for.
const (
	WayBrowser = "browser"
	WayPeer    = "peer"
)

// The two modes.
const (
	ModeObserve = "observe"
	ModeEnforce = "enforce"
)

// ReadyAfter is how long a way in must run in observe with nothing it would
// refuse before it enforces itself (rule 3, the owner's answer Q2).
const ReadyAfter = 3 * 24 * time.Hour

const (
	kvModes = "auth.modes"
	kvNodes = "auth.nodes"
	kvAudit = "auth.audit"

	codeTTL     = 5 * time.Minute
	maxWrong    = 5               // wrong approval tries that end every open code
	wrongWindow = 5 * time.Minute // ... within this time
	maxPerMin   = 10              // approval attempts and code requests a minute, per front
	maxPending  = 20              // devices waiting for approval at once
	auditKeep   = 200
	codeAlpha   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0 O 1 I: 32 symbols, 8 of them are 40 bits
)

// ModeState is one way in's stored mode and what its observation counted.
type ModeState struct {
	Mode string `json:"mode"`
	// Hand: the owner set it back to observe; rule 3 leaves it there until the
	// owner enforces again.
	Hand bool `json:"hand,omitempty"`
	// Since is when this observation began (a change of mode); LastRefuse the
	// last request the enforce rule would have refused. A clean day count runs
	// from the later of the two.
	Since      time.Time `json:"since"`
	LastRefuse time.Time `json:"lastRefuse,omitempty"`
	Refused    int64     `json:"refused"` // would-refuse count since Since
	// Success counts the sessions of each way a device got in (How), since Since:
	// rule 3 wants one of each way in use.
	Success map[string]int64 `json:"success,omitempty"`
	By      string           `json:"by,omitempty"`
	At      time.Time        `json:"at"`
}

// NodeRec is a tailnet node the owner approved or blocked.
type NodeRec struct {
	State  string    `json:"state"` // approved or blocked
	Name   string    `json:"name"`
	Device string    `json:"device,omitempty"` // the device it came with
	At     time.Time `json:"at"`
}

// Audit is one line of the trail: what changed, and who changed it.
type Audit struct {
	At     time.Time `json:"at"`
	Event  string    `json:"event"`
	By     string    `json:"by"`
	Detail string    `json:"detail,omitempty"`
}

func (s *Store) openState() error {
	s.modes, s.nodes = map[string]*ModeState{}, map[string]*NodeRec{}
	for _, c := range []struct {
		key string
		dst any
	}{{kvModes, &s.modes}, {kvNodes, &s.nodes}, {kvAudit, &s.audit}} {
		raw, err := s.kv.GetKV(c.key)
		if err != nil {
			return err
		}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), c.dst); err != nil {
				return fmt.Errorf("%s: %w", c.key, err)
			}
		}
	}
	for _, w := range []string{WayBrowser, WayPeer} {
		if s.modes[w] == nil || s.modes[w].Mode == "" {
			// No release changes a mode: a way in that has none stored is observe,
			// and has been observing since now.
			s.modes[w] = &ModeState{Mode: ModeObserve, Since: s.now(), At: s.now()}
		}
	}
	return nil
}

func (s *Store) saveJSON(key string, v any) {
	b, _ := json.Marshal(v)
	s.kv.SetKV(key, string(b))
}

func (s *Store) saveModesLocked() { s.saveJSON(kvModes, s.modes) }

func (s *Store) auditLocked(event, by, detail string) {
	s.audit = append(s.audit, Audit{At: s.now(), Event: event, By: by, Detail: detail})
	if len(s.audit) > auditKeep {
		s.audit = s.audit[len(s.audit)-auditKeep:]
	}
	s.saveJSON(kvAudit, s.audit)
}

// Mode is a way in's stored mode.
func (s *Store) Mode(way string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.modes[way]; m != nil {
		return m.Mode
	}
	return ModeObserve
}

// ErrWay: no such way in, or it cannot enforce yet.
var ErrWay = errors.New("that way in cannot be switched yet")

// SetMode moves a way in to a mode. by is "owner.sock", "device:<id>" or
// "auto". Observe is the way back: it stays until the owner enforces again.
// Only the browser way can enforce in this release (peer follows in slice 5).
func (s *Store) SetMode(way, mode, by string) error {
	if way != WayBrowser && way != WayPeer || (mode != ModeObserve && mode != ModeEnforce) {
		return fmt.Errorf("auth: %q %q: %w", way, mode, ErrWay)
	}
	if way == WayPeer && mode == ModeEnforce {
		return fmt.Errorf("auth: the peer way in cannot enforce before the peer link exists (decision 0030 slice 5): %w", ErrWay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.modes[way]
	if m.Mode == mode {
		return nil
	}
	now := s.now()
	*m = ModeState{Mode: mode, Since: now, At: now, By: by}
	if mode == ModeObserve && by != "auto" {
		m.Hand = true
	}
	s.saveModesLocked()
	s.auditLocked("mode", by, way+" is now "+mode)
	return nil
}

// WouldRefuse counts a request the enforce rule would refuse, in observe.
func (s *Store) WouldRefuse(way string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.modes[way]
	if m == nil || m.Mode != ModeObserve {
		return
	}
	now := s.now()
	m.Refused++
	if now.Sub(m.LastRefuse) > time.Minute { // the clock of a clean day, saved at most a minute apart
		m.LastRefuse = now
		s.saveModesLocked()
	}
}

// Success counts a session of a way a device got in, while observing.
func (s *Store) Success(way, how string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.modes[way]
	if m == nil || m.Mode != ModeObserve {
		return
	}
	if m.Success == nil {
		m.Success = map[string]int64{}
	}
	first := m.Success[how] == 0
	m.Success[how]++
	if first {
		s.saveModesLocked()
	}
}

// Ready says whether rule 3 would enforce a way in now, and why not.
func (s *Store) Ready(way string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyLocked(way)
}

func (s *Store) readyLocked(way string) (bool, string) {
	m := s.modes[way]
	switch {
	case m == nil || m.Mode != ModeObserve:
		return false, "already enforcing"
	case way != WayBrowser:
		return false, "this way in cannot enforce yet"
	case m.Hand:
		return false, "set back to observe by hand; it stays until you enforce it"
	}
	from := m.Since
	if m.LastRefuse.After(from) {
		from = m.LastRefuse
	}
	if left := ReadyAfter - s.now().Sub(from); left > 0 {
		return false, fmt.Sprintf("%s of clean days to go", left.Round(time.Minute))
	}
	// One success of each way a device in use got in.
	inUse := map[string]bool{}
	for _, d := range s.devices {
		if !d.Pending {
			inUse[d.How] = true
		}
	}
	var missing []string
	for how := range inUse {
		if m.Success[how] == 0 {
			missing = append(missing, how)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return false, "no sign-in seen yet by " + strings.Join(missing, ", ")
	}
	return true, ""
}

// Tick applies rule 3: a way in that has run three days in observe with
// nothing it would refuse, a success of each way in use, and no hand-set
// observe, moves to enforce by itself. It returns the ways it moved; the
// caller tells the devices.
func (s *Store) Tick() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var moved []string
	for _, way := range []string{WayBrowser} {
		if ok, _ := s.readyLocked(way); !ok {
			continue
		}
		now := s.now()
		*s.modes[way] = ModeState{Mode: ModeEnforce, Since: now, At: now, By: "auto"}
		s.saveModesLocked()
		s.auditLocked("mode", "auto", way+" moved to enforce after "+fmt.Sprint(int(ReadyAfter.Hours()/24))+" clean days; `fyr auth observe` goes back")
		moved = append(moved, way)
	}
	return moved
}

// ModeView is a way in as the owner sees it.
type ModeView struct {
	Way     string           `json:"way"`
	Mode    string           `json:"mode"`
	Hand    bool             `json:"hand"`
	Since   time.Time        `json:"since"`
	Refused int64            `json:"refused"`
	Success map[string]int64 `json:"success"`
	Ready   bool             `json:"ready"`
	Why     string           `json:"why,omitempty"`
	By      string           `json:"by,omitempty"`
}

// Modes lists the ways in.
func (s *Store) Modes() []ModeView {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ModeView
	for _, w := range []string{WayBrowser, WayPeer} {
		m := s.modes[w]
		ok, why := s.readyLocked(w)
		out = append(out, ModeView{Way: w, Mode: m.Mode, Hand: m.Hand, Since: m.Since, Refused: m.Refused, Success: m.Success, Ready: ok, Why: why, By: m.By})
	}
	return out
}

// Audit is the trail, newest last.
func (s *Store) Audit() []Audit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Audit(nil), s.audit...)
}

// NodeState is "approved", "blocked" or "" for a node the owner never decided.
func (s *Store) NodeState(node string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.nodes[node]; n != nil {
		return n.State
	}
	return ""
}

// ApproveNode records a tailnet node as the owner's device. A blocked node
// stays blocked: only unblocking (not built) lifts it.
func (s *Store) ApproveNode(node, name, device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approveNodeLocked(node, name, device)
}

func (s *Store) approveNodeLocked(node, name, device string) {
	if node == "" {
		return
	}
	if n := s.nodes[node]; n != nil && n.State == "blocked" {
		return
	}
	if n := s.nodes[node]; n != nil && n.State == "approved" && n.Device == device {
		return
	}
	s.nodes[node] = &NodeRec{State: "approved", Name: name, Device: device, At: s.now()}
	s.saveJSON(kvNodes, s.nodes)
}

// Empty: the install has no device at all, pending ones aside (Q4: the first
// tailnet node of a fresh install is let in at once).
func (s *Store) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if !d.Pending {
			return false
		}
	}
	return true
}

// Enrolled is what the tailnet door did with a node.
type Enrolled int

const (
	EnrolRefused Enrolled = iota // blocked
	EnrolSession                 // a session, usable at once
	EnrolPending                 // a session that waits for approval
)

// EnrolTailnet decides what the owner's tailnet node gets at serve.sock
// (decision 0030 3.4). node is its stable id, name its MagicDNS name. A
// blocked node gets nothing. An approved one a session in silence. A new one:
// in observe, the old way (a session, and the node approved: it signed in);
// in enforce, a session at once only while the install has no device (Q4),
// else a pending session and a code.
func (s *Store) EnrolTailnet(node, nodeName, agent string, enforce bool) (string, *Device, Enrolled) {
	switch s.NodeState(node) {
	case "blocked":
		return "", nil, EnrolRefused
	case "approved":
		session, d := s.MintFor(HowTailnet, agent, node, "")
		return session, d, EnrolSession
	}
	if !enforce || s.Empty() {
		session, d := s.MintFor(HowTailnet, agent, node, "")
		s.ApproveNode(node, nodeName, d.ID)
		return session, d, EnrolSession
	}
	session, d, err := s.Pending(HowTailnet, agent, node)
	if err != nil {
		return "", nil, EnrolRefused
	}
	return session, d, EnrolPending
}

// ErrBusy: too many requests a minute, or too many devices waiting.
var ErrBusy = errors.New("too many sign-in requests; wait a minute")

func (s *Store) rateLocked() bool {
	now := s.now()
	keep := s.asked[:0]
	for _, t := range s.asked {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	s.asked = keep
	if len(s.asked) >= maxPerMin {
		return false
	}
	s.asked = append(s.asked, now)
	return true
}

func newCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = codeAlpha[int(b[i])%len(codeAlpha)] // 256 is a multiple of 32: no bias
	}
	return string(b)
}

// Pending makes a device that waits: it holds a session and a code, can do
// nothing, and ends with the code in 5 minutes. A browser with no session
// shows the code; the owner types it on a signed-in device or to
// `fyr device approve` (how says what the device becomes).
func (s *Store) Pending(how, name, node string) (string, *Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rateLocked() {
		return "", nil, ErrBusy
	}
	waiting := 0
	for _, d := range s.devices {
		if d.Pending {
			waiting++
		}
	}
	if waiting >= maxPending {
		return "", nil, ErrBusy
	}
	session := random(32)
	now := s.now()
	d := &Device{ID: "dev_" + random(6), Name: name, How: how, Created: now, LastSeen: now, Hash: hash(session), Node: node,
		Pending: true, Code: newCode(), CodeUntil: now.Add(codeTTL)}
	s.devices[d.Hash] = d
	s.saveLocked()
	return session, d, nil
}

// ErrCode: the code is unknown, used, or has ended.
var ErrCode = errors.New("that code does not work (it may have ended)")

// ApproveCode lets in the pending device with that code. by is the signed-in
// device that approves ("" for the owner socket). Five wrong tries within five
// minutes end every open code; more than ten attempts a minute are refused.
func (s *Store) ApproveCode(code, by string) (*Device, error) {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rateLocked() {
		return nil, ErrBusy
	}
	now := s.now()
	keep := s.wrong[:0]
	for _, t := range s.wrong {
		if now.Sub(t) < wrongWindow {
			keep = append(keep, t)
		}
	}
	s.wrong = keep
	if len(s.wrong) >= maxWrong {
		return nil, ErrCode // the codes are ended
	}
	for _, d := range s.devices {
		if d.Pending && code != "" && d.Code == code && now.Before(d.CodeUntil) {
			return s.approveLocked(d, by)
		}
	}
	s.wrong = append(s.wrong, now)
	if len(s.wrong) >= maxWrong {
		// Every open code ends: a guesser gets no further.
		for h, d := range s.devices {
			if d.Pending {
				delete(s.devices, h)
			}
		}
		s.saveLocked()
		s.auditLocked("codes-ended", by, "five wrong approval tries")
	}
	return nil, ErrCode
}

// ApproveDevice lets in a pending device by its id (the Allow button).
func (s *Store) ApproveDevice(id, by string) (*Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.ID == id && d.Pending && s.now().Before(d.CodeUntil) {
			return s.approveLocked(d, by)
		}
	}
	return nil, ErrCode
}

func (s *Store) approveLocked(d *Device, by string) (*Device, error) {
	if by != "" {
		// Step-up (0028): widening trust needs a session older than the one
		// approved, or the owner socket.
		var approver *Device
		for _, x := range s.devices {
			if x.ID == by {
				approver = x
			}
		}
		if approver == nil || approver.Pending || !approver.Created.Before(d.Created) {
			return nil, errors.New("only a device that signed in before this one may approve it")
		}
	}
	d.Pending, d.Code, d.CodeUntil = false, "", time.Time{}
	if d.How != HowTailnet {
		d.How = HowCode
	}
	d.By = by
	if d.Node != "" {
		s.approveNodeLocked(d.Node, d.Name, d.ID)
	}
	s.saveLocked()
	cp := *d
	adds := s.onAdd
	defer func() {
		for _, f := range adds {
			go f(cp)
		}
	}()
	by2 := by
	if by2 == "" {
		by2 = "owner.sock"
	} else {
		by2 = "device:" + by2
	}
	s.auditLocked("approve", by2, d.ID+" ("+d.Name+")")
	c := *d
	return &c, nil
}

// Deny removes a pending device.
func (s *Store) Deny(id, by string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, d := range s.devices {
		if d.ID == id && d.Pending {
			delete(s.devices, h)
			s.saveLocked()
			s.auditLocked("deny", by, id)
			return true
		}
	}
	return false
}

// OnRevoke sets what hears of the devices a revoke ended: the front closes
// their streams and drops their push subscriptions.
func (s *Store) OnRevoke(f func(ids []string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onGone = append(s.onGone, f)
}

// OnDevice sets what hears of a device that is signed in (a session made, or a
// waiting one approved): the roster tells every install about it (decision 0030
// 3.4).
func (s *Store) OnDevice(f func(d Device)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onAdd = append(s.onAdd, f)
}

// Revoke ends a device's session, and with it the devices it added (the
// cascade of 0028), and blocks their tailnet nodes so they cannot enrol again.
// It returns the ids revoked. by is "owner.sock" or "device:<id>".
func (s *Store) Revoke(id, by string) []string {
	s.mu.Lock()
	found := false
	for _, d := range s.devices {
		found = found || d.ID == id
	}
	if !found {
		s.mu.Unlock()
		return nil
	}
	gone := map[string]bool{id: true}
	order := []string{id}
	for changed := true; changed; { // the devices it added, and theirs
		changed = false
		for _, d := range s.devices {
			if !gone[d.ID] && d.By != "" && gone[d.By] {
				gone[d.ID] = true
				order = append(order, d.ID)
				changed = true
			}
		}
	}
	for h, d := range s.devices {
		if !gone[d.ID] {
			continue
		}
		delete(s.devices, h)
		if d.Node != "" {
			s.nodes[d.Node] = &NodeRec{State: "blocked", Name: d.Name, Device: d.ID, At: s.now()}
		}
	}
	s.saveLocked()
	s.saveJSON(kvNodes, s.nodes)
	s.auditLocked("revoke", by, strings.Join(order, ", "))
	fs := append([]func([]string){}, s.onGone...)
	s.mu.Unlock()
	for _, f := range fs {
		f(order)
	}
	return order
}

// BindNode records the tailnet node a device came through, for a device whose
// record predates nodes (V0.130), and approves the node: it signed in during
// observe (decision 0030 5.2). A blocked node is left as it is.
func (s *Store) BindNode(deviceID, node, nodeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.ID == deviceID && d.Node == "" {
			d.Node = node
			s.saveLocked()
			s.approveNodeLocked(node, nodeName, deviceID)
			return
		}
	}
}

// DeviceView is a device as the owner sees it: no session hash.
type DeviceView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	How       string    `json:"how"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"lastSeen"`
	Node      string    `json:"node,omitempty"`
	By        string    `json:"by,omitempty"`
	Pending   bool      `json:"pending,omitempty"`
	Code      string    `json:"code,omitempty"`
	CodeUntil time.Time `json:"codeUntil,omitempty"`
}

// List is every device, the ones that wait last.
func (s *Store) List() []DeviceView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DeviceView, 0, len(s.devices))
	for _, d := range s.devices {
		if d.Pending && s.now().After(d.CodeUntil) {
			continue
		}
		out = append(out, DeviceView{ID: d.ID, Name: d.Name, How: d.How, Created: d.Created, LastSeen: d.LastSeen, Node: d.Node, By: d.By, Pending: d.Pending, Code: d.Code, CodeUntil: d.CodeUntil})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pending != out[j].Pending {
			return !out[i].Pending
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// Signed lists the devices that are signed in (not waiting), for the roster.
func (s *Store) Signed() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Device
	for _, d := range s.devices {
		if !d.Pending {
			out = append(out, *d)
		}
	}
	return out
}

// Has reports whether a device is signed in here.
func (s *Store) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.ID == id {
			return true
		}
	}
	return false
}
