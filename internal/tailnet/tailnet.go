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
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultSocket is tailscaled's LocalAPI socket on Linux.
const DefaultSocket = "/run/tailscale/tailscaled.sock"

func localClient(socket string) *http.Client {
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}
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
