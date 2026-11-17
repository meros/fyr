package peer

// SPEC V0.160: a certificate's rotation chain is merged into the roster only when it
// leads from a key the roster holds to the key on the connection; a stranger's junk
// chain writes nothing.

import (
	"net"
	"net/http"
	"testing"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/roster"
)

func TestAStrangersRotationChainWritesNothing(t *testing.T) {
	dir := t.TempDir()
	key, _ := identity.Load(dir)
	ros, err := roster.Open(dir, key, "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{Key: key, Roster: ros, Name: "a", Drive: OpenDrive(dir)}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv, err := peerlink.Serve(ln, key, env.Handler(http.NotFoundHandler()))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	before := len(ros.Statements())

	stranger, _ := identity.Load(t.TempDir())
	var junk []roster.Statement
	for i := 0; i < 16; i++ {
		junk = append(junk, roster.Statement{V: 1, Kind: roster.KindKeyRotate, By: "FAKE-FAKE-FAKE-FAKE", Subject: "FAKE-FAKE-FAKE-FAKE", Key: stranger.Public, Sig: make([]byte, 64)})
	}
	d := &peerlink.Dialer{Key: stranger, Opts: func() peerlink.Options { return peerlink.Options{Rotation: junk} }}
	resp, err := d.Client(key.ID(), ln.Addr().String()).Get("https://peer/roster")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("a stranger with a junk chain: %d (want 403)", resp.StatusCode)
	}
	if n := len(ros.Statements()); n != before {
		t.Errorf("the stranger's certificate wrote %d statements to the roster", n-before)
	}
}
