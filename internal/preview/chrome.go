// Package preview takes page screenshots for the show tool (SPEC 0.5
// V0.19) with one long-lived headless Chrome, driven over the DevTools
// protocol in pipe mode (fd 3 and 4, messages ending in NUL). It starts on
// the first shot and stops after Idle without one.
package preview

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/meros/fyr/internal/proc"
)

// FindChrome returns the first Chrome or Chromium on PATH, else the macOS
// app's binary, or "".
func FindChrome() string {
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	for _, p := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

type Chrome struct {
	Bin  string
	Dir  string        // profile directory
	Idle time.Duration // stop after this long without a shot; default 5 min

	mu      sync.Mutex // one shot at a time keeps memory flat
	cmd     *exec.Cmd
	w       io.WriteCloser
	nextID  int
	pmu     sync.Mutex
	pending map[int]chan rpcReply
	events  chan rpcReply
	timer   *time.Timer
	dead    chan struct{}
}

type rpcReply struct {
	ID        int             `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Result    json.RawMessage `json:"result"`
	Params    json.RawMessage `json:"params"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Chrome) start() error {
	inR, inW, err := os.Pipe() // Chrome reads fd 3
	if err != nil {
		return err
	}
	outR, outW, err := os.Pipe() // Chrome writes fd 4
	if err != nil {
		return err
	}
	os.MkdirAll(c.Dir, 0o700)
	cmd := exec.Command(c.Bin, "--headless=new", "--remote-debugging-pipe", "--no-first-run",
		"--no-default-browser-check", "--disable-gpu", "--hide-scrollbars", "--mute-audio",
		"--disable-extensions", "--user-data-dir="+c.Dir, "about:blank")
	cmd.ExtraFiles = []*os.File{inR, outW}
	proc.Group(cmd) // Close stops the whole tree
	if err := cmd.Start(); err != nil {
		return err
	}
	inR.Close()
	outW.Close()
	c.cmd, c.w = cmd, inW
	c.pending = map[int]chan rpcReply{}
	c.events = make(chan rpcReply, 256)
	c.dead = make(chan struct{})
	go c.read(outR, c.dead)
	go func(dead chan struct{}) { cmd.Wait(); close(dead) }(c.dead)
	return nil
}

func (c *Chrome) read(r io.Reader, dead chan struct{}) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		msg, err := br.ReadBytes(0)
		if err != nil {
			return
		}
		var m rpcReply
		if json.Unmarshal(msg[:len(msg)-1], &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.pmu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.pmu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		select {
		case c.events <- m:
		default:
		}
	}
}

func (c *Chrome) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	ch := make(chan rpcReply, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	if _, err := c.w.Write(append(b, 0)); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	case <-c.dead:
		return nil, errors.New("Chrome exited")
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// Shoot loads url at width × height and returns a PNG. With full it
// captures the whole page.
func (c *Chrome) Shoot(ctx context.Context, url string, width, height int, full bool) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || isClosed(c.dead) {
		if err := c.start(); err != nil {
			return nil, err
		}
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	idle := c.Idle
	if idle == 0 {
		idle = 5 * time.Minute
	}
	defer func() { c.timer = time.AfterFunc(idle, c.Close) }()

	var t struct{ TargetID string }
	r, err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return nil, err
	}
	json.Unmarshal(r, &t)
	defer c.call(context.Background(), "", "Target.closeTarget", map[string]any{"targetId": t.TargetID})
	var s struct{ SessionID string }
	if r, err = c.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true}); err != nil {
		return nil, err
	}
	json.Unmarshal(r, &s)
	mobile := width < 600
	scale := 1
	if mobile {
		scale = 2
	}
	if _, err := c.call(ctx, s.SessionID, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": scale, "mobile": mobile}); err != nil {
		return nil, err
	}
	if _, err := c.call(ctx, s.SessionID, "Page.enable", map[string]any{}); err != nil {
		return nil, err
	}
	// Drop events from earlier shots, then navigate and wait for load.
	for len(c.events) > 0 {
		<-c.events
	}
	if _, err := c.call(ctx, s.SessionID, "Page.navigate", map[string]any{"url": url}); err != nil {
		return nil, err
	}
	loadWait := time.After(15 * time.Second)
wait:
	for {
		select {
		case ev := <-c.events:
			if ev.SessionID == s.SessionID && ev.Method == "Page.loadEventFired" {
				break wait
			}
		case <-loadWait:
			break wait // take what has rendered
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	time.Sleep(400 * time.Millisecond) // let fonts and late layout settle
	r, err = c.call(ctx, s.SessionID, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": full})
	if err != nil {
		return nil, err
	}
	var shot struct{ Data string }
	json.Unmarshal(r, &shot)
	return base64.StdEncoding.DecodeString(shot.Data)
}

// Close stops Chrome; the next shot starts it again.
func (c *Chrome) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil {
		return
	}
	c.w.Close()
	proc.Kill(c.cmd)
	<-c.dead
	c.cmd = nil
}

func isClosed(ch chan struct{}) bool {
	if ch == nil {
		return true
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
