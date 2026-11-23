package front

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/httpguard"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/meros/fyr/internal/stream"
)

// followOne follows a host until ctx ends or the host is dropped (removed from
// the roster).
func (f *Front) followOne(ctx context.Context, l *link) {
	lctx, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	l.cancel, l.ctx = cancel, lctx
	gone := l.gone
	f.mu.Unlock()
	if gone {
		cancel()
		return
	}
	f.follow(lctx, l)
}

func (f *Front) follow(ctx context.Context, l *link) {
	backoff := httpguard.Backoff{Min: f.opt.MinBackoff, Max: 15 * time.Second}
	for {
		start := time.Now()
		err := f.session(ctx, l)
		if ctx.Err() != nil {
			return
		}
		f.mu.Lock()
		was := l.online
		f.mu.Unlock()
		if was {
			// A stream that ends (the host dropped a slow subscriber, or it
			// restarts) is tried again at once; the mark waits for that try.
			// A network blip (a dead pooled connection, a link that resets
			// as the Mac wakes) fails the first try too, and the host answers
			// a second later: the dot waits for two more tries, a pause apart.
			for try := 0; try < 3; try++ {
				if try > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(f.opt.MinBackoff << try):
					}
				}
				if err = f.session(ctx, l); ctx.Err() != nil {
					return
				}
			}
		}
		f.setOffline(l, err)
		if time.Since(start) > 30*time.Second {
			backoff.Reset()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff.Next()):
		}
	}
}

const (
	// hostCallTimeout bounds one write or small call to a host.
	hostCallTimeout = 10 * time.Second
	// hostReadTimeout bounds a read of a host's state or settings.
	hostReadTimeout = 30 * time.Second
	// tailnetLookupTimeout bounds the lookup of a node on the tailnet during sign-in.
	tailnetLookupTimeout = 3 * time.Second
)

// idle is how long a stream may stay silent (the host pings every 25 s)
// before the front counts the host as gone.
const idle = 60 * time.Second

// session subscribes to the host's stream, loads its state and follows it
// until the stream ends (subscribe first, then the snapshot, as the client
// does: nothing is lost between them).
func (f *Front) session(ctx context.Context, l *link) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(idle, cancel)
	defer watchdog.Stop()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.base.String()+"/api/stream", nil)
	req.Header.Set("Accept-Encoding", "identity") // each frame at once
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("stream: %s", resp.Status)
	}
	rd := bufio.NewReader(resp.Body)
	next := func() (string, []byte, error) {
		var ev string
		var data []byte
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return "", nil, err
			}
			watchdog.Reset(idle)
			line = bytes.TrimRight(line, "\r\n")
			switch {
			case bytes.HasPrefix(line, []byte("event: ")):
				ev = string(line[7:])
			case bytes.HasPrefix(line, []byte("data: ")):
				data = append([]byte(nil), line[6:]...)
			case len(line) == 0 && ev != "":
				return ev, data, nil
			}
		}
	}
	ev, _, err := next()
	if err != nil {
		return err
	}
	if ev != "hello" {
		return fmt.Errorf("stream began with %q, not hello", ev)
	}
	var st map[string]any
	if err := f.get(ctx, l, "/api/state", &st); err != nil {
		return err
	}
	f.load(l, st)
	// The host patches against what it sent on this connection (V0.38).
	conn := map[string]map[string]any{}
	for {
		ev, data, err := next()
		if err != nil {
			return err
		}
		f.apply(ctx, l, conn, ev, data)
	}
}

func (f *Front) get(ctx context.Context, l *link, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, hostReadTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.base.String()+path, nil)
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber() // revs are nanosecond counts above 2^53
	return dec.Decode(v)
}

func decode(b []byte) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	err := dec.Decode(&m)
	return m, err
}

func revOf(m map[string]any) int64 {
	switch v := m["rev"].(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

// load takes a host's whole state after (re)connecting.
func (f *Front) load(l *link, st map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := map[string]any{}
	for _, k := range []string{"actions", "profiles", "deploy", "stall", "user", "host", "hostId", "hostOrder", "machine", "os", "hostMarks", "agentMark", "update"} {
		if v, ok := st[k]; ok {
			env[k] = v
		}
	}
	l.setEnv(env)
	f.pinLocked(l)
	raw := map[string]map[string]any{}
	ts, _ := st["threads"].([]any)
	for _, x := range ts {
		if d, ok := x.(map[string]any); ok {
			if id, _ := d["id"].(string); id != "" {
				raw[id] = d
				// V0.181: a stalled host's state is loaded beside its live
				// stream; a newer frame taken meanwhile is kept.
				if old := l.raw[id]; old != nil && revOf(old) > revOf(d) {
					raw[id] = old
				}
			}
		}
	}
	// V0.54: a thread whose attention changed while the front was not
	// following the host (an outage, a front restart) still notifies.
	for id, m := range raw {
		if old := l.raw[id]; old != nil && revOf(m) > revOf(old) {
			f.noticeLocked(l, old, m)
		}
	}
	l.raw = raw
	l.dirty = true
	l.projects = nil
	ps, _ := st["projects"].([]any)
	for _, x := range ps {
		if p, ok := x.(map[string]any); ok {
			l.projects = append(l.projects, f.stampProject(l, p))
		}
	}
	if !l.online {
		f.opt.Logf("front: host %s online (%d threads)", l.name(), len(raw))
	}
	l.online, l.since, l.err = true, time.Now(), ""
	f.catchUpLocked(l)      // V0.132
	f.catchUpMarksLocked(l) // V0.171
	f.syncLinksLocked()     // decision 0034
	f.publishHostsLocked()
}

func (f *Front) stampProject(l *link, p map[string]any) map[string]any {
	out := make(map[string]any, len(p)+1)
	for k, v := range p {
		out[k] = v
	}
	out["host"] = l.name()
	return out
}

// apply takes one frame of a host's stream.
func (f *Front) apply(ctx context.Context, l *link, conn map[string]map[string]any, ev string, data []byte) {
	switch ev {
	case "thread":
		m, err := decode(data)
		if err != nil {
			return
		}
		id, _ := m["id"].(string)
		conn[id] = m
		f.take(l, m)
	case "thread.patch":
		p, err := decode(data)
		if err != nil {
			return
		}
		id, _ := p["id"].(string)
		base, _ := p["base"].(json.Number)
		b, _ := base.Int64()
		patch, _ := p["patch"].(map[string]any)
		var m map[string]any
		if c := conn[id]; c != nil && revOf(c) == b && patch != nil {
			m = stream.ApplyMergePatch(c, patch)
		} else if err := f.get(ctx, l, "/api/threads/"+url.PathEscape(id), &m); err != nil {
			return
		}
		conn[id] = m
		f.take(l, m)
	case "project":
		p, err := decode(data)
		if err != nil {
			return
		}
		f.mu.Lock()
		if p["deleted"] == true { // V0.58: the host deleted it
			kept := l.projects[:0]
			for _, x := range l.projects {
				if x["id"] != p["id"] {
					kept = append(kept, x)
				}
			}
			l.projects, l.dirty = kept, true
			f.publishFrameLocked(l, "project", data)
			f.mu.Unlock()
			return
		}
		sp := f.stampProject(l, p)
		replaced := false
		for i, x := range l.projects {
			if x["id"] == sp["id"] {
				l.projects[i], replaced = sp, true
			}
		}
		if !replaced {
			l.projects = append([]map[string]any{sp}, l.projects...)
		}
		l.dirty = true
		f.syncLinksLocked() // decision 0034
		// The host's own project; the frame names its host by id.
		f.publishFrameLocked(l, "project", data)
		f.mu.Unlock()
	case "hostMarks": // V0.171: the host keeps newer host marks
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["hostMarks"] = d
			l.dirty = true
			f.publishMarksLocked()
			f.mu.Unlock()
		}
	case "agentMark": // V0.208: the host's agent set its mark, or a setup run started
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["agentMark"] = d
			l.dirty = true
			f.publishMarksLocked()
			f.mu.Unlock()
		}
	case "update": // V0.host-updates: the host's update state, on its row in Settings → Hosts
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["update"] = d
			l.dirty = true
			f.publishHostsLocked()
			f.mu.Unlock()
		}
	case "hostOrder": // V0.132: the host keeps a newer host order
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["hostOrder"] = d
			l.dirty = true
			f.publishHostsLocked()
			f.mu.Unlock()
		}
	case "deploy":
		// V0.54: a deploy is per host (V0.24's drain runs on the host). Every
		// host's goes out with its id; the client's banner is the one of the
		// host the front runs in (decision 0031 2.6).
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["deploy"] = d
			l.dirty = true
			f.mu.Unlock()
		}
		f.publishFrame(l, ev, data)
	case "stall":
		// V0.147: every host's stall shows, named by its host.
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["stall"] = d
			l.dirty = true
			f.publishFrameLocked(l, ev, data) // named by its host's id
			f.mu.Unlock()
			// V0.181: a host that reports a stall and then does not answer
			// is offline until it does.
			what, _ := d["what"].(string)
			f.suspect(l, "the host reported a stall: "+what)
		}
	case stream.FrameQuotaReset: // V0.quota-reset-wake: push if the thread still waits after the window reset
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			f.quotaResetLocked(l, d)
			f.mu.Unlock()
		}
	default: // event and anything newer: the client ignores what it does not know
		f.publishFrame(l, ev, data)
	}
}

// take stores a newer version of a host's thread and publishes it.
func (f *Front) take(l *link, m map[string]any) {
	id, _ := m["id"].(string)
	if id == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	old := l.raw[id]
	if old != nil && revOf(m) < revOf(old) {
		return
	}
	f.noticeLocked(l, old, m)
	l.raw[id] = m
	if b, err := json.Marshal(m); err == nil { // the host's own thread, whole
		f.publishFrameLocked(l, "thread", b)
	}
}

// setOffline marks a host unreachable: its threads stay, marked read-only.
func (f *Front) setOffline(l *link, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l.online || l.since.IsZero() {
		l.since = time.Now()
		msg := "unreachable"
		if err != nil {
			msg = err.Error()
		}
		f.opt.Logf("front: host %s offline: %s", l.name(), msg)
	}
	l.online = false
	l.dirty = true
	l.err = "unreachable"
	if err != nil && !errors.Is(err, io.EOF) {
		l.err = err.Error()
	}
	f.publishHostsLocked() // the client reads "offline, read-only" from hosts[]
}
