// Package tailnet asks the local tailscaled who owns this machine
// (SPEC 0.5 V0.14): that Tailscale login is the only one Fyr serves over
// the tailnet.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultSocket is tailscaled's LocalAPI socket on Linux.
const DefaultSocket = "/run/tailscale/tailscaled.sock"

// MacDir is where the macOS Tailscale app (the standalone app, and the
// open-source tailscaled) leaves its LocalAPI port: ipnport names it and
// sameuserproof-<port> holds the token. The app has no Unix socket.
var MacDir = "/Library/Tailscale"

// macLocalAPI reads the port and the token from dir; false without them.
func macLocalAPI(dir string) (port, token string, ok bool) {
	port, err := os.Readlink(filepath.Join(dir, "ipnport"))
	if err != nil {
		b, err := os.ReadFile(filepath.Join(dir, "ipnport"))
		if err != nil {
			return "", "", false
		}
		port = string(b)
	}
	port = strings.TrimSpace(port)
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", false
	}
	b, err := os.ReadFile(filepath.Join(dir, "sameuserproof-"+port))
	if err != nil {
		return "", "", false
	}
	token = strings.TrimSpace(string(b))
	return port, token, token != ""
}

// Available reports a LocalAPI to talk to: the socket, or the macOS app's
// port.
func Available(socket string) bool {
	if _, err := os.Stat(socket); err == nil {
		return true
	}
	_, _, ok := macLocalAPI(MacDir)
	return ok
}

// localClient talks to the LocalAPI at socket, or, when there is none, at
// the macOS app's port with its token.
func localClient(socket string) *http.Client {
	if _, err := os.Stat(socket); err != nil {
		if port, token, ok := macLocalAPI(MacDir); ok {
			return &http.Client{Timeout: 3 * time.Second, Transport: tokenTransport{token: token, rt: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
				}}}}
		}
	}
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}
}

// tokenTransport sends the macOS app's token as the LocalAPI asks: basic
// auth with an empty user.
type tokenTransport struct {
	token string
	rt    http.RoundTripper
}

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth("", t.token)
	return t.rt.RoundTrip(r)
}

// Peers names the owner of a tailnet peer by the address of its connection
// (LocalAPI whois). A host's tailnet listener serves a front only when that
// login is the owner's (SPEC 0.5 V0.51); a tagged node has no user and is
// refused.
type Peers struct{ Socket string }

func (p Peers) Login(ctx context.Context, remoteAddr string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://local-tailscaled.sock/localapi/v0/whois?addr="+url.QueryEscape(remoteAddr), nil)
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localClient(p.Socket).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tailscaled whois %s: %s", remoteAddr, resp.Status)
	}
	var w struct {
		Node struct {
			Tags []string
		}
		UserProfile struct{ LoginName string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		return "", err
	}
	if len(w.Node.Tags) > 0 || w.UserProfile.LoginName == "" {
		return "", fmt.Errorf("tailnet peer %s has no owner (tagged node)", remoteAddr)
	}
	return w.UserProfile.LoginName, nil
}

// ErrTagged: this node is tagged (tag:server), so it has no user. tailscaled
// still names a user for it, whose login is the node itself, which must not
// be taken as the owner (SPEC V0.129 (4), decision 0028).
var ErrTagged = errors.New("this node is tagged and has no Tailscale user: set ownerLogin")

// Self returns the login of this node's user and its MagicDNS name. On a
// tagged node it returns the name with ErrTagged.
func Self(socket string) (login, dnsName string, err error) {
	c := localClient(socket)
	req, _ := http.NewRequest("GET", "http://local-tailscaled.sock/localapi/v0/status", nil)
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := c.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("tailscaled status: %s", resp.Status)
	}
	var s struct {
		Self struct {
			UserID  int64
			DNSName string
			Tags    []string
		}
		User map[string]struct{ LoginName string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", "", err
	}
	if len(s.Self.Tags) > 0 {
		return "", strings.TrimSuffix(s.Self.DNSName, "."), ErrTagged
	}
	login = s.User[strconv.FormatInt(s.Self.UserID, 10)].LoginName
	if login == "" {
		return "", "", fmt.Errorf("tailscaled did not name this node's user")
	}
	return login, strings.TrimSuffix(s.Self.DNSName, "."), nil
}

// IPv4 is this node's tailnet IPv4 address (SPEC 0.5 V0.54): a host's
// `hostListen` of "tailnet:PORT" listens there, so a config need not name
// the address.
func IPv4(socket string) (string, error) {
	req, _ := http.NewRequest("GET", "http://local-tailscaled.sock/localapi/v0/status", nil)
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localClient(socket).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tailscaled status: %s", resp.Status)
	}
	var s struct {
		TailscaleIPs []string
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", err
	}
	for _, ip := range s.TailscaleIPs {
		if p := net.ParseIP(ip); p != nil && p.To4() != nil {
			return ip, nil
		}
	}
	return "", fmt.Errorf("tailscaled reports no IPv4 address for this node (is it logged in?)")
}

// Candidates are the MagicDNS names of the tailnet peers that may be Fyr
// hosts (SPEC 0.5 V0.123, decision 0025): online, untagged, and owned by this
// node's own Tailscale user. Discovery then probes each for /.well-known/fyr.
func Candidates(ctx context.Context, socket string) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://local-tailscaled.sock/localapi/v0/status", nil)
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localClient(socket).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tailscaled status: %s", resp.Status)
	}
	var s struct {
		Self struct{ UserID int64 }
		Peer map[string]struct {
			DNSName string
			UserID  int64
			Online  bool
			Tags    []string
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range s.Peer {
		name := strings.TrimSuffix(p.DNSName, ".")
		if p.Online && len(p.Tags) == 0 && p.UserID == s.Self.UserID && s.Self.UserID != 0 && name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Names are this node's own MagicDNS names (the full name and its first
// label), which a front may use as the Host of its requests; the host's
// listener refuses any other name (decision 0030 slice 1: DNS rebinding).
func (p Peers) Names(ctx context.Context) []string {
	_, dns, err := Self(p.Socket)
	if err != nil && !errors.Is(err, ErrTagged) {
		return nil
	}
	if dns == "" {
		return nil
	}
	return []string{dns, strings.SplitN(dns, ".", 2)[0]}
}

// NodeInfo is what tailscaled's whois says about the node at an address
// (decision 0030 3.4): which node a request came from, not only whose it is.
type NodeInfo struct {
	StableID string // the node's stable id: what the owner approves or blocks
	Name     string // MagicDNS name without the trailing dot
	Login    string // the user's login; "" for a tagged node
	UserID   int64
	Tags     []string
	// Shared: the node was shared into this tailnet by another (Sharer is set).
	Shared bool
}

// Node asks whois about the node at remoteAddr (an IP or IP:port).
func (p Peers) Node(ctx context.Context, remoteAddr string) (NodeInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://local-tailscaled.sock/localapi/v0/whois?addr="+url.QueryEscape(remoteAddr), nil)
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localClient(p.Socket).Do(req)
	if err != nil {
		return NodeInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return NodeInfo{}, fmt.Errorf("tailscaled whois %s: %s", remoteAddr, resp.Status)
	}
	var w struct {
		Node struct {
			StableID string
			Name     string
			Tags     []string
			Sharer   int64
		}
		UserProfile struct {
			ID        int64
			LoginName string
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		return NodeInfo{}, err
	}
	n := NodeInfo{StableID: w.Node.StableID, Name: strings.TrimSuffix(w.Node.Name, "."), Tags: w.Node.Tags, Shared: w.Node.Sharer != 0}
	if len(w.Node.Tags) == 0 {
		n.Login, n.UserID = w.UserProfile.LoginName, w.UserProfile.ID
	}
	if n.StableID == "" {
		return NodeInfo{}, fmt.Errorf("tailscaled whois %s: no node id", remoteAddr)
	}
	return n, nil
}

// StatusNode is a node as tailscaled's status lists it.
type StatusNode struct {
	Name    string // MagicDNS name, no trailing dot
	UserID  int64
	Login   string // the user's login; "" for a tagged node
	Tags    []string
	Online  bool
	Shared  bool // shared in from another tailnet
	Expired bool // the node key has expired
	IPs     []string
}

// Untagged: the node belongs to a user, not to a tag.
func (n StatusNode) Untagged() bool { return len(n.Tags) == 0 }

// Status is this node and its peers (decision 0030 3.6 condition 1 reads who is
// the same user's, untagged, not shared in and not expired).
func Status(ctx context.Context, socket string) (self StatusNode, peers []StatusNode, err error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://local-tailscaled.sock/localapi/v0/status", nil)
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localClient(socket).Do(req)
	if err != nil {
		return self, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return self, nil, fmt.Errorf("tailscaled status: %s", resp.Status)
	}
	type node struct {
		DNSName      string
		UserID       int64
		Tags         []string
		Online       bool
		ShareeNode   bool
		Expired      bool
		TailscaleIPs []string
	}
	var s struct {
		Self node
		Peer map[string]node
		User map[string]struct{ LoginName string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return self, nil, err
	}
	mk := func(n node) StatusNode {
		out := StatusNode{Name: strings.TrimSuffix(n.DNSName, "."), UserID: n.UserID, Tags: n.Tags, Online: n.Online, Shared: n.ShareeNode, Expired: n.Expired, IPs: n.TailscaleIPs}
		if len(n.Tags) == 0 {
			out.Login = s.User[strconv.FormatInt(n.UserID, 10)].LoginName
		}
		return out
	}
	self = mk(s.Self)
	self.Online = true
	for _, p := range s.Peer {
		peers = append(peers, mk(p))
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return self, peers, nil
}
