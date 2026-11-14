package tailnet

import (
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

// SPEC 0.5 V0.14: the owner is the Tailscale user of this machine.
func TestOwnerLoginFromLocalAPI(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ts.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/status" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"Self":{"UserID":42,"DNSName":"box.tail.ts.net."},"User":{"42":{"LoginName":"me@example.com"}}}`))
	}))
	login, dns, err := Self(sock)
	if err != nil || login != "me@example.com" || dns != "box.tail.ts.net" {
		t.Fatalf("%q %q %v", login, dns, err)
	}
	if _, _, err := Self(filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Fatal("missing socket gave no error")
	}
}
