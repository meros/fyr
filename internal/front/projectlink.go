package front

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
	maxLinkRefs       = 16
)

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

// writeLink stores link on one record, through the host's command path.
func (f *Front) writeLink(ctx context.Context, t linkTarget, link, commandID string) error {
	body, _ := json.Marshal(map[string]any{"commandId": commandID, "action": core.KindProjectLink, "args": map[string]any{"id": t.id, "link": link}})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", t.l.base.String()+"/api/command", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fyr", "1")
	resp, err := t.l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		return nil
	}
	var r struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if strings.HasPrefix(r.Error, "unknown action") {
		return fmt.Errorf("%s needs an update of Fyr to link projects", t.l.name())
	}
	if r.Error == "" {
		r.Error = resp.Status
	}
	return fmt.Errorf("%s: %s", t.l.name(), r.Error)
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
		f.mu.Unlock()
		if link == "" {
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
