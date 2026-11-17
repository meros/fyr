package front

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/identity"
)

// V0.124 (decision 0026, slice 1): every install has a key, and a host
// reports its id in its state. The front pins the id it first sees for each
// host, by name, and says so when the host later reports another: a new
// install under an old name (a reinstall, a second machine with the same
// name). Nothing is refused yet; trust by key comes with the roster (slice 2).

const (
	// KindHostID trusts the id a host reports now in place of the pinned one
	// (V0.124). It is the front's own command, as front.projectOrder is.
	KindHostID = "front.hostId"
	hostIDsKey = "front.hostIds"
)

func (f *Front) loadPins() {
	f.pins = map[string]string{}
	if f.opt.Store == nil {
		return
	}
	raw, err := f.opt.Store.GetKV(hostIDsKey)
	if err != nil || raw == "" {
		return
	}
	if err := json.Unmarshal([]byte(raw), &f.pins); err != nil {
		f.opt.Logf("front: host ids: %v", err)
		f.pins = map[string]string{}
	}
}

// savePinsLocked stores the pins; the caller holds f.mu.
func (f *Front) savePinsLocked() error {
	if f.opt.Store == nil {
		return nil
	}
	b, _ := json.Marshal(f.pins)
	return f.opt.Store.SetKV(hostIDsKey, string(b))
}

// reportedID is the id the host sent in its state, or "" (a host older
// than V0.124); the caller holds f.mu.
func (l *link) reportedID() string {
	s, _ := l.env["hostId"].(string)
	return s
}

// pinLocked pins a host's first id, or marks a changed one; the caller
// holds f.mu.
func (f *Front) pinLocked(l *link) {
	id := l.reportedID()
	if id == "" {
		return
	}
	name := l.name()
	switch was := f.pins[name]; {
	case was == "":
		f.pins[name] = id
		if err := f.savePinsLocked(); err != nil {
			f.opt.Logf("front: host ids: %v", err)
		}
		l.idWas = ""
	case was != id:
		if l.idWas != was {
			f.opt.Logf("front: host %s reports key id %s, not the %s it had: another install under its name (Settings → Hosts)", name, id, was)
		}
		l.idWas = was
	default:
		l.idWas = ""
	}
}

// hostLocked is one host's row in hosts[]; the caller holds f.mu.
func (f *Front) hostLocked(l *link) api.Host {
	h := api.Host{Name: l.name(), Online: l.online, Found: l.cfg.Found, ID: l.reportedID(), IDWas: l.idWas, URL: strings.TrimRight(l.cfg.URL, "/")}
	if h.ID == "" {
		h.ID = f.pins[h.Name]
	}
	h.Link = f.linkOf(l)
	if !l.since.IsZero() {
		h.Since = l.since.UTC().Format(time.RFC3339)
	}
	switch {
	case l.cfg.Dial != nil:
		h.Link = &api.Link{State: "up", Via: "pipe"}
	case l.lt != nil:
		ls := l.lt.linkStatus()
		h.Link = &ls
	}
	if !l.online && l.err != "" {
		h.Error = l.err
	}
	if ps, ok := l.env["profiles"].([]any); ok {
		h.Profiles = ps
	}
	if d := l.env["deploy"]; d != nil {
		h.Deploy = d
	}
	// V0.170: what it runs on, for its mark.
	h.Machine, _ = l.env["machine"].(string)
	h.OS, _ = l.env["os"].(string)
	return h
}

// publishHostsLocked sends hosts[] as the stream event "hosts" (V0.124), so
// Settings → Hosts follows a host's state and id without a reload; the
// caller holds f.mu.
func (f *Front) publishHostsLocked() {
	hs := []api.Host{}
	for _, l := range f.orderedLocked() { // V0.132: the owner's order
		hs = append(hs, f.hostLocked(l))
	}
	b, err := json.Marshal(map[string]any{"hosts": hs})
	if err == nil {
		f.hub.Publish("hosts", b)
	}
}

// hostIDCommand runs front.hostId {host}: the owner trusts the id the host
// reports now. Owner only (the browser's command path with X-Fyr; no host
// has the kind, so no agent tool sends it).
func (f *Front) hostIDCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		name, _ := args["host"].(string)
		f.mu.Lock()
		defer f.mu.Unlock()
		var l *link
		for _, x := range f.all() {
			if x.name() == name {
				l = x
			}
		}
		if name == "" || l == nil {
			return 400, api.Reply{Error: "no such host: " + name, Status: 400}
		}
		id := l.reportedID()
		if id == "" {
			return 409, api.Reply{Error: name + " has reported no id yet: it must be online and run V0.124 or later", Status: 409}
		}
		was := f.pins[name]
		f.pins[name] = id
		if err := f.savePinsLocked(); err != nil {
			f.pins[name] = was
			return 500, api.Reply{Error: "Could not save the host's id: " + err.Error(), Status: 500}
		}
		l.idWas = ""
		f.publishHostsLocked()
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"host": name, "id": id, "short": identity.Short(id)}}
	})
}
