package authtest

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/store"
)

// Live is a fixture of the owner's install as it runs today (decision 0030
// Part 5.2, V0.130): a laptop with an install key and two browser sessions
// in its database (one made by fyr open, one by the tailnet), observe mode
// (every way in still serves, none is refused), serve.sock, a static `hosts`
// entry that its front follows, and a phone that reaches it through Tailscale
// Serve. Every later slice's state change is applied to it in order by
// TestMigrationKeepsTheOwnerSignedIn.
type Live struct {
	TS     *Tailscaled
	Laptop *Fyrd // role both: the front, its own host, serve.sock, owner.sock
	Server *Fyrd // role host: the front's static hosts entry

	OwnerLogin string
	ID         string // the laptop's install id
	Local      string // the session `fyr open` made (How local)
	Tailnet    string // the session the phone got through Serve (How tailnet)

	Self  Node // the laptop's node
	Phone Node // the owner's phone: untagged, the owner's user
	Other Node // another tailnet user's node
	Tag   Node // a tagged node (meros-server's kind)

	Phones *Serve // the phone's path: Serve emulator into the laptop's serve.sock

	cleanups []func()
}

// NewLive builds the fixture and starts both fyrds. Close stops them.
func NewLive() (l *Live, err error) {
	l = &Live{OwnerLogin: "owner@example.com"}
	defer func() {
		if err != nil {
			l.Close()
		}
	}()
	tsDir, rm, err := NewDataDir()
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, rm)
	l.Self = Node{Name: "laptop.tail-test.ts.net", IP: "100.64.0.1", UserID: 1, Online: true}
	l.Phone = Node{Name: "phone.tail-test.ts.net", IP: "100.64.0.2", UserID: 1, Online: true}
	l.Other = Node{Name: "guest.tail-test.ts.net", IP: "100.64.0.4", UserID: 2, Online: true}
	l.Tag = Node{Name: "server.tail-test.ts.net", IP: "100.64.0.3", Tags: []string{"tag:server"}, Online: true}
	l.TS, err = NewTailscaled(tsDir, l.Self, User{1, l.OwnerLogin}, User{2, "guest@example.com"})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.TS.Close)
	for _, n := range []Node{l.Phone, l.Other, l.Tag} {
		l.TS.AddNode(n)
	}

	l.Server, err = Start(Options{Role: "host", Name: "server"})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Server.Stop)

	// The laptop's data folder as an install that ran V0.130 left it.
	data, rm2, err := NewDataDir()
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, rm2)
	k, err := identity.Load(data)
	if err != nil {
		return nil, err
	}
	l.ID = k.ID()
	st, err := store.Open(filepath.Join(data, "fyr.db"))
	if err != nil {
		return nil, err
	}
	a, err := auth.Open(st, nil)
	if err != nil {
		st.Close()
		return nil, err
	}
	l.Local, _ = a.Mint(auth.HowLocal, "Chrome on Linux · on laptop")
	l.Tailnet, _ = a.Mint(auth.HowTailnet, "Chrome on Android")
	if err := st.Close(); err != nil {
		return nil, err
	}
	l.Laptop, err = Start(Options{Name: "laptop", Data: data, Tailscale: l.TS,
		Hosts: []HostEntry{{Name: "server", URL: l.Server.URL()}}})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Laptop.Stop)
	l.Phones, err = l.TS.ServeFor(l.Phone, l.Laptop.Sock(auth.ServeSocket))
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Phones.Close)
	return l, nil
}

// Close stops everything, last started first.
func (l *Live) Close() {
	for i := len(l.cleanups) - 1; i >= 0; i-- {
		l.cleanups[i]()
	}
	l.cleanups = nil
}

// HostsOnline asks the laptop's front which hosts it follows and whether each
// is online: name → online. cookie is a session ("" sends none).
func (l *Live) HostsOnline(cookie string) (map[string]bool, error) {
	r, err := l.Laptop.Get("/api/state", cookie)
	if err != nil {
		return nil, err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("GET /api/state: %d %s", r.Status, r.Body)
	}
	var st struct {
		Hosts []struct {
			Name   string
			Online bool
		}
	}
	if err := json.Unmarshal(r.Body, &st); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, h := range st.Hosts {
		out[h.Name] = h.Online
	}
	return out, nil
}

// WaitHostsOnline polls until the front shows every named host online, or
// the deadline passes; it returns the last answer.
func (l *Live) WaitHostsOnline(cookie string, d time.Duration, names ...string) (map[string]bool, error) {
	end := time.Now().Add(d)
	for {
		hosts, err := l.HostsOnline(cookie)
		all := err == nil
		for _, n := range names {
			all = all && hosts[n]
		}
		if all || time.Now().After(end) {
			return hosts, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
