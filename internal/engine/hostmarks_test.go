package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/store"
)

// SPEC V0.171: host.hostMarks is the owner's icon and colour per host, keyed
// by install id; the newest write wins, bad marks are refused, and it
// survives a restart.
func TestHostMarksNewestWins(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(st)
	t0 := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	set := func(id string, marks map[string]any, at time.Time) api.Reply {
		return e.Command(api.Cmd{CommandID: id, Action: "host.hostMarks", Args: map[string]any{"marks": marks, "at": at.Format(time.RFC3339Nano)}})
	}
	if r := set("c1", map[string]any{"F7QK-2M9X": map[string]any{"icon": "🏠", "color": "teal"}}, t0); !r.OK {
		t.Fatalf("set: %+v", r)
	}
	if r := set("c2", map[string]any{"F7QK-2M9X": map[string]any{"icon": "server"}}, t0.Add(-time.Minute)); !r.OK {
		t.Fatalf("an older write is answered, not refused: %+v", r)
	}
	if got := e.HostMarks(); got.Marks["F7QK-2M9X"] != (HostMark{Icon: "🏠", Color: "teal"}) || !got.At.Equal(t0) {
		t.Fatalf("the older write won: %+v", got)
	}
	for name, bad := range map[string]map[string]any{
		"a colour that is not a name": {"A": map[string]any{"color": "#ff0000"}},
		"a long icon":                 {"A": map[string]any{"icon": "this is not an icon at all, it is a sentence"}},
		"not an object":               {"A": "teal"},
		"an empty id":                 {"": map[string]any{"color": "teal"}},
	} {
		if r := set("bad-"+name, bad, t0.Add(time.Hour)); r.OK {
			t.Fatalf("%s is taken", name)
		}
	}
	if r := set("c3", map[string]any{}, time.Time{}); r.OK {
		t.Fatal("marks without a time are taken")
	}
	st.Close()
	st, _ = store.Open(filepath.Join(dir, "db"))
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := e2.HostMarks(); got.Marks["F7QK-2M9X"].Icon != "🏠" {
		t.Fatalf("after a restart: %+v", got)
	}
}
