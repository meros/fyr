package auth

import (
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) GetKV(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}

func (k *memKV) SetKV(key, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = v
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTest(t *testing.T) (*Store, *memKV, *clock) {
	kv := &memKV{m: map[string]string{}}
	c := &clock{t: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
	s, err := Open(kv, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return s, kv, c
}

// SPEC V0.130 (2): a one-time link works once, within 60 s.
func TestOnceWorksOnceWithinAMinute(t *testing.T) {
	s, _, c := newTest(t)
	tok := s.Once(HowLocal, "this machine")
	cookie, d, err := s.Redeem(tok, "", "Chrome on Linux")
	if err != nil || cookie == "" || d.How != HowLocal {
		t.Fatalf("redeem: %q %+v %v", cookie, d, err)
	}
	if _, _, err := s.Redeem(tok, "", "x"); err == nil {
		t.Fatal("a used link worked again")
	}
	late := s.Once(HowLocal, "x")
	c.t = c.t.Add(61 * time.Second)
	if _, _, err := s.Redeem(late, "", "x"); err == nil {
		t.Fatal("a link older than 60 s worked")
	}
	if _, _, err := s.Redeem("nonsense", "", "x"); err == nil {
		t.Fatal("an unknown link worked")
	}
}

// SPEC V0.130 (2): a browser that already holds a session keeps it; the
// link is used up and no second device appears.
func TestRedeemKeepsAValidSession(t *testing.T) {
	s, _, _ := newTest(t)
	first, _, _ := s.Redeem(s.Once(HowLocal, "m"), "", "Chrome")
	again, d, err := s.Redeem(s.Once(HowLocal, "m"), first, "Chrome")
	if err != nil || again != "" || d == nil {
		t.Fatalf("second redeem: %q %v", again, err)
	}
	if n := len(s.Devices()); n != 1 {
		t.Fatalf("%d devices", n)
	}
}

// SPEC V0.130 (2): the session names its device; only the hash is kept; a
// session unseen for 90 days ends.
func TestLookupStoresOnlyTheHashAndExpires(t *testing.T) {
	s, kv, c := newTest(t)
	cookie, _ := s.Mint(HowTailnet, "Pixel 9 · Chrome")
	if d, ok := s.Lookup(cookie); !ok || d.Name != "Pixel 9 · Chrome" || d.How != HowTailnet {
		t.Fatalf("lookup %+v %v", d, ok)
	}
	for _, v := range kv.m {
		if len(v) > 0 && contains(v, cookie) {
			t.Fatal("the session itself is stored")
		}
	}
	c.t = c.t.Add(89 * 24 * time.Hour)
	if _, ok := s.Lookup(cookie); !ok {
		t.Fatal("ended before 90 days")
	}
	c.t = c.t.Add(90*24*time.Hour + time.Minute)
	if _, ok := s.Lookup(cookie); ok {
		t.Fatal("a session unseen for 90 days still works")
	}
	if _, ok := s.Lookup(""); ok {
		t.Fatal("an empty cookie is a session")
	}
}

// SPEC V0.130 (2): sessions survive a restart.
func TestSessionsSurviveARestart(t *testing.T) {
	s, kv, c := newTest(t)
	cookie, _ := s.Mint(HowLocal, "this machine")
	s2, err := Open(kv, c.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Lookup(cookie); !ok {
		t.Fatal("the session is gone after a restart")
	}
}

// SPEC V0.130 (1): the owner socket takes fyrd's own user and root only.
func TestPeerAllowed(t *testing.T) {
	if !PeerAllowed(1000, 1000) || !PeerAllowed(0, 1000) || PeerAllowed(1001, 1000) {
		t.Fatal("peer rule")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// SPEC V0.130 (1): the owner socket is mode 0600 and takes this user.
func TestOwnerSocketTakesThisUser(t *testing.T) {
	path := SocketPath(t.TempDir(), OwnerSocket)
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", st.Mode().Perm(), err)
	}
	ol := OwnerListener{ln}
	got := make(chan error, 1)
	go func() {
		c, err := ol.Accept()
		if err == nil {
			c.Close()
		}
		got <- err
	}()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := <-got; err != nil {
		t.Fatalf("this user refused: %v", err)
	}
	if _, err := ListenUnix(path); err == nil {
		t.Fatal("a second listener on a live socket")
	}
}
