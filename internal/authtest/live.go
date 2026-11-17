package authtest

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
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
	LocalID    string // the device ids of those sessions
	TailnetID  string

	// Rec records what the laptop's front sends its static host (Server).
	Rec *Recorder

	Self  Node // the laptop's node
	Phone Node // the owner's phone: untagged, the owner's user
	Other Node // another tailnet user's node
	Tag   Node // a tagged node (meros-server's kind)

	Phones *Serve // the phone's path: Serve emulator into the laptop's serve.sock

	cleanups []func()
	// resolveFiles are the peer installs' name→address files, by install name; caFile
	// is the CA they trust for each other's Serve (EnablePeers).
	resolveFiles map[string]string
	caFile       string
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
	var d1, d2 *auth.Device
	l.Local, d1 = a.Mint(auth.HowLocal, "Chrome on Linux · on laptop")
	l.Tailnet, d2 = a.Mint(auth.HowTailnet, "Chrome on Android")
	l.LocalID, l.TailnetID = d1.ID, d2.ID
	if err := st.Close(); err != nil {
		return nil, err
	}
	if l.Rec, err = NewRecorder(l.Server.URL()); err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Rec.Close)
	// Serve's emulator is not root: decision 0030 3.2 closes serve.sock to
	// every user but root, so the harness names its own user (config
	// serveSocketUsers).
	l.Laptop, err = Start(Options{Name: "laptop", Data: data, Tailscale: l.TS, Extra: map[string]any{"serveSocketUsers": []int{os.Getuid()}},
		Hosts: []HostEntry{{Name: "server", URL: l.Rec.URL}}})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Laptop.Stop)
	l.Phones, err = l.TS.ServeFor(l.Phone, l.Laptop.Sock(auth.ServeSocket))
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Phones.Close)
	// A ready mesh: the front follows both hosts before any test starts (a
	// command posted to a host that has not connected yet is a 503).
	if hosts, err := l.WaitHostsOnline(l.Local, 30*time.Second, "laptop", "server"); err != nil || !hosts["laptop"] || !hosts["server"] {
		return nil, fmt.Errorf("the front does not follow its hosts: %v %v\n%s", hosts, err, l.Laptop.Log())
	}
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

// Mac is the owner's second install, started by EnablePeers.
type Mac struct {
	Fyrd *Fyrd
	TS   *Tailscaled
	Node Node
}

// EnablePeers is slice 3's state change of the live install (decision 0030 5.2):
// the laptop restarts as a release would restart it, with a peer port, and the
// owner's Mac (the same tailnet user, Serve up on both) pairs with it by condition
// 1. The laptop's data folder, sessions and static host stay as they were; the
// Mac is a new install beside them.
func (l *Live) EnablePeers() (*Mac, error) {
	ca, err := NewCA()
	if err != nil {
		return nil, err
	}
	dir, rm, err := NewDataDir()
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, rm)
	macNode := Node{Name: "mac.tail-test.ts.net", IP: "100.64.0.9", UserID: 1, Online: true}
	l.TS.AddNode(macNode)
	macTS, err := NewTailscaled(dir, macNode, User{1, l.OwnerLogin}, User{2, "guest@example.com"})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, macTS.Close)
	macTS.AddNode(l.Self)
	macData, rm2, err := NewDataDir()
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, rm2)
	root := filepath.Dir(macData)
	pemFile := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(pemFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Cert.Raw}), 0o600); err != nil {
		return nil, err
	}
	laptopData := l.Laptop.Data
	// Each one's view of the other's Serve.
	toMac, err := macTS.ServeTLS(l.Self, auth.SocketPath(macData, auth.ServeSocket), ca)
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, toMac.Close)
	toLaptop, err := l.TS.ServeTLS(macNode, auth.SocketPath(laptopData, auth.ServeSocket), ca)
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, toLaptop.Close)
	l.caFile = pemFile
	l.resolveFiles = map[string]string{}
	res := func(name, serve string) string {
		p := filepath.Join(root, "resolve-"+name+".json")
		l.resolveFiles[name] = p
		b, _ := json.Marshal(map[string]string{l.Self.Name: "127.0.0.1", macNode.Name: "127.0.0.1", net.JoinHostPort(map[string]string{"laptop": macNode.Name, "mac": l.Self.Name}[name], "7443"): serve})
		os.WriteFile(p, b, 0o600)
		return p
	}
	peerCfg := func(resolve string) map[string]any {
		return map[string]any{"serveSocketUsers": []int{os.Getuid()}, "peerListen": "127.0.0.1:0", "peerEverySeconds": 1, "resolveFile": resolve, "rootCAs": pemFile}
	}
	// The release restarts the laptop with a peer port; its data stays.
	l.Laptop.Stop()
	cfg := peerCfg(res("laptop", toMac.Addr))
	l.Laptop, err = Start(Options{Name: "laptop", Data: laptopData, Tailscale: l.TS, Extra: cfg, Hosts: []HostEntry{{Name: "server", URL: l.Rec.URL}}})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, l.Laptop.Stop)
	mac, err := Start(Options{Name: "mac", Data: macData, Tailscale: macTS, Extra: peerCfg(res("mac", toLaptop.Addr))})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, mac.Stop)
	return &Mac{Fyrd: mac, TS: macTS, Node: macNode}, nil
}

// AddTaggedInstall is slice 4's state change of the live install (decision 0030
// 5.2): a tagged install (the owner's meros-server) beside the others. It cannot
// pair by itself; the owner pairs it with an invite from `fyr pair`. The caller
// does that and checks what followed.
func (l *Live) AddTaggedInstall(mac *Mac) (*Mac, error) {
	dir, rm, err := NewDataDir()
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, rm)
	boxNode := Node{Name: "box.tail-test.ts.net", IP: "100.64.0.10", Tags: []string{"tag:server"}, Online: true}
	l.TS.AddNode(boxNode)
	mac.TS.AddNode(boxNode)
	boxTS, err := NewTailscaled(dir, boxNode, User{1, l.OwnerLogin}, User{2, "guest@example.com"})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, boxTS.Close)
	boxTS.AddNode(l.Self)
	boxTS.AddNode(mac.Node)
	boxTS.AddNode(l.Phone)
	data, rm2, err := NewDataDir()
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, rm2)
	root := filepath.Dir(data)
	// every install resolves every install's name to loopback (any port)
	for _, rf := range l.resolveFiles {
		b, _ := os.ReadFile(rf)
		var m map[string]string
		json.Unmarshal(b, &m)
		m[boxNode.Name] = "127.0.0.1"
		b, _ = json.Marshal(m)
		os.WriteFile(rf, b, 0o600)
	}
	rf := filepath.Join(root, "resolve-box.json")
	b, _ := json.Marshal(map[string]string{l.Self.Name: "127.0.0.1", mac.Node.Name: "127.0.0.1", boxNode.Name: "127.0.0.1"})
	os.WriteFile(rf, b, 0o600)
	box, err := Start(Options{Name: "box", Data: data, Tailscale: boxTS, Extra: map[string]any{"serveSocketUsers": []int{os.Getuid()}, "peerListen": "127.0.0.1:0", "peerEverySeconds": 1, "resolveFile": rf, "rootCAs": l.caFile}})
	if err != nil {
		return nil, err
	}
	l.cleanups = append(l.cleanups, box.Stop)
	return &Mac{Fyrd: box, TS: boxTS, Node: boxNode}, nil
}
