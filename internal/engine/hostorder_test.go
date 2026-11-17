package engine

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/store"
)

// SPEC V0.132: host.hostOrder is the owner's; the newest write wins, so a
// front that set an older order cannot undo a newer one; it survives a restart.
func TestHostOrderNewestWins(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(st)
	t0 := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	set := func(id string, names []any, at time.Time) api.Reply {
		return e.Command(api.Cmd{CommandID: id, Action: "host.hostOrder", Args: map[string]any{"names": names, "at": at.Format(time.RFC3339Nano)}})
	}
	if r := set("c1", []any{"laptop", "server"}, t0); !r.OK {
		t.Fatalf("set: %+v", r)
	}
	if r := set("c2", []any{"server", "laptop"}, t0.Add(-time.Minute)); !r.OK {
		t.Fatalf("an older order is answered, not refused: %+v", r)
	}
	if got := e.HostOrder(); !slices.Equal(got.Names, []string{"laptop", "server"}) || !got.At.Equal(t0) {
		t.Fatalf("the older write won: %+v", got)
	}
	if r := set("c3", []any{"x"}, time.Time{}); r.OK {
		t.Fatal("an order without a time is taken")
	}
	st.Close()
	st, _ = store.Open(filepath.Join(dir, "db"))
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := e2.HostOrder(); !slices.Equal(got.Names, []string{"laptop", "server"}) {
		t.Fatalf("after a restart: %+v", got)
	}
}
