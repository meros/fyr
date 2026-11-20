package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// Device is this app as a native push device (V0.mac-notify, as Fyr.app):
// it stores itself on the owner socket, follows its stream across fyrd's
// restarts and updates, and answers the page's fyrPush bridge (web/src/pwa.ts)
// so Settings → Notifications works in the window as on a phone: the bell,
// the test, hidden projects and finished threads.
type Device struct {
	Owner Owner
	Name  string // shown in Settings, "Fyr app on <host>"
	// Load and Save keep the endpoint and the off switch (Prefs).
	Load func() (endpoint string, off bool)
	Save func(endpoint string, off bool)
	// Show posts a push; Up says whether the stream is up.
	Show func(Notice)
	Up   func(bool)

	mu   sync.Mutex
	wake chan struct{}
	stop context.CancelFunc // the current read
}

func (d *Device) poke() {
	d.mu.Lock()
	if d.wake == nil {
		d.wake = make(chan struct{}, 1)
	}
	w := d.wake
	d.mu.Unlock()
	select {
	case w <- struct{}{}:
	default:
	}
}

func (d *Device) waitWake(ctx context.Context, after time.Duration) {
	d.mu.Lock()
	if d.wake == nil {
		d.wake = make(chan struct{}, 1)
	}
	w := d.wake
	d.mu.Unlock()
	var t <-chan time.Time
	if after > 0 {
		t = time.After(after)
	}
	select {
	case <-ctx.Done():
	case <-w:
	case <-t:
	}
}

// Run keeps the stream open until ctx ends: it stores the device, reads,
// and starts again after a pause (1 s, doubling to 15 s) when fyrd restarts
// or forgets the device. While the owner has notifications off it waits for
// On.
func (d *Device) Run(ctx context.Context) {
	pause := time.Second
	for ctx.Err() == nil {
		ep, off := d.Load()
		if off {
			d.Up(d.Owner.Ping())
			d.waitWake(ctx, 15*time.Second)
			continue
		}
		start := time.Now()
		err := d.read(ctx, ep)
		d.Up(false)
		if errors.Is(err, ErrUnknown) {
			d.Save("", false) // removed elsewhere: store a new device
		}
		if time.Since(start) > time.Minute {
			pause = time.Second
		}
		d.waitWake(ctx, pause)
		pause = min(pause*2, 15*time.Second)
	}
}

func (d *Device) read(ctx context.Context, ep string) error {
	ep, err := d.Owner.Register(ep, d.Name)
	if err != nil {
		return err
	}
	d.Save(ep, false)
	rctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.stop = cancel
	d.mu.Unlock()
	defer cancel()
	return d.Owner.Follow(rctx, ep, func() { d.Up(true) }, d.Show)
}

// Reply is the bridge's answer to the page: { on, endpoint?, error? }.
type Reply struct {
	On       bool   `json:"on"`
	Endpoint string `json:"endpoint,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Bridge answers one fyrPush message from the page: {"op": "status" | "on"
// | "off" | "clear", …}. clear ({threadId, state}) closes a notification
// that no longer fits its thread, as the service worker's are closed
// (clearThreadNotification); close is how.
func (d *Device) Bridge(msg []byte, close func(threadID, state string)) (Reply, error) {
	var m struct{ Op, ThreadID, State string }
	if err := json.Unmarshal(msg, &m); err != nil {
		return Reply{}, err
	}
	ep, off := d.Load()
	switch m.Op {
	case "status":
		if off || ep == "" {
			return Reply{On: false}, nil
		}
		return Reply{On: true, Endpoint: ep}, nil
	case "on":
		ep, err := d.Owner.Register(ep, d.Name)
		if err != nil {
			return Reply{On: false, Error: err.Error()}, nil
		}
		d.Save(ep, false)
		d.poke()
		return Reply{On: true, Endpoint: ep}, nil
	case "off":
		// Stop reading first, so the read does not store the device again
		// after the page removes it.
		d.Save(ep, true)
		d.mu.Lock()
		if d.stop != nil {
			d.stop()
		}
		d.mu.Unlock()
		return Reply{On: false, Endpoint: ep}, nil
	case "clear":
		if m.ThreadID != "" && close != nil {
			close(m.ThreadID, m.State)
		}
		return Reply{On: !off && ep != ""}, nil
	}
	return Reply{}, errors.New("unknown op " + m.Op)
}

// RefHasThread reports whether a notification's ref is the thread id
// (`<host>/<id>` or the id alone).
func RefHasThread(ref, threadID string) bool {
	return ref == threadID || strings.HasSuffix(ref, "/"+threadID)
}
