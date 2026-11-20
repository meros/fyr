// Package desktop is the logic of Fyr's Linux app (decision 0038), kept out
// of the GTK shell (cmd/fyr-desktop) so it is tested without a display: the
// owner socket's one-time link and native push device, the systemd unit
// behind Pause and Resume, the window's remembered size, which links leave
// for the browser, and when a signed-out window asks for a new link.
//
// The app is a push device as Fyr.app is (V0.mac-notify): fyrd decides
// what to push to it, by the same rules and settings as a phone, and the
// app only shows it.
package desktop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Owner talks to fyrd's owner socket (V0.130, decision 0028): only this
// user reaches it.
type Owner struct {
	Socket string
}

func (o Owner) client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", o.Socket)
		}}}
}

// Once asks for a one-time sign-in link, as `fyr open` does.
func (o Owner) Once() (string, error) {
	resp, err := o.client(5*time.Second).Post("http://owner/session/once", "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("Fyr does not answer on %s (is it running, as this user?): %w", o.Socket, err)
	}
	defer resp.Body.Close()
	var out struct{ URL, Error string }
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out.URL == "" {
		return "", fmt.Errorf("Fyr refused: %s %s", resp.Status, out.Error)
	}
	return out.URL, nil
}

// ErrUnknown: fyrd no longer knows the endpoint (the device was removed);
// the app stores itself again.
var ErrUnknown = errors.New("fyrd does not know this device")

// Register stores this app as a native push device (POST /push/native,
// V0.mac-notify) and returns its endpoint: the one it holds while fyrd
// still knows it, so the device's settings survive restarts.
func (o Owner) Register(endpoint, device string) (string, error) {
	body, _ := json.Marshal(map[string]string{"endpoint": endpoint, "device": device})
	resp, err := o.client(5*time.Second).Post("http://owner/push/native", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		OK       bool
		Endpoint string
		Error    string
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || !strings.HasPrefix(out.Endpoint, NativePrefix) {
		return "", fmt.Errorf("POST /push/native: %s %s", resp.Status, out.Error)
	}
	return out.Endpoint, nil
}

// NativePrefix starts a native device's endpoint (push.NativePrefix).
const NativePrefix = "native:"

// Notice is a push for this app: a line of /push/native/stream (a
// push.Payload), with Ref the thread it opens (`<hostId>/<threadId>`, or
// the id alone).
type Notice struct {
	Title string
	Body  string
	State string
	Ref   string
}

func noticeOf(line []byte) (Notice, bool) {
	var p struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		ThreadID string `json:"threadId"`
		Host     string `json:"host"`
		State    string `json:"state"`
	}
	if json.Unmarshal(line, &p) != nil || (p.Title == "" && p.Body == "") {
		return Notice{}, false
	}
	ref := p.ThreadID
	if p.Host != "" && ref != "" {
		ref = p.Host + "/" + ref
	}
	return Notice{Title: p.Title, Body: p.Body, State: p.State, Ref: ref}, true
}

// silence is how long the stream may say nothing before it counts as gone:
// fyrd sends an empty line every 30 s.
const silence = 90 * time.Second

// Follow reads endpoint's pushes until the stream ends or ctx does. up is
// called when the stream answers.
func (o Owner) Follow(ctx context.Context, endpoint string, up func(), show func(Notice)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://owner/push/native/stream?endpoint="+url.QueryEscape(endpoint), nil)
	resp, err := o.client(0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ErrUnknown
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("push stream: %s", resp.Status)
	}
	up()
	alive := time.AfterFunc(silence, cancel)
	defer alive.Stop()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		alive.Reset(silence)
		if n, ok := noticeOf(sc.Bytes()); ok {
			show(n)
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return fmt.Errorf("push stream ended")
}

// Ping reports whether fyrd answers on the owner socket.
func (o Owner) Ping() bool {
	resp, err := o.client(3 * time.Second).Get("http://owner/auth/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}
