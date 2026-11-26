package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// V0.quiet-lift: project.update {quietLift} lifts the quiet of a project in
// its window: the thread and the project carry no quietUntil until the lift
// ends, the change is sent at once, the tick sends the quiet back when the
// lift ends, and quiet started by hand ends a lift.
func TestQuietLift(t *testing.T) {
	h := newHarness(t, time.Minute)
	c := h.e.Command(api.Cmd{CommandID: "p", Action: "project.create", Args: core.Args{"name": "Work", "folder": h.dir}})
	pid := c.Result.(map[string]any)["id"].(string)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	h.m.StopAll()

	at := func(hh, mm int) time.Time { return time.Date(2026, 11, 13, hh, mm, 0, 0, time.Local) }
	h.e.Now = func() time.Time { return at(18, 0) }
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": map[string]any{"from": "17:00", "to": "06:00"}}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if d := dtoOf(h.e, id); d.QuietUntil == nil {
		t.Fatal("not quiet in the window")
	}
	ch := h.e.Hub.Subscribe()
	defer h.e.Hub.Unsubscribe(ch)

	if r := h.cmd("project.update", "", core.Args{"id": pid, "quietLift": at(18, 0).Add(25 * time.Hour).Format(time.RFC3339)}); r.OK || r.Status != 400 {
		t.Fatalf("a lift past a day accepted: %+v", r)
	}
	end := at(18, 30)
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quietLift": end.Format(time.RFC3339)}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if p := projectOf(h.e, id); p.QuietLift == nil || !p.QuietLift.Equal(end) {
		t.Fatalf("lift not stored: %v", p.QuietLift)
	}
	if d := dtoOf(h.e, id); d.QuietUntil != nil {
		t.Fatalf("lifted, yet quiet until %v", d.QuietUntil)
	}
	if fs := drain(ch, "thread"); len(fs) != 1 || fs[0]["quietUntil"] != nil {
		t.Fatalf("the lift sent %v", fs)
	}
	for _, pd := range h.e.Projects() {
		if pd.ID == pid && pd.QuietUntil != nil {
			t.Fatalf("project quiet while lifted: %v", pd.QuietUntil)
		}
	}

	// The lift ends: the tick sends the quiet back.
	h.e.Now = func() time.Time { return end }
	h.e.Tick()
	if fs := drain(ch, "thread"); len(fs) != 1 || fs[0]["quietUntil"] == nil {
		t.Fatalf("the lift's end sent %v", fs)
	}

	// Lifted again, then quiet by hand: the lift ends.
	h.e.Now = func() time.Time { return at(19, 0) }
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quietLift": at(19, 30).Format(time.RFC3339)}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quietNow": at(23, 0).Format(time.RFC3339)}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if p := projectOf(h.e, id); p.QuietLift != nil {
		t.Fatalf("quiet by hand kept the lift: %v", p.QuietLift)
	}
	if d := dtoOf(h.e, id); d.QuietUntil == nil {
		t.Fatal("quiet by hand while lifted is not quiet")
	}
}
