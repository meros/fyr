package front

import (
	"testing"

	"github.com/meros/fyr/internal/machine"
)

// SPEC V0.170: each host says what kind of machine it runs on and its OS; the
// front carries both in hosts[], so the clients draw the host's mark. A host
// the front cannot reach has none until it is seen.
func TestV170HostsCarryTheirMachine(t *testing.T) {
	a := hostWithID(t, "a", "AAAA-AAAA-AAAA-AAAA")
	f := startFront(t, Options{}, HostConfig{Name: "a", URL: a.URL}, HostConfig{Name: "c", URL: "http://127.0.0.1:1"})
	eventually(t, "a online", func() bool { r := hostRow(t, f.URL, "a"); return r != nil && r["online"] == true })
	r := hostRow(t, f.URL, "a")
	if r["machine"] != machine.Kind() || r["os"] != machine.OS() {
		t.Fatalf("host a's machine: %v (want %s on %s)", r, machine.Kind(), machine.OS())
	}
	if c := hostRow(t, f.URL, "c"); c == nil || c["machine"] != nil {
		t.Fatalf("an unreachable host's machine: %v", c)
	}
}
