package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/auth"
)

// open signs this machine's browser in to Fyr (SPEC V0.130 (1), decision
// 0028): it asks fyrd's owner socket for a one-time link and opens it, in a
// Chrome app window with --app.
func open(data string, app bool) error {
	u, err := onceURL(auth.SocketPath(data, auth.OwnerSocket))
	if err != nil {
		return err
	}
	cmd := browserCommand(runtime.GOOS, app, u, os.Getenv("FYR_BROWSER"), exec.LookPath)
	if cmd == nil {
		fmt.Println(u)
		return fmt.Errorf("no browser found: open the link above within a minute (or set FYR_BROWSER)")
	}
	bin, err := exec.LookPath(cmd[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, cmd, os.Environ())
}

// onceURL asks the owner socket for a one-time link.
func onceURL(sock string) (string, error) {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}}
	resp, err := c.Post("http://owner/session/once", "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("Fyr does not answer on %s (is it running, as this user?): %w", sock, err)
	}
	defer resp.Body.Close()
	var out struct{ URL, Error string }
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out.URL == "" {
		return "", fmt.Errorf("Fyr refused: %s %s", resp.Status, out.Error)
	}
	return out.URL, nil
}

// browserCommand is the command that opens u: FYR_BROWSER (a binary) when
// set, else Chrome or Chromium (--app=u for an app window) or the system's
// opener. nil when there is none.
func browserCommand(goos string, app bool, u, browser string, look func(string) (string, error)) []string {
	if goos == "darwin" && browser == "" {
		if app {
			return []string{"open", "-na", "Google Chrome", "--args", "--app=" + u}
		}
		return []string{"open", u}
	}
	arg := u
	if app {
		arg = "--app=" + u
	}
	if browser != "" {
		return []string{browser, arg}
	}
	if app {
		for _, b := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if p, err := look(b); err == nil {
				return []string{p, arg}
			}
		}
	}
	if p, err := look("xdg-open"); err == nil {
		return []string{p, u}
	}
	return nil
}
