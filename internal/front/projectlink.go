package front

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// Decision 0034: linked projects. Every host keeps its own project record; a
// link id on the records makes them one project to the owner. A link spans
// hosts, so the front writes it: front.projectLink and front.projectUnlink
// name records by ref (<hostId>/<id>), and each host stores its own record's
// link. A link on some members only is a valid state: the reply names the
// hosts that stored it and the hosts that did not.

const (
	KindProjectLink   = "front.projectLink"
	KindProjectUnlink = "front.projectUnlink"
	KindProjectShared = "front.projectShared"
	KindProjectAdd    = "front.projectAdd"
	maxLinkRefs       = 16
)

// sharedKeys are the values a link's members share.
var sharedKeys = []string{"name", "color", "icon", "quiet"}

// linkTarget is one project record a link command writes.
type linkTarget struct {
	l      *link
	host   string // install id
	id     string // the host's own project id
	linked string // its link now
}

// linkFail is why a host did not store a link.
type linkFail struct {
	Host  string `json:"host"`
	Error string `json:"error"`
}

// projectAtLocked is the project a ref names; the caller holds f.mu. The
// error is for the owner, the int its status.
func (f *Front) projectAtLocked(ref string) (linkTarget, int, error) {
	host, id, ok := strings.Cut(ref, "/")
	if !ok || host == "" || id == "" {
		return linkTarget{}, 400, fmt.Errorf("%q is not a project ref (<host id>/<project id>)", ref)
	}
	for _, l := range f.all() {
		if f.idOfLocked(l) != host {
			continue
		}
		if !l.online {
			return linkTarget{}, 409, fmt.Errorf("%s is offline: a link is stored on the host itself, so link it when the host is back", l.name())
		}
		for _, p := range l.projects {
			if p["id"] == id {
				linked, _ := p["link"].(string)
				return linkTarget{l: l, host: host, id: id, linked: linked}, 200, nil
			}
		}
		return linkTarget{}, 404, fmt.Errorf("%s has no project %s", l.name(), id)
	}
	return linkTarget{}, 404, fmt.Errorf("this front follows no host %s", host)
}

// hostCommand runs one project command on a host.
func (f *Front) hostCommand(ctx context.Context, l *link, action, commandID string, args map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"commandId": commandID, "action": action, "args": args})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", l.base.String()+"/api/command", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fyr", "1")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Error  string         `json:"error"`
		Result map[string]any `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if resp.StatusCode == 200 {
		return r.Result, nil
	}
	if strings.HasPrefix(r.Error, "unknown action") {
		return nil, fmt.Errorf("%s needs an update of Fyr to link projects", l.name())
	}
	if r.Error == "" {
		r.Error = resp.Status
	}
	return nil, fmt.Errorf("%s: %s", l.name(), r.Error)
}

// writeLink stores link on one record.
func (f *Front) writeLink(ctx context.Context, t linkTarget, link, commandID string) error {
	_, err := f.hostCommand(ctx, t.l, core.KindProjectLink, commandID, map[string]any{"id": t.id, "link": link})
	return err
}

func newLinkID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "l_" + hex.EncodeToString(b)
}

func refusal(code int, err error) (int, api.Reply) {
	return code, api.Reply{Error: err.Error(), Status: code}
}

// projectLinkCommand runs front.projectLink {refs}: the records become
// members of one link, the one a record already has or a new one. Every host
// named must be online; at most one member per host; two links never merge.
func (f *Front) projectLinkCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		raw, _ := args["refs"].([]any)
		if len(raw) < 2 || len(raw) > maxLinkRefs {
			return 400, api.Reply{Error: fmt.Sprintf("refs must name 2 to %d projects", maxLinkRefs), Status: 400}
		}
		f.mu.Lock()
		var targets []linkTarget
		hosts, link := map[string]string{}, ""
		for _, x := range raw {
			ref, _ := x.(string)
			t, code, err := f.projectAtLocked(ref)
			if err != nil {
				f.mu.Unlock()
				return refusal(code, err)
			}
			if _, dup := hosts[t.host]; dup {
				f.mu.Unlock()
				return 400, api.Reply{Error: t.l.name() + " is named twice: a linked project has one folder per host", Status: 400}
			}
			if t.linked != "" && link != "" && t.linked != link {
				f.mu.Unlock()
				return 409, api.Reply{Error: "these projects belong to two different links: unlink one first", Status: 409}
			}
			if t.linked != "" {
				link = t.linked
			}
			hosts[t.host] = t.id
			targets = append(targets, t)
		}
		// A member the command does not name may sit on a named host.
		for _, l := range f.all() {
			host := f.idOfLocked(l)
			want, named := hosts[host]
			for _, p := range l.projects {
				if named && link != "" && p["link"] == link && p["id"] != want {
					f.mu.Unlock()
					return 409, api.Reply{Error: l.name() + " already has a project in this link: a linked project has one folder per host", Status: 409}
				}
			}
		}
		fresh := link == ""
		first := map[string]any{}
		for _, p := range targets[0].l.projects {
			if p["id"] == targets[0].id {
				first = p
			}
		}
		shared := sharedOf(first)
		f.mu.Unlock()
		if fresh {
			link = newLinkID()
		}
		stored, failed := []string{}, []linkFail{}
		var last error
		for _, t := range targets {
			if t.linked == link {
				stored = append(stored, t.host)
				continue
			}
			if err := f.writeLink(context.Background(), t, link, commandID+":"+t.host); err != nil {
				f.opt.Logf("front: project link on %s: %v", t.l.name(), err)
				failed, last = append(failed, linkFail{t.host, err.Error()}), err
				continue
			}
			stored = append(stored, t.host)
		}
		if len(stored) == 0 {
			return refusal(502, last)
		}
		// A new link takes the first project's values; a member joining an
		// older link gets the link's newest from the catch-up.
		if fresh {
			at := time.Now().UTC()
			for _, t := range targets {
				if slices.Contains(stored, t.host) {
					if err := f.writeShared(context.Background(), t.l, t.id, link, shared, at, commandID+":shared:"+t.host); err != nil {
						f.opt.Logf("front: shared values on %s: %v", t.l.name(), err)
					}
				}
			}
		} else {
			f.mu.Lock()
			f.syncLinksLocked()
			f.mu.Unlock()
		}
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"link": link, "stored": stored, "failed": failed}}
	})
}

// projectUnlinkCommand runs front.projectUnlink {ref}: that record leaves its
// link and keeps its values and its threads.
func (f *Front) projectUnlinkCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		ref, _ := args["ref"].(string)
		f.mu.Lock()
		t, code, err := f.projectAtLocked(ref)
		f.mu.Unlock()
		if err != nil {
			return refusal(code, err)
		}
		if t.linked != "" {
			if err := f.writeLink(context.Background(), t, "", commandID+":"+t.host); err != nil {
				return refusal(502, err)
			}
		}
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"ref": ref}}
	})
}

// linkMember is one record of a link, as the front last saw it.
type linkMember struct {
	l  *link
	id string
	at time.Time
	p  map[string]any
}

func sharedAtOf(p map[string]any) time.Time {
	s, _ := p["sharedAt"].(string)
	at, _ := time.Parse(time.RFC3339Nano, s)
	return at
}

// sharedOf is a record's shared values; a cleared quiet list is sent as null.
func sharedOf(p map[string]any) map[string]any {
	out := map[string]any{"quiet": p["quiet"]}
	for _, k := range sharedKeys[:3] {
		if v, ok := p[k]; ok {
			out[k] = v
		}
	}
	return out
}

// linksLocked is every link's members; the caller holds f.mu.
func (f *Front) linksLocked() map[string][]linkMember {
	out := map[string][]linkMember{}
	for _, l := range f.all() {
		for _, p := range l.projects {
			link, _ := p["link"].(string)
			id, _ := p["id"].(string)
			if link != "" && id != "" {
				out[link] = append(out[link], linkMember{l, id, sharedAtOf(p), p})
			}
		}
	}
	return out
}

func newestMember(ms []linkMember) linkMember {
	best := ms[0]
	for _, m := range ms[1:] {
		if m.at.After(best.at) {
			best = m
		}
	}
	return best
}

// writeShared stores the shared values on one record.
func (f *Front) writeShared(ctx context.Context, l *link, id, link string, vals map[string]any, at time.Time, commandID string) error {
	args := map[string]any{"id": id, "link": link, "at": at.UTC().Format(time.RFC3339Nano)}
	for k, v := range vals {
		args[k] = v
	}
	_, err := f.hostCommand(ctx, l, core.KindProjectShared, commandID, args)
	return err
}

// syncLinksLocked sends a link's newest shared values to each online member
// that keeps older ones (it was offline, or it just joined). The stamp is in
// the command id, so fronts doing this at once change nothing twice. The
// caller holds f.mu.
func (f *Front) syncLinksLocked() {
	for link, ms := range f.linksLocked() {
		best := newestMember(ms)
		if best.at.IsZero() {
			continue
		}
		for _, m := range ms {
			key := link + ":" + best.at.UTC().Format(time.RFC3339Nano) + ":" + m.id
			if !m.l.online || !best.at.After(m.at) || f.linkSync[key] {
				continue
			}
			if f.linkSync == nil || len(f.linkSync) > 1000 {
				f.linkSync = map[string]bool{}
			}
			f.linkSync[key] = true
			go func(m linkMember, vals map[string]any, at time.Time) {
				if err := f.writeShared(context.Background(), m.l, m.id, link, vals, at, "shared:"+key); err != nil {
					f.opt.Logf("front: shared values to %s: %v", m.l.name(), err)
					f.mu.Lock()
					delete(f.linkSync, key) // the next frame tries again
					f.mu.Unlock()
				}
			}(m, sharedOf(best.p), best.at)
		}
	}
}

// checkShared refuses a value no host would take, before any host is written.
func checkShared(args map[string]any) error {
	if v, ok := args["name"]; ok {
		if s, _ := v.(string); strings.TrimSpace(s) == "" {
			return fmt.Errorf("a project needs a name")
		}
	}
	if v, ok := args["color"]; ok {
		if s, _ := v.(string); !core.ValidColor(s) {
			return fmt.Errorf("color must be one of %v", core.Palette)
		}
	}
	if v, ok := args["icon"]; ok {
		var ic core.Icon
		b, _ := json.Marshal(v)
		json.Unmarshal(b, &ic)
		if ic.Kind != "monogram" && ic.Kind != "emoji" && ic.Kind != "line" {
			return fmt.Errorf("icon kind must be monogram, emoji or line")
		}
	}
	if v, ok := args["quiet"]; ok && v != nil {
		var q core.QuietHours
		b, _ := json.Marshal(v)
		if err := json.Unmarshal(b, &q); err != nil {
			return fmt.Errorf("quiet must be a list of {from, to, days?}")
		}
		return q.Validate()
	}
	return nil
}

// projectSharedCommand runs front.projectShared {link, name?, color?, icon?,
// quiet?}: the link's newest values with these changed, stamped later than
// anything this front has seen and written to every online member. An offline
// member catches up when it is back.
func (f *Front) projectSharedCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		if err := checkShared(args); err != nil {
			return refusal(400, err)
		}
		link, _ := args["link"].(string)
		f.mu.Lock()
		ms := f.linksLocked()[link]
		if len(ms) == 0 {
			f.mu.Unlock()
			return 404, api.Reply{Error: "no project has the link " + link, Status: 404}
		}
		best := newestMember(ms)
		vals := sharedOf(best.p)
		f.mu.Unlock()
		for _, k := range sharedKeys {
			if v, ok := args[k]; ok {
				vals[k] = v
			}
		}
		at := time.Now().UTC()
		if !at.After(best.at) {
			at = best.at.Add(time.Millisecond)
		}
		stored, failed := []string{}, []linkFail{}
		var last error
		for _, m := range ms {
			f.mu.Lock()
			host, on := f.idOfLocked(m.l), m.l.online
			f.mu.Unlock()
			if !on {
				continue // it catches up
			}
			if err := f.writeShared(context.Background(), m.l, m.id, link, vals, at, commandID+":"+host); err != nil {
				failed, last = append(failed, linkFail{host, err.Error()}), err
				continue
			}
			stored = append(stored, host)
		}
		if len(stored) == 0 {
			if last == nil {
				last = fmt.Errorf("every host of this project is offline")
			}
			return refusal(502, last)
		}
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"link": link, "stored": stored, "failed": failed}}
	})
}

// projectAddCommand runs front.projectAdd {ref, host, folder}: the project
// the ref names gets a member on that host, made there with its folder. A
// project not yet linked gets a link now.
func (f *Front) projectAddCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		ref, _ := args["ref"].(string)
		host, _ := args["host"].(string)
		folder, _ := args["folder"].(string)
		ctx := context.Background()
		f.mu.Lock()
		from, code, err := f.projectAtLocked(ref)
		if err != nil {
			f.mu.Unlock()
			return refusal(code, err)
		}
		var to *link
		for _, l := range f.all() {
			if f.idOfLocked(l) == host {
				to = l
			}
		}
		switch {
		case to == nil:
			f.mu.Unlock()
			return 404, api.Reply{Error: "this front follows no host " + host, Status: 404}
		case !to.online:
			name := to.name()
			f.mu.Unlock()
			return 409, api.Reply{Error: name + " is offline", Status: 409}
		case to == from.l:
			f.mu.Unlock()
			return 400, api.Reply{Error: "the project is already on " + to.name(), Status: 400}
		}
		var first map[string]any
		for _, p := range from.l.projects {
			if p["id"] == from.id {
				first = p
			}
		}
		link, vals, at := from.linked, sharedOf(first), time.Now().UTC()
		if ms := f.linksLocked()[link]; link != "" {
			for _, m := range ms {
				if m.l == to {
					name := to.name()
					f.mu.Unlock()
					return 409, api.Reply{Error: name + " already has this project", Status: 409}
				}
			}
			if best := newestMember(ms); !best.at.IsZero() {
				vals, at = sharedOf(best.p), best.at
			}
		}
		f.mu.Unlock()
		fresh := link == ""
		if fresh {
			link = newLinkID()
			if err := f.writeLink(ctx, from, link, commandID+":from"); err != nil {
				return refusal(502, err)
			}
			if err := f.writeShared(ctx, from.l, from.id, link, vals, at, commandID+":from-shared"); err != nil {
				return refusal(502, err)
			}
		}
		create := map[string]any{"name": vals["name"], "link": link}
		if folder != "" {
			create["folder"] = folder
		}
		made, err := f.hostCommand(ctx, to, core.KindProjectCreate, commandID+":create", create)
		if err != nil {
			return refusal(502, err)
		}
		id, _ := made["id"].(string)
		t := linkTarget{l: to, host: host, id: id}
		// A host too old for links ignores link in project.create; this refuses there.
		if err := f.writeLink(ctx, t, link, commandID+":link"); err != nil {
			return refusal(502, err)
		}
		if err := f.writeShared(ctx, to, id, link, vals, at, commandID+":shared"); err != nil {
			return refusal(502, err)
		}
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"link": link, "id": host + "/" + id}}
	})
}
