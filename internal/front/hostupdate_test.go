package front

import (
	"fmt"
	"testing"

	"github.com/meros/fyr/internal/stream"
)

// V0.host-updates (#154): a host's update state (what it runs, the release
// it wants, how far behind, its last update) comes as the frame "update" and
// shows on that host's row in hosts[], for Settings → Hosts on every device.
func TestHostUpdateShowsOnItsRow(t *testing.T) {
	a, b := newHost(t, "a"), newHost(t, "b")
	f := startFront(t, Options{}, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	eventually(t, "b online", func() bool { r := hostRow(t, f.URL, "b"); return r != nil && r["online"] == true })
	eventually(t, "b's update on its row", func() bool {
		// sent again until the front's stream of b has subscribed
		b.eng.Hub.Send(stream.FrameUpdate, map[string]any{"running": "9b1e04a", "wanted": "c705ef5", "behind": 3, "last": map[string]any{"result": "failed"}})
		u, _ := hostRow(t, f.URL, "b")["update"].(map[string]any)
		return u != nil && fmt.Sprint(u["behind"]) == "3" && u["wanted"] == "c705ef5"
	})
	if u := hostRow(t, f.URL, "a")["update"]; u != nil {
		t.Fatalf("b's update reached a's row: %v", u)
	}
}
