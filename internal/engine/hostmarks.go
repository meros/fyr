package engine

import (
	"encoding/json"
	"log"
	"maps"
	"time"

	"github.com/meros/fyr/internal/stream"
)

// hostMarksKey is the host kv key of the owner's host marks (V0.171).
const hostMarksKey = "hostMarks"

// HostMark is one host's icon and colour as the owner set them; empty is
// automatic.
type HostMark struct {
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
}

// HostMarks are the owner's marks by host install id and when they were set;
// the newest wins.
type HostMarks struct {
	Marks map[string]HostMark `json:"marks"`
	At    time.Time           `json:"at"`
}

// loadHostMarks reads the stored marks. Call it with e.mu held.
func (e *Engine) loadHostMarks() {
	raw, _ := e.st.GetKV(hostMarksKey)
	if raw == "" {
		return
	}
	var m HostMarks
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		log.Printf("host marks: %v", err)
		return
	}
	e.hostMarks = m
}

// HostMarks are the marks this host keeps (none set: empty).
func (e *Engine) HostMarks() HostMarks {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.hostMarks
	m.Marks = maps.Clone(m.Marks)
	if m.Marks == nil {
		m.Marks = map[string]HostMark{}
	}
	return m
}

// setHostMarks keeps m when it is newer than the marks kept, and tells the
// fronts on the stream; older marks change nothing. It returns what is kept.
func (e *Engine) setHostMarks(m HostMarks) (HostMarks, error) {
	e.mu.Lock()
	if !m.At.After(e.hostMarks.At) {
		cur := e.hostMarks
		e.mu.Unlock()
		return cur, nil
	}
	e.mu.Unlock()
	b, _ := json.Marshal(m)
	if err := e.st.SetKV(hostMarksKey, string(b)); err != nil {
		return HostMarks{}, err
	}
	e.mu.Lock()
	e.hostMarks = m
	e.mu.Unlock()
	if e.Hub != nil {
		e.Hub.Send(stream.FrameHostMarks, m)
	}
	return m, nil
}

// hostMarksOf reads the effect's args ({marks: {id: {icon, color}}, at}).
func hostMarksOf(args map[string]any) HostMarks {
	m := HostMarks{Marks: map[string]HostMark{}}
	raw, _ := args["marks"].(map[string]any)
	for id, v := range raw {
		e, _ := v.(map[string]any)
		icon, _ := e["icon"].(string)
		color, _ := e["color"].(string)
		m.Marks[id] = HostMark{Icon: icon, Color: color}
	}
	m.At, _ = time.Parse(time.RFC3339Nano, args["at"].(string))
	return m
}
