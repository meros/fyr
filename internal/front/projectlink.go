package front

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
var sharedKeys = []string{"name", "color", "icon", "quiet", "quietNow", "quietLift", "priority", "agentSettle", "toolApproval"} // V0.linked-agent-settle, V0.tool-approval-judge, V0.quiet-now

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
	ctx, cancel := context.WithTimeout(ctx, hostCallTimeout)
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
	decErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r)
	if resp.StatusCode == 200 {
		if decErr != nil {
			return nil, fmt.Errorf("%s: the answer was not understood: %w", l.name(), decErr)
		}
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
		// Every named host is written, also one this front already shows with
		// the link: its copy of the record is a frame behind the host, so the
		// host may not have it (V0.link-writes-through-a-stale-copy). The
		// write is the same reduce for a record that has the link already.
		for _, t := range targets {
			if err := f.writeLink(context.Background(), t, link, commandID+":"+t.host); err != nil {
				f.opt.Logf("front: project link on %s: %v", t.l.name(), err)
				failed, last = append(failed, linkFail{t.host, err.Error()}), err
				continue
			}
			f.noteLink(t.l, t.id, link)
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
						continue
					}
					f.noteShared(t.l, t.id, shared, at)
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
		// Written even when this front shows the record without a link: its
		// copy is a frame behind the host (V0.link-writes-through-a-stale-copy).
		if err := f.writeLink(context.Background(), t, "", commandID+":"+t.host); err != nil {
			return refusal(502, err)
		}
		f.noteLink(t.l, t.id, "")
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

// sharedOf is a record's shared values; a cleared quiet list, a normal
// priority (V0.project-priority), no quiet stretch started by hand
// (V0.quiet-now), and ask for agentSettle / toolApproval are sent as null.
func sharedOf(p map[string]any) map[string]any {
	out := map[string]any{"quiet": p["quiet"], "quietNow": p["quietNow"], "quietLift": p["quietLift"], "priority": p["priority"], "agentSettle": p["agentSettle"], "toolApproval": p["toolApproval"]}
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

// noteShared puts what a host just stored into this front's copy of its
// project, without waiting for the host's event: an edit that follows at once
// starts from the newest values, not from the older ones the event has yet to
// replace (it would write the old ones back). The event, when it comes, is
// the same record.
func (f *Front) noteShared(l *link, id string, vals map[string]any, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range l.projects {
		if pid, _ := p["id"].(string); pid != id {
			continue
		}
		if sharedAtOf(p).After(at) {
			return // a newer record is already here
		}
		cp := make(map[string]any, len(p)+1)
		for k, v := range p {
			cp[k] = v
		}
		for k, v := range vals {
			cp[k] = v
		}
		cp["sharedAt"] = at.UTC().Format(time.RFC3339Nano)
		l.projects[i] = cp
		return
	}
}

// noteLink puts the link a host just stored into this front's copy of its
// project, as noteShared does for the values (V0.shared-edit-after-link): a
// shared edit that follows at once reaches the member this front just linked,
// instead of skipping it until the host's next frame carries the link. The
// event, when it comes, is the same record.
func (f *Front) noteLink(l *link, id, link string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range l.projects {
		if pid, _ := p["id"].(string); pid != id {
			continue
		}
		if p["link"] == link || (link == "" && p["link"] == nil) {
			return
		}
		cp := make(map[string]any, len(p)+1)
		for k, v := range p {
			cp[k] = v
		}
		if link == "" {
			delete(cp, "link")
		} else {
			cp["link"] = link
		}
		l.projects[i] = cp
		return
	}
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
	f.syncImagesLocked()
	f.syncPlaybooksLocked()
}

func playbookAtOf(p map[string]any) time.Time {
	s, _ := p["playbookAt"].(string)
	at, _ := time.Parse(time.RFC3339Nano, s)
	return at
}

// syncPlaybooksLocked gives each online member of a link the playbook that
// was written last (V0.project-playbook): what an agent learned or the owner
// corrected on one host is known on the others. The playbook has its own
// stamp, playbookAt, as the image has; the member whose stamp is newest is
// the source, and its record as this front last saw it is what is sent, so
// its host need not be online. The caller holds f.mu.
func (f *Front) syncPlaybooksLocked() {
	for link, ms := range f.linksLocked() {
		best := ms[0]
		for _, m := range ms[1:] {
			if playbookAtOf(m.p).After(playbookAtOf(best.p)) {
				best = m
			}
		}
		at := playbookAtOf(best.p)
		if at.IsZero() {
			continue
		}
		for _, m := range ms {
			key := "playbook:" + link + ":" + at.UTC().Format(time.RFC3339Nano) + ":" + m.id
			if !m.l.online || !at.After(playbookAtOf(m.p)) || f.linkSync[key] {
				continue
			}
			if f.linkSync == nil || len(f.linkSync) > 1000 {
				f.linkSync = map[string]bool{}
			}
			f.linkSync[key] = true
			go func(m linkMember, playbook any) {
				args := map[string]any{"id": m.id, "link": link, "at": at.UTC().Format(time.RFC3339Nano), "playbook": playbook}
				if _, err := f.hostCommand(context.Background(), m.l, core.KindProjectSharedPlaybook, key, args); err != nil {
					f.opt.Logf("front: playbook to %s: %v", m.l.name(), err)
					f.mu.Lock()
					delete(f.linkSync, key) // the next frame tries again
					f.mu.Unlock()
				}
			}(m, best.p["playbook"])
		}
	}
}

func imageAtOf(p map[string]any) time.Time {
	s, _ := p["imageAt"].(string)
	at, _ := time.Parse(time.RFC3339Nano, s)
	return at
}

// syncImagesLocked gives each online member of a link the image that was set
// last (V0.linked-values-sync): the member whose imageAt is newest is the
// source, and an online member with an older imageAt gets its picture (read
// from the source's host, so that host must be online) or its removal, with
// the source's imageAt. The image has its own stamp, so a name edit copies no
// picture. The caller holds f.mu.
func (f *Front) syncImagesLocked() {
	for link, ms := range f.linksLocked() {
		best := ms[0]
		for _, m := range ms[1:] {
			if imageAtOf(m.p).After(imageAtOf(best.p)) {
				best = m
			}
		}
		at := imageAtOf(best.p)
		if at.IsZero() {
			continue
		}
		image, _ := best.p["image"].(string)
		by := ""
		if b, ok := best.p["by"].(map[string]any); ok {
			by, _ = b["image"].(string)
		}
		for _, m := range ms {
			key := "image:" + link + ":" + at.UTC().Format(time.RFC3339Nano) + ":" + m.id
			if !m.l.online || !at.After(imageAtOf(m.p)) || f.linkSync[key] || (image != "" && !best.l.online) {
				continue
			}
			if f.linkSync == nil || len(f.linkSync) > 1000 {
				f.linkSync = map[string]bool{}
			}
			f.linkSync[key] = true
			go func(m, src linkMember) {
				err := f.copyImage(context.Background(), src, m, link, image, by, at, key)
				if err != nil {
					f.opt.Logf("front: project image to %s: %v", m.l.name(), err)
					f.mu.Lock()
					delete(f.linkSync, key) // the next frame tries again
					f.mu.Unlock()
				}
			}(m, best)
		}
	}
}

// copyImage stores src's image (or its removal, image "") on m's record.
func (f *Front) copyImage(ctx context.Context, src, m linkMember, link, image, by string, at time.Time, commandID string) error {
	args := map[string]any{"id": m.id, "link": link, "at": at.UTC().Format(time.RFC3339Nano), "image": image, "by": by}
	if image != "" {
		png, err := f.iconOf(ctx, src)
		if err != nil {
			return err
		}
		args["png"] = base64.StdEncoding.EncodeToString(png)
	}
	_, err := f.hostCommand(ctx, m.l, core.KindProjectSharedImage, commandID, args)
	return err
}

// iconOf reads a member's stored icon, the larger square.
func (f *Front) iconOf(ctx context.Context, m linkMember) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, hostCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", m.l.base.String()+"/api/projects/"+url.PathEscape(m.id)+"/icon?size=128", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: icon: %s", m.l.name(), resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
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
	if v, ok := args["priority"]; ok && v != nil {
		if s, _ := v.(string); !core.ValidPriority(s) {
			return fmt.Errorf("priority must be high, normal or low")
		}
	}
	if v, ok := args["agentSettle"]; ok && v != nil {
		if s, _ := v.(string); s != core.AgentSettleAsk && s != core.AgentSettleAuto {
			return fmt.Errorf("agentSettle must be ask or auto")
		}
	}
	if v, ok := args["toolApproval"]; ok && v != nil {
		if s, _ := v.(string); s != core.ToolApprovalAsk && s != core.ToolApprovalAuto {
			return fmt.Errorf("toolApproval must be ask or auto")
		}
	}
	if v, ok := args["quietNow"]; ok {
		if _, err := core.ParseQuietNow(v, time.Now()); err != nil {
			return err
		}
	}
	if v, ok := args["quietLift"]; ok { // V0.quiet-lift
		if _, err := core.ParseQuietLift(v, time.Now()); err != nil {
			return err
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
// quiet?, quietNow?, quietLift?, priority?, agentSettle?}: the link's newest values with these changed, stamped later than
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
			f.noteShared(m.l, m.id, vals, at)
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
