package peer

// The owner's invite (decision 0030 3.6 condition 3, V0.159): the way a tagged
// install, or a host on a LAN with no tailnet, joins. `fyr pair` (or Settings →
// Hosts → Pair) on a member shows `fyr1-<install id>-<10 characters>` as text, a
// QR code and a link; 10 minutes, single use, five wrong tries end it. The other
// side pins the id from the code, dials, and sends the secret inside the channel
// that is already pinned to that id, so a relay with a key of its own cannot
// complete it. No PAKE and no short code: the code carries the id.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/peerlink"
)

const (
	// InviteTTL is how long an invite lives.
	InviteTTL = 10 * time.Minute
	// MaxInviteTries is how many wrong secrets end an invite.
	MaxInviteTries = 5
	// DefaultPort is the peer port tried on a candidate when nothing says another.
	DefaultPort = 7779

	inviteAlpha  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0, O, 1, I: 32 symbols, 10 of them are 50 bits
	invitePrefix = "fyr1-"
)

// ErrInvite: the invite does not work (unknown, used, ended or wrong).
var ErrInvite = errors.New("that invite does not work (it may have ended or been used)")

// Invites is the invite a member has open. There is one at a time: a new one
// ends the old.
type Invites struct {
	Now func() time.Time

	mu  sync.Mutex
	cur *invite
}

type invite struct {
	secret string
	until  time.Time
	tries  int
	used   bool
}

func (iv *Invites) now() time.Time {
	if iv.Now != nil {
		return iv.Now()
	}
	return time.Now()
}

func randomSecret() string {
	b := make([]byte, 10)
	rand.Read(b)
	for i := range b {
		b[i] = inviteAlpha[int(b[i])%len(inviteAlpha)] // 256 is a multiple of 32: no bias
	}
	return string(b)
}

// New opens an invite for the install with this id; the code is what the other
// side types.
func (iv *Invites) New(id string) (code string, until time.Time) {
	iv.mu.Lock()
	defer iv.mu.Unlock()
	iv.cur = &invite{secret: randomSecret(), until: iv.now().Add(InviteTTL)}
	return invitePrefix + id + "-" + iv.cur.secret, iv.cur.until
}

// Redeem checks a secret: it must be the live invite's, unused and not ended.
// A wrong secret counts a try; the fifth ends the invite. A right one is used up.
func (iv *Invites) Redeem(secret string) error {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	iv.mu.Lock()
	defer iv.mu.Unlock()
	c := iv.cur
	if c == nil || c.used || iv.now().After(c.until) || c.tries >= MaxInviteTries {
		return ErrInvite
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(c.secret)) != 1 {
		c.tries++
		return ErrInvite
	}
	c.used = true
	return nil
}

// Open reports whether an invite is live.
func (iv *Invites) Open() (until time.Time, ok bool) {
	iv.mu.Lock()
	defer iv.mu.Unlock()
	c := iv.cur
	if c == nil || c.used || iv.now().After(c.until) || c.tries >= MaxInviteTries {
		return time.Time{}, false
	}
	return c.until, true
}

// ParseInvite reads `fyr1-<install id>-<secret>`: the id is four groups of
// four, the secret ten characters. Spaces and case are forgiven.
func ParseInvite(code string) (id, secret string, err error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	rest, ok := strings.CutPrefix(code, strings.ToUpper(invitePrefix))
	if !ok || len(rest) != 19+1+10 || rest[19] != '-' {
		return "", "", errors.New("that is not an invite (it looks like fyr1-XXXX-XXXX-XXXX-XXXX-XXXXXXXXXX)")
	}
	id, secret = rest[:19], rest[20:]
	for i, r := range id {
		if (i+1)%5 == 0 {
			if r != '-' {
				return "", "", errors.New("that is not an invite: the install id is malformed")
			}
		} else if !strings.ContainsRune(inviteAlpha+"01", r) && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return "", "", errors.New("that is not an invite: the install id is malformed")
		}
	}
	return id, secret, nil
}

type inviteReq struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
	Host   string `json:"host,omitempty"`
	Port   int    `json:"port"`
}

type inviteResp struct {
	Name string `json:"name"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port"`
}

// joinInvite is the inviter's side: the caller's key is the one the handshake
// proved; the secret is the invite's. On success the caller is a member, signed
// by this install, and the answer says who this is.
func (e *Env) joinInvite(w http.ResponseWriter, r *http.Request) {
	pub, ok := peerlink.Caller(r)
	if !ok || e.Roster.SelfRemoved() {
		http.Error(w, "a key is needed", http.StatusForbidden)
		return
	}
	var req inviteReq
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req) != nil || req.Port <= 0 || req.Port > 65535 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := e.Invites.Redeem(req.Secret); err != nil {
		e.logf("peer: a pairing with an invite failed: %v", err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	host := req.Host
	if host == "" {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	name := req.Name
	if name == "" {
		name = host
	}
	e.Roster.Add(pub, name, host, req.Port)
	e.logf("peer: paired with %s by the owner's invite", name)
	selfName, selfHost := e.selfNameHost()
	json.NewEncoder(w).Encode(inviteResp{Name: selfName, Host: selfHost, Port: e.self().Port})
}

func (e *Env) selfNameHost() (name, host string) {
	if m, ok := e.Roster.Member(e.Key.ID()); ok {
		return e.Name, m.Host
	}
	return e.Name, ""
}

// NewInvite opens an invite on this install.
func (e *Env) NewInvite() (code string, until time.Time) {
	e.init()
	return e.Invites.New(e.Key.ID())
}

// JoinResult is what a successful pairing says.
type JoinResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Join pairs with the install an invite names. The id comes from the code; the
// address from addrHint ("host" or "host:port") or from the candidates
// (every tailnet node, tagged ones included, on the default peer port). Each
// attempt is a link pinned to the id, so only that install answers; the secret
// goes inside it.
func (e *Env) Join(ctx context.Context, code, addrHint string) (*JoinResult, error) {
	id, secret, err := ParseInvite(code)
	if err != nil {
		return nil, err
	}
	if id == e.Key.ID() {
		return nil, errors.New("that invite is from this install")
	}
	if _, in := e.Roster.Member(id); in {
		return nil, errors.New("this install is already paired with that one")
	}
	var addrs []string
	if addrHint != "" {
		if _, _, err := net.SplitHostPort(addrHint); err != nil {
			addrHint = net.JoinHostPort(addrHint, strconv.Itoa(DefaultPort))
		}
		addrs = append(addrs, addrHint)
	} else {
		addrs = e.candidateAddrs(ctx)
	}
	if len(addrs) == 0 {
		return nil, errors.New("no address to try: give one with -addr host:port")
	}
	var tried []string
	for _, a := range addrs {
		res, err := e.joinAt(ctx, id, secret, a)
		if err == nil {
			return res, nil
		}
		var wrong *joinRefused
		if errors.As(err, &wrong) {
			return nil, wrong // the install answered and said no: no use trying more addresses
		}
		tried = append(tried, a)
	}
	return nil, fmt.Errorf("no install with that id answered at %s: is its peer port up, and is the address right?", strings.Join(tried, ", "))
}

type joinRefused struct{ msg string }

func (j *joinRefused) Error() string { return j.msg }

func (e *Env) candidateAddrs(ctx context.Context) []string {
	var out []string
	if e.TS != nil {
		if _, nodes, err := e.TS.Status(ctx); err == nil {
			for _, n := range nodes {
				if n.Online {
					out = append(out, net.JoinHostPort(n.Name, strconv.Itoa(DefaultPort)))
				}
			}
		}
	}
	for _, m := range e.Roster.Members() { // a member's hints: a LAN address it was seen at
		if m.ID != e.Key.ID() && m.Port != 0 {
			out = append(out, addr(m))
		}
	}
	return out
}

func (e *Env) joinAt(ctx context.Context, id, secret, a string) (*JoinResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := e.Dial.DialTLS(ctx, id, a) // only the install with this id gets past the handshake
	if err != nil {
		return nil, err
	}
	pub, err := peerlink.ConnKey(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	host, _, _ := net.SplitHostPort(a)
	selfName, selfHost := e.selfNameHost()
	body, _ := json.Marshal(inviteReq{Secret: secret, Name: selfName, Host: selfHost, Port: e.self().Port})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://"+a+"/pair/invite", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: peerlink.Once(conn)}).Do(req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, &joinRefused{strings.TrimSpace(string(b))}
	}
	var out inviteResp
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out) != nil {
		return nil, errors.New("the answer was not understood")
	}
	if out.Host == "" {
		out.Host = host
	}
	if out.Port == 0 {
		_, p, _ := net.SplitHostPort(a)
		out.Port, _ = strconv.Atoi(p)
	}
	e.Roster.Add(pub, nameOr(out.Name, out.Host), out.Host, out.Port)
	e.logf("peer: paired with %s by the owner's invite", nameOr(out.Name, out.Host))
	return &JoinResult{ID: id, Name: nameOr(out.Name, out.Host), Addr: a}, nil
}

// Candidate is a node of the tailnet that is not in the roster, and why nothing
// paired it by itself.
type Candidate struct {
	Name   string `json:"name"`
	Online bool   `json:"online"`
	// Kind: tagged (a tag says who may reach a node, not whose it is: pair it with
	// an invite), other-user, shared, expired, waiting (the same user's, untagged:
	// it pairs by itself once its Serve and peer port are up).
	Kind string `json:"kind"`
}

// Candidates lists the tailnet nodes that are not members.
func (e *Env) Candidates(ctx context.Context) []Candidate {
	if e.TS == nil {
		return nil
	}
	self, nodes, err := e.TS.Status(ctx)
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, m := range e.Roster.Members() {
		known[strings.ToLower(m.Host)] = true
		known[strings.ToLower(m.Name)] = true
	}
	var out []Candidate
	for _, n := range nodes {
		if known[strings.ToLower(n.Name)] {
			continue
		}
		c := Candidate{Name: n.Name, Online: n.Online}
		switch {
		case !n.Untagged():
			c.Kind = "tagged"
		case n.Shared:
			c.Kind = "shared"
		case n.UserID != self.UserID:
			c.Kind = "other-user"
		case n.Expired:
			c.Kind = "expired"
		default:
			c.Kind = "waiting"
		}
		out = append(out, c)
	}
	return out
}

// MemberView is an install of the roster as Settings → Hosts shows it.
type MemberView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Self  bool   `json:"self,omitempty"`
	Port  int    `json:"port,omitempty"`
	Drive bool   `json:"drive"`
	// Via says how it became a member: "paired" (this install signed its add) or
	// "vouched" by the member named in By.
	Via string `json:"via"`
	By  string `json:"by,omitempty"`
}

// Members lists the roster with how each member came in.
func (e *Env) Members() []MemberView {
	ms := e.Roster.Members()
	names := map[string]string{}
	for _, m := range ms {
		names[m.ID] = m.Name
	}
	self := e.Key.ID()
	var out []MemberView
	for _, m := range ms {
		v := MemberView{ID: m.ID, Name: m.Name, Self: m.ID == self, Port: m.Port, Drive: e.Drive.MayDrive(m.ID), Via: "paired"}
		if m.AddedBy != self && m.AddedBy != m.ID && m.ID != self {
			v.Via, v.By = "vouched", nameOr(names[m.AddedBy], m.AddedBy)
		}
		out = append(out, v)
	}
	return out
}
