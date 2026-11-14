// Package tailnet asks the local tailscaled who owns this machine
// (SPEC 0.5 V0.14): that Tailscale login is the only one Fyr serves over
// the tailnet.
package tailnet

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultSocket is tailscaled's LocalAPI socket on Linux.
const DefaultSocket = "/run/tailscale/tailscaled.sock"

// Self returns the login of this node's user and its MagicDNS name.
func Self(socket string) (login, dnsName string, err error) {
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}
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
		}
		User map[string]struct{ LoginName string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", "", err
	}
	login = s.User[strconv.FormatInt(s.Self.UserID, 10)].LoginName
	if login == "" {
		return "", "", fmt.Errorf("tailscaled did not name this node's user")
	}
	return login, strings.TrimSuffix(s.Self.DNSName, "."), nil
}
