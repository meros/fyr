package authtest

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"

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
}

// MeshNode is a started install of a Mesh.
type MeshNode struct {
	Spec MeshSpec
	Node Node
	TS   *Tailscaled
	Fyrd *Fyrd
	Name string // the fyrd's -name
}

// Mesh is several scratch fyrds on one fake tailnet, each with its own
// tailscaled (its own Self), a Serve endpoint per pair that a node's program
// reaches over HTTPS with a test CA, and peer ports on loopback: decision 0030
// 4.1's "two and three of them make a front and hosts".
type Mesh struct {
	CA    *CA
	Nodes []*MeshNode

	cleanups []func()
}

// NewMesh starts the installs. Users 1 (the owner) and 2 exist.
func NewMesh(specs ...MeshSpec) (m *Mesh, err error) {
	m = &Mesh{}
	defer func() {
		if err != nil {
			m.Close()
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
		caller.Fyrd, err = Start(Options{Name: caller.Name, Role: caller.Spec.Role, Data: datas[i], Tailscale: caller.TS,
			Extra: map[string]any{"serveSocketUsers": []int{os.Getuid()}, "peerListen": "127.0.0.1:0", "peerEverySeconds": 1, "resolveFile": rf, "rootCAs": caFile}})
		if err != nil {
			return nil, err
		}
		m.cleanups = append(m.cleanups, caller.Fyrd.Stop)
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
