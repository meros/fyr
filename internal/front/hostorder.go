package front

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
)

// SPEC V0.132: the owner's host order. It is the owner's, not this front's:
// front.hostOrder writes it, with the time, to every host the front follows
// (host.hostOrder, newest wins), and every front reads the newest from its
// hosts and from its own copy. hosts[] in the state and the hosts event come
// in that order, and its first host is the default for a call that names none.
// A host that was offline gets it when it is back. The roster of decision
// 0026 will carry it later.

const (
	KindHostOrder = "front.hostOrder"
	hostOrderKey  = "front.hostOrder"
)

type hostOrder struct {
	Names []string  `json:"names"`
	At    time.Time `json:"at"`
}

// hostOrderOf reads a host's copy from its state ({names, at}).
func hostOrderOf(v any) hostOrder {
	m, _ := v.(map[string]any)
	var o hostOrder
	if m == nil {
		return o
	}
	if ns, ok := m["names"].([]any); ok {
		for _, x := range ns {
			if s, ok := x.(string); ok {
				o.Names = append(o.Names, s)
			}
		}
	}
	if s, ok := m["at"].(string); ok {
		o.At, _ = time.Parse(time.RFC3339Nano, s)
	}
	return o
}

func (f *Front) loadHostOrder() {
	if f.opt.Store == nil {
		return
	}
	if raw, err := f.opt.Store.GetKV(hostOrderKey); err == nil && raw != "" {
		json.Unmarshal([]byte(raw), &f.hosts)
	}
}

// newestLocked is the newest order the front knows: its own, or a host's.
func (f *Front) newestLocked() hostOrder {
	o := f.hosts
	for _, l := range f.all() {
		if h := hostOrderOf(l.env["hostOrder"]); h.At.After(o.At) {
			o = h
		}
	}
	return o
}

// orderedLocked is the hosts in the owner's order; a host the order does
// not name keeps its place after them, in the config's order.
func (f *Front) orderedLocked() []*link {
	ls := slices.Clone(f.all())
	names := f.newestLocked().Names
	rank := func(l *link) int {
		if i := slices.Index(names, l.name()); i >= 0 {
			return i
		}
		return len(names)
	}
	slices.SortStableFunc(ls, func(a, b *link) int { return rank(a) - rank(b) })
	return ls
}

// pushOrder writes the order to one host.
func (f *Front) pushOrder(ctx context.Context, l *link, o hostOrder, commandID string) error {
	names := make([]any, len(o.Names))
	for i, n := range o.Names {
		names[i] = n
	}
	body, _ := json.Marshal(map[string]any{"commandId": commandID, "action": "host.hostOrder", "args": map[string]any{"names": names, "at": o.At.UTC().Format(time.RFC3339Nano)}})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", l.base.String()+"/api/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fyr", "1")
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", l.name(), resp.Status)
	}
	return nil
}

// catchUpLocked sends the newest order to a host that keeps an older one
// (it was offline, or the order was set before it was followed).
func (f *Front) catchUpLocked(l *link) {
	o := f.newestLocked()
	if len(o.Names) == 0 || !o.At.After(hostOrderOf(l.env["hostOrder"]).At) {
		return
	}
	go func() {
		if err := f.pushOrder(context.Background(), l, o, "hostOrder:"+o.At.UTC().Format(time.RFC3339Nano)+":"+l.name()); err != nil {
			f.opt.Logf("front: host order to %s: %v", l.name(), err)
		}
	}()
}

// hostOrderCommand runs front.hostOrder {names}: the owner's order, stamped
// now, written to every online host and kept here.
func (f *Front) hostOrderCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		raw, ok := args["names"].([]any)
		if !ok || len(raw) > 100 {
			return 400, api.Reply{Error: "names must be a list of at most 100 host names", Status: 400}
		}
		o := hostOrder{At: time.Now().UTC()}
		for _, x := range raw {
			s, _ := x.(string)
			if s = strings.TrimSpace(s); s != "" && !slices.Contains(o.Names, s) {
				o.Names = append(o.Names, s)
			}
		}
		f.mu.Lock()
		var on []*link
		for _, l := range f.all() {
			if l.online {
				on = append(on, l)
			}
		}
		f.hosts = o
		f.mu.Unlock()
		if f.opt.Store != nil {
			b, _ := json.Marshal(o)
			if err := f.opt.Store.SetKV(hostOrderKey, string(b)); err != nil {
				f.opt.Logf("front: host order: %v", err)
			}
		}
		stored := []string{}
		for _, l := range on {
			if err := f.pushOrder(context.Background(), l, o, commandID+":"+l.name()); err != nil {
				f.opt.Logf("front: host order to %s: %v", l.name(), err)
				continue
			}
			stored = append(stored, l.name())
		}
		f.mu.Lock()
		f.publishHostsLocked()
		f.mu.Unlock()
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"names": o.Names, "stored": stored}}
	})
}
