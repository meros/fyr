package front

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// SPEC V0.171: the owner's host marks, an icon and a colour per host by its
// install id. As the host order (V0.132), they are the owner's, not this
// front's: front.hostMark changes one host's mark, the front writes the whole
// set, stamped now, to every host it follows (host.hostMarks, newest wins),
// keeps a copy, and every front reads the newest from its hosts. The state's
// hostMarks is that set by id; the stream event "hostMarks" sends it again
// when it changes. A host that was offline gets it when it is back.

const (
	KindHostMark = "front.hostMark"
	hostMarksKey = "front.hostMarks"
)

type hostMark struct {
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	// By says who set each field (V0.208). Setup is the host's last setup
	// run; it is only in what the browsers get.
	By    core.HostMarkBy    `json:"by,omitzero"`
	Setup *core.ProjectSetup `json:"setup,omitempty"`
}

type hostMarks struct {
	Marks map[string]hostMark `json:"marks"`
	At    time.Time           `json:"at"`
}

// hostMarksOf reads a host's copy from its state ({marks, at}).
func hostMarksOf(v any) hostMarks {
	o := hostMarks{Marks: map[string]hostMark{}}
	m, _ := v.(map[string]any)
	if m == nil {
		return o
	}
	if ms, ok := m["marks"].(map[string]any); ok {
		for id, x := range ms {
			e, _ := x.(map[string]any)
			icon, _ := e["icon"].(string)
			color, _ := e["color"].(string)
			o.Marks[id] = hostMark{Icon: icon, Color: color, By: core.HostMarkByOf(e["by"])}
		}
	}
	if s, ok := m["at"].(string); ok {
		o.At, _ = time.Parse(time.RFC3339Nano, s)
	}
	return o
}

func (f *Front) loadHostMarks() {
	if f.opt.Store == nil {
		return
	}
	if raw, err := f.opt.Store.GetKV(hostMarksKey); err == nil && raw != "" {
		json.Unmarshal([]byte(raw), &f.marks)
	}
}

// newestMarksLocked are the newest marks the front knows: its own, or a host's.
func (f *Front) newestMarksLocked() hostMarks {
	o := f.marks
	for _, l := range f.all() {
		if h := hostMarksOf(l.env["hostMarks"]); h.At.After(o.At) {
			o = h
		}
	}
	if o.Marks == nil {
		o.Marks = map[string]hostMark{}
	}
	return o
}

// agentMarkOf reads what a host's agent set from its state (V0.208).
func agentMarkOf(v any) hostMark {
	m, _ := v.(map[string]any)
	var o hostMark
	o.Icon, _ = m["icon"].(string)
	o.Color, _ = m["color"].(string)
	if s, ok := m["setup"].(map[string]any); ok {
		o.Setup = &core.ProjectSetup{}
		o.Setup.Thread, _ = s["thread"].(string)
		o.Setup.Error, _ = s["error"].(string)
		at, _ := s["at"].(string)
		o.Setup.At, _ = time.Parse(time.RFC3339Nano, at)
	}
	return o
}

// shownMark is one host's mark as it shows (V0.208): the owner's fields, and
// the agent's value in each field the owner has not set.
func shownMark(own, agent hostMark) hostMark {
	m := own
	m.By = core.HostMarkOwned(own.Icon, own.Color, own.By)
	if m.By.Icon == "" && agent.Icon != "" {
		m.Icon, m.By.Icon = agent.Icon, core.ByAgent
	}
	if m.By.Color == "" && agent.Color != "" {
		m.Color, m.By.Color = agent.Color, core.ByAgent
	}
	m.Setup = agent.Setup
	return m
}

// shownMarksLocked are the marks the browsers get, by host install id.
func (f *Front) shownMarksLocked() map[string]hostMark {
	own := f.newestMarksLocked().Marks
	agents := map[string]hostMark{}
	for _, l := range f.all() {
		id := l.reportedID()
		if id == "" {
			id = f.pins[l.name()]
		}
		if a := agentMarkOf(l.env["agentMark"]); id != "" && (a.Icon != "" || a.Color != "" || a.Setup != nil) {
			agents[id] = a
		}
	}
	out := make(map[string]hostMark, len(own)+len(agents))
	for id, m := range own {
		out[id] = shownMark(m, agents[id])
	}
	for id, a := range agents {
		out[id] = shownMark(own[id], a)
	}
	return out
}

// publishMarksLocked sends the marks to the browsers; the caller holds f.mu.
func (f *Front) publishMarksLocked() {
	if b, err := json.Marshal(map[string]any{"marks": f.shownMarksLocked()}); err == nil {
		f.publishFront("hostMarks", b)
	}
}

func (f *Front) pushMarks(ctx context.Context, l *link, o hostMarks, commandID string) error {
	marks := map[string]any{}
	for id, m := range o.Marks {
		marks[id] = map[string]any{"icon": m.Icon, "color": m.Color, "by": core.HostMarkOwned(m.Icon, m.Color, m.By).Map()}
	}
	body, _ := json.Marshal(map[string]any{"commandId": commandID, "action": core.KindHostMarksSet, "args": map[string]any{"marks": marks, "at": o.At.UTC().Format(time.RFC3339Nano)}})
	ctx, cancel := context.WithTimeout(ctx, hostCallTimeout)
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

// catchUpMarksLocked sends the newest marks to a host that keeps older ones.
func (f *Front) catchUpMarksLocked(l *link) {
	o := f.newestMarksLocked()
	if o.At.IsZero() || !o.At.After(hostMarksOf(l.env["hostMarks"]).At) {
		return
	}
	go func() {
		if err := f.pushMarks(context.Background(), l, o, "hostMarks:"+o.At.UTC().Format(time.RFC3339Nano)+":"+l.name()); err != nil {
			f.opt.Logf("front: host marks to %s: %v", l.name(), err)
		}
	}()
}

// hostMarkCommand runs front.hostMark {id, icon, color}: one host's mark set,
// the whole set stamped now and written to every online host and kept here.
// A field that differs from what shows becomes the owner's, an emptied one too
// (the automatic value, which no agent replaces); a field sent unchanged stays
// whoever's it was (V0.208, as a project's fields).
func (f *Front) hostMarkCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		id, _ := args["id"].(string)
		icon, _ := args["icon"].(string)
		color, _ := args["color"].(string)
		id, icon, color = strings.TrimSpace(id), strings.TrimSpace(icon), strings.TrimSpace(color)
		if !core.HostMarkOK(id, icon, color) {
			return 400, api.Reply{Error: "a host mark needs the host's id; an icon is a glyph name or an emoji, a colour a palette name", Status: 400}
		}
		f.mu.Lock()
		o := f.newestMarksLocked()
		o.Marks = maps.Clone(o.Marks)
		cur := o.Marks[id]
		next := hostMark{Icon: cur.Icon, Color: cur.Color, By: core.HostMarkOwned(cur.Icon, cur.Color, cur.By)}
		shown := f.shownMarksLocked()[id]
		if icon != shown.Icon {
			next.Icon, next.By.Icon = icon, core.ByOwner
		}
		if color != shown.Color {
			next.Color, next.By.Color = color, core.ByOwner
		}
		if next == (hostMark{}) {
			delete(o.Marks, id)
		} else {
			o.Marks[id] = next
		}
		o.At = time.Now().UTC()
		var on []*link
		for _, l := range f.all() {
			if l.online {
				on = append(on, l)
			}
		}
		f.marks = o
		f.publishMarksLocked()
		f.mu.Unlock()
		if f.opt.Store != nil {
			b, _ := json.Marshal(o)
			if err := f.opt.Store.SetKV(hostMarksKey, string(b)); err != nil {
				f.opt.Logf("front: host marks: %v", err)
			}
		}
		stored := []string{}
		for _, l := range on {
			if err := f.pushMarks(context.Background(), l, o, commandID+":"+l.name()); err != nil {
				f.opt.Logf("front: host marks to %s: %v", l.name(), err)
				continue
			}
			stored = append(stored, l.name())
		}
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"id": id, "stored": stored}}
	})
}
