package main

// fyr auth … and fyr device … (SPEC V0.157, decision 0030 3.8): the local
// channel to the owner socket, which no mode gates. It is how the owner moves
// a way in between observe and enforce, adds and approves a device, and
// revokes one, from any shell, whatever state the browsers are in.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/auth"
)

func ownerCall(data, method, path string, body any) (map[string]any, error) {
	sock := auth.SocketPath(data, auth.OwnerSocket)
	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://owner"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Fyr does not answer on %s (is it running, as this user?): %w", sock, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 {
		return out, fmt.Errorf("Fyr refused: %s %v", resp.Status, out["error"])
	}
	return out, nil
}

// authCommand runs `fyr auth …` or `fyr device …`; args come after the word.
func authCommand(word string, args []string) error {
	fl := flag.NewFlagSet("fyr "+word, flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	data := fl.String("data", filepath.Join(home, ".local", "share", "fyr"), "data directory")
	name := fl.String("name", "", "device add: what to call the device")
	addr := fl.String("addr", "", "pair: host:port of the install that made the invite")
	// flags may come before or after the verb
	var rest []string
	for len(args) > 0 {
		if strings.HasPrefix(args[0], "-") {
			if err := fl.Parse(args); err != nil {
				return err
			}
			args = fl.Args()
			continue
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
	if word == "pair" {
		return pairCommand(*data, *addr, rest)
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: fyr %s %s", word, map[string]string{"auth": "status | observe [browser|peer] | enforce [browser|peer]", "device": "add [-name N] | approve CODE | revoke ID | list", "host": "list | drive ID on|off", "pair": "[CODE [-addr host:port]]"}[word])
	}
	verb, more := rest[0], rest[1:]
	switch word + " " + verb {
	case "auth status":
		out, err := ownerCall(*data, "GET", "/auth/status", nil)
		if err != nil {
			return err
		}
		printStatus(out)
	case "auth observe", "auth enforce":
		way := "browser"
		if len(more) > 0 {
			way = more[0]
		}
		out, err := ownerCall(*data, "POST", "/auth/mode", map[string]string{"way": way, "mode": verb})
		if err != nil {
			return err
		}
		fmt.Printf("%s is now %s.\n", way, verb)
		printStatus(out)
	case "device add":
		out, err := ownerCall(*data, "POST", "/device/add", map[string]string{"name": *name})
		if err != nil {
			return err
		}
		if q, _ := out["qr"].(string); q != "" {
			fmt.Print(q)
		}
		fmt.Printf("%v\nThe link works once, for 10 minutes. Open it on the device (scan the code or type the link).\n", out["url"])
	case "device approve":
		if len(more) != 1 {
			return fmt.Errorf("usage: fyr device approve CODE")
		}
		out, err := ownerCall(*data, "POST", "/device/approve", map[string]string{"code": more[0]})
		if err != nil {
			return err
		}
		fmt.Printf("Approved %v.\n", out["device"])
	case "device revoke":
		if len(more) != 1 {
			return fmt.Errorf("usage: fyr device revoke ID")
		}
		out, err := ownerCall(*data, "POST", "/device/revoke", map[string]string{"id": more[0]})
		if err != nil {
			return err
		}
		fmt.Printf("Revoked %v.\n", out["revoked"])
	case "host list":
		out, err := ownerCall(*data, "GET", "/host/list", nil)
		if err != nil {
			return err
		}
		ms, _ := out["members"].([]any)
		for _, x := range ms {
			m, _ := x.(map[string]any)
			note := ""
			if self, _ := m["self"].(bool); self {
				note = "  (this install)"
			} else if v, _ := m["via"].(string); v == "vouched" {
				note = fmt.Sprintf("  vouched for by %v", m["by"])
			}
			if d, _ := m["drive"].(bool); !d {
				note += "  may not drive this host"
			}
			fmt.Printf("%-21v %-24v%s\n", m["id"], m["name"], note)
		}
		cs, _ := out["candidates"].([]any)
		for _, x := range cs {
			c, _ := x.(map[string]any)
			fmt.Printf("not paired: %-40v %v\n", c["name"], map[string]string{"tagged": "tagged: pair it with an invite (fyr pair)", "other-user": "another tailnet user's", "shared": "shared in from another tailnet", "expired": "its key has expired", "waiting": "the same user's: pairs by itself once its Serve and peer port are up"}[fmt.Sprint(c["kind"])])
		}
		if n, ok := out["whoisLinkUses"].(float64); ok && n > 0 {
			fmt.Printf("%d requests went over a whois link although a peer link exists.\n", int(n))
		}
	case "host drive":
		if len(more) != 2 || (more[1] != "on" && more[1] != "off") {
			return fmt.Errorf("usage: fyr host drive ID on|off")
		}
		if _, err := ownerCall(*data, "POST", "/host/drive", map[string]any{"id": more[0], "on": more[1] == "on"}); err != nil {
			return err
		}
		fmt.Printf("%s %s drive this host.\n", more[0], map[string]string{"on": "may", "off": "may not"}[more[1]])
	case "device list":
		out, err := ownerCall(*data, "GET", "/device/list", nil)
		if err != nil {
			return err
		}
		devs, _ := out["devices"].([]any)
		for _, d := range devs {
			m, _ := d.(map[string]any)
			state := ""
			if p, _ := m["pending"].(bool); p {
				state = fmt.Sprintf("  WAITING, code %v", m["code"])
			}
			fmt.Printf("%-14v %-8v %-40v last used %v%s\n", m["id"], m["how"], m["name"], m["lastSeen"], state)
		}
	default:
		return fmt.Errorf("fyr %s: unknown %q", word, verb)
	}
	return nil
}

func printStatus(out map[string]any) {
	modes, _ := out["modes"].([]any)
	for _, x := range modes {
		m, _ := x.(map[string]any)
		line := fmt.Sprintf("%-8v %-8v would refuse %v since %v", m["way"], m["mode"], m["refused"], m["since"])
		if ready, _ := m["ready"].(bool); ready {
			line += "; ready: it enforces itself at the next check"
		} else if why, _ := m["why"].(string); why != "" && m["mode"] == "observe" {
			line += "; not ready: " + why
		}
		fmt.Println(line)
	}
	if n, ok := out["legacyFronts"].(float64); ok && n > 0 {
		fmt.Printf("%d requests from a front without Fyr-Actor were served (an older install).\n", int(n))
	}
}

// pairCommand is `fyr pair` (open an invite here: text, QR, a code to type on the
// other install) and `fyr pair CODE [-addr host:port]` (join with one).
func pairCommand(data, addr string, args []string) error {
	if len(args) == 0 {
		out, err := ownerCall(data, "POST", "/pair/invite", nil)
		if err != nil {
			return err
		}
		if q, _ := out["qr"].(string); q != "" {
			fmt.Print(q)
		}
		fmt.Printf("%v\nThe invite works once, for 10 minutes. On the other install run: fyr pair CODE\n(add -addr host:port when it cannot find this install by itself)\n", out["code"])
		return nil
	}
	out, err := ownerCall(data, "POST", "/pair/join", map[string]string{"code": args[0], "addr": addr})
	if err != nil {
		return err
	}
	fmt.Printf("Paired with %v (%v).\n", out["name"], out["addr"])
	return nil
}
