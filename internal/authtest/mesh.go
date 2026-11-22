package authtest

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/auth"
)

// MeshSpec is one install of a Mesh: a node of the fake tailnet with a fyrd on it.
type MeshSpec struct {
	Name     string // "a" becomes a.tail-test.ts.net
	UserID   int64  // 1 is the owner
	Tags     []string
	SharedIn bool
	Expired  bool
	// NoServe: the node has no Tailscale Serve answering, though its peer port
	// is up (a phone app, or Serve not set up).
	NoServe bool
	// Hide lists the names of nodes that this node's tailscaled does not show
	// (and which do not show it): two installs that only meet through a third.
	Hide []string
	// Role is the fyrd's role; default both.
	Role string
	// Block lists the installs this one cannot dial (a one-way ACL on their peer ports): a
	// dial goes into a black hole and times out, as a Tailscale ACL drop does.
	Block []string
	// HostListen gives the install a loopback host port (the old way a front reached a
	// host, decision 0030 slice 5), at MeshNode.HostPort.
	HostListen bool
	// NoOwnerLogin leaves ownerLogin unset in the config even for an untagged node
	// (V0.owner-without-tailnet: tests that fyrd does not fall back to tailnet.Self).
	NoOwnerLogin bool
}

// MeshNode is a started install of a Mesh.
type MeshNode struct {
	resolveFile string
	m           *Mesh
	Spec        MeshSpec
	Node        Node
	TS          *Tailscaled
	Fyrd        *Fyrd
	Name        string // the fyrd's -name
	// PeerPort is the fixed port of its peer listener, so a restart (a rotation, a
	// release) keeps it; HostPort the loopback host port, 0 with none.
	PeerPort, HostPort int
}

// Mesh is several scratch fyrds on one fake tailnet, each with its own
// tailscaled (its own Self), a Serve endpoint per pair that a node's program
// reaches over HTTPS with a test CA, and peer ports on loopback: decision 0030
// 4.1's "two and three of them make a front and hosts".
type Mesh struct {
	CA    *CA
	Nodes []*MeshNode

	blackhole net.Listener

	cleanups []func()
}

// NewMesh starts the installs. Users 1 (the owner) and 2 exist.
func NewMesh(specs ...MeshSpec) (m *Mesh, err error) {
	started := &Mesh{}
	m = started
	defer func() {
		if err != nil {
			started.Close() // m is nil in a failed return: close what was started
		}
	}()
	if m.CA, err = NewCA(); err != nil {
		return nil, err
	}
	users := []User{{1, "owner@example.com"}, {2, "guest@example.com"}}
	var nodes []Node
	for i, s := range specs {
		nodes = append(nodes, Node{Name: s.Name + ".tail-test.ts.net", IP: fmt.Sprintf("100.64.1.%d", i+1), UserID: s.UserID, Tags: s.Tags, SharedIn: s.SharedIn, Expired: s.Expired, Online: true})
	}
	hidden := func(a, b string) bool {
		for _, s := range specs {
			for _, h := range s.Hide {
				if (s.Name == a && h == b) || (s.Name == b && h == a) {
					return true
				}
			}
		}
		return false
	}
	// tailscaled, data folders and the CA file first: the fyrds need paths.
	datas := make([]string, len(specs))
	for i, s := range specs {
		dir, rm, err := NewDataDir()
		if err != nil {
			return nil, err
		}
		m.cleanups = append(m.cleanups, rm)
		tsDir, rm2, err := NewDataDir()
		if err != nil {
			return nil, err
		}
		m.cleanups = append(m.cleanups, rm2)
		datas[i] = dir
		ts, err := NewTailscaled(tsDir, nodes[i], users...)
		if err != nil {
			return nil, err
		}
		m.cleanups = append(m.cleanups, ts.Close)
		for j, n := range nodes {
			if j != i && !hidden(s.Name, specs[j].Name) {
				ts.AddNode(n)
			}
		}
		m.Nodes = append(m.Nodes, &MeshNode{Spec: s, Node: nodes[i], TS: ts, Name: s.Name})
	}
	caFile := filepath.Join(filepath.Dir(datas[0]), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: m.CA.Cert.Raw}), 0o600); err != nil {
		return nil, err
	}
	// For each caller, where each target's Serve answers: a TLS front that is
	// the caller's own view of the target's Serve (it carries the caller's node).
	for i, caller := range m.Nodes {
		resolve := map[string]string{}
		for j, target := range m.Nodes {
			if i == j || hidden(caller.Name, target.Name) {
				continue
			}
			resolve[target.Node.Name] = "127.0.0.1" // peer ports: the name maps to loopback, any port
			if target.Spec.NoServe {
				continue
			}
			front, err := target.TS.ServeTLS(caller.Node, auth.SocketPath(datas[j], auth.ServeSocket), m.CA)
			if err != nil {
				return nil, err
			}
			m.cleanups = append(m.cleanups, front.Close)
			resolve[net.JoinHostPort(target.Node.Name, "7443")] = front.Addr
		}
		rf := filepath.Join(filepath.Dir(datas[i]), "resolve.json")
		b, _ := json.Marshal(resolve)
		if err := os.WriteFile(rf, b, 0o600); err != nil {
			return nil, err
		}
		// The peer port is kept for the restarts a test makes, as a release keeps an
		// install's: a reserved one, which no listen on :0 is handed while the install is
		// down (V0.port-race). Released after the fyrd stops (cleanups run last first).
		caller.PeerPort, err = m.reserve()
		if err != nil {
			return nil, err
		}
		extra := map[string]any{"serveSocketUsers": []int{os.Getuid()}, "peerListen": net.JoinHostPort("127.0.0.1", strconv.Itoa(caller.PeerPort)), "peerEverySeconds": 1, "resolveFile": rf, "rootCAs": caFile,
			"linkBackoffMillis": 100, "linkDialMillis": 700}
		if !caller.Spec.NoOwnerLogin && caller.Spec.UserID != 0 && len(caller.Spec.Tags) == 0 {
			for _, u := range users {
				if u.ID == caller.Spec.UserID {
					extra["ownerLogin"] = u.Login
				}
			}
		}
		caller.resolveFile, caller.m = rf, m
		if caller.Spec.HostListen {
			if caller.HostPort, err = m.reserve(); err != nil {
				return nil, err
			}
			extra["hostListen"] = net.JoinHostPort("127.0.0.1", strconv.Itoa(caller.HostPort))
		}
		if len(caller.Spec.Block) > 0 { // before it starts: it never dials an earlier node's peer port, so no link is opened the other way before the block is set
			caller.SetBlocked(caller.Spec.Block...)
		}
		caller.Fyrd, err = Start(Options{Name: caller.Name, Role: caller.Spec.Role, Data: datas[i], Tailscale: caller.TS, Extra: extra})
		if err != nil {
			return nil, err
		}
		m.cleanups = append(m.cleanups, caller.Fyrd.Stop)
		if _, err := caller.Fyrd.WaitRoster(30*time.Second, func(r *Roster) bool { // the peer port is up
			for _, mm := range r.Members {
				if mm.Self && mm.Port == caller.PeerPort {
					return true
				}
			}
			return false
		}); err != nil {
			return nil, err
		}
	}
	for _, n := range m.Nodes {
		if len(n.Spec.Block) > 0 {
			n.SetBlocked(n.Spec.Block...)
		}
	}
	return m, nil
}

// Close stops everything, last started first.
func (m *Mesh) Close() {
	for i := len(m.cleanups) - 1; i >= 0; i-- {
		m.cleanups[i]()
	}
	m.cleanups = nil
}

// Node finds an install by name.
func (m *Mesh) Node(name string) *MeshNode {
	for _, n := range m.Nodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

// reserve is a port kept until the mesh closes (reservePort).
func (m *Mesh) reserve() (int, error) {
	p, release, err := reservePort()
	if err != nil {
		return 0, err
	}
	m.cleanups = append(m.cleanups, release)
	return p, nil
}

// SetBlocked makes the named installs unreachable from this one: its dials to their
// peer ports hang (a black hole), as an ACL drop does. Called with none, it
// unblocks. A link that is already up is not touched; it ends when it ends.
func (n *MeshNode) SetBlocked(names ...string) {
	m := n.m
	if m.blackhole == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return
		}
		m.blackhole = ln
		m.cleanups = append(m.cleanups, func() { ln.Close() })
		go func() {
			var held []net.Conn
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				held = append(held, c) // held open and silent, as a dropped flow is
				_ = held
			}
		}()
	}
	b, _ := os.ReadFile(n.resolveFile)
	var res map[string]string
	json.Unmarshal(b, &res)
	for k := range res { // drop the old black holes
		if strings.HasSuffix(k, "#acl") {
			delete(res, k)
		}
	}
	for _, other := range m.Nodes {
		key := net.JoinHostPort(other.Node.Name, strconv.Itoa(other.PeerPort))
		delete(res, key)
		for _, name := range names {
			if name == other.Name && other.PeerPort != 0 {
				res[key] = m.blackhole.Addr().String()
			}
		}
	}
	b, _ = json.Marshal(res)
	os.WriteFile(n.resolveFile, b, 0o600)
}
