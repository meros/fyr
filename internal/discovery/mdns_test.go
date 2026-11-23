package discovery

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The message of one install: PTR _fyr._tcp.local → fyr-<short id>, SRV with the peer
// port and a host name, TXT v=1 id=<id>, and an address; and nothing else (no machine
// name, owner, build or tailnet name, decision 0030 3.6).
func TestAnswerCarriesTheIdThePortAndNothingElse(t *testing.T) {
	id := "ABCD-EFGH-IJKL-MNOP"
	msg := buildAnswer(id, 7788, []net.IP{net.ParseIP("192.168.1.20")})
	got, ok := parseAnswer(msg)
	if !ok {
		t.Fatal("the answer does not parse")
	}
	if got.ID != id || got.Port != 7788 || got.Instance != "fyr-"+shortID(id) || len(got.IPs) != 1 || !got.IPs[0].Equal(net.ParseIP("192.168.1.20")) {
		t.Fatalf("parsed %+v", got)
	}
	s := string(msg)
	for _, bad := range []string{"owner", "build", "ts.net", "@"} {
		if strings.Contains(s, bad) {
			t.Errorf("the answer carries %q", bad)
		}
	}
}

func TestAMalformedOrForeignAnswerIsDropped(t *testing.T) {
	for _, m := range [][]byte{nil, {0, 1, 2}, []byte(strings.Repeat("\xff", 200)), buildQuery()} {
		if a, ok := parseAnswer(m); ok {
			t.Errorf("parsed %v as %+v", m, a)
		}
	}
	// an answer whose TXT has no v=1 or no id is not an install's
	bad := buildAnswerWith("fyr-xxxx", "v=2", "id=ABCD-EFGH-IJKL-MNOP", 7777, []net.IP{net.ParseIP("10.0.0.1")})
	if a, ok := parseAnswer(bad); ok {
		t.Errorf("a v=2 answer parsed: %+v", a)
	}
}

func TestAQueryIsRecognised(t *testing.T) {
	if !isQuery(buildQuery()) {
		t.Fatal("our own query is not recognised")
	}
	if isQuery(buildAnswer("ABCD-EFGH-IJKL-MNOP", 1, nil)) {
		t.Fatal("an answer is not a query")
	}
}

// The bus file a test (or a fixed setup) uses as the mDNS: read as it is. A
// missing file is no candidates; a bad one is no candidates and an error
// (V0.review-robust (4)), not a silently empty LAN.
func TestFileSourceReadsTheBus(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bus.json")
	src := File(p)
	if c, err := src.Candidates(context.Background()); err != nil || len(c) != 0 {
		t.Fatalf("no file: %v %v", c, err)
	}
	os.WriteFile(p, []byte(`[{"ID":"X","Addr":"127.0.0.1:1","Source":"mdns"}]`), 0o600)
	c, _ := src.Candidates(context.Background())
	if len(c) != 1 || c[0].ID != "X" || c[0].Source != "mdns" {
		t.Fatalf("%v", c)
	}
	os.WriteFile(p, []byte(`not json`), 0o600)
	if c, err := src.Candidates(context.Background()); len(c) != 0 || err == nil {
		t.Fatalf("a bad file gave %v, %v", c, err)
	}
}

// Real multicast on loopback: skipped where the sandbox has none.
func TestTwoResponondersFindEachOtherOverRealMulticast(t *testing.T) {
	a := &MDNS{ID: "AAAA-AAAA-AAAA-AAAA", Port: 7001, Iface: "lo", Every: 200 * time.Millisecond}
	b := &MDNS{ID: "BBBB-BBBB-BBBB-BBBB", Port: 7002, Iface: "lo", Every: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	if err := b.Start(ctx); err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		cs, _ := a.Candidates(ctx)
		for _, c := range cs {
			if c.ID == b.ID && strings.HasSuffix(c.Addr, ":7002") && c.Source == "mdns" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Skip("multicast on loopback gave no answer in 10 s (a sandbox without it)")
}

// #259 (V0.lan-candidates): real multicast where the test can make its own network. The
// test binary runs itself again under `unshare -rn` (a user and network namespace,
// no root) with a dummy interface that has MULTICAST on and a route for 224.0.0.0/4,
// so two responders find each other over the kernel's multicast, not a fake. Where
// the namespace cannot be made it skips and says why; inside it, no answer fails.
func TestTwoRespondersFindEachOtherInANetworkNamespace(t *testing.T) {
	if os.Getenv("FYR_MDNS_NETNS") == "" {
		if testing.Short() {
			t.Skip("-short: no network namespace")
		}
		for _, bin := range []string{"unshare", "ip"} {
			if _, err := exec.LookPath(bin); err != nil {
				t.Skipf("no %s here, so no network namespace (run it on Linux with util-linux and iproute2)", bin)
			}
		}
		if out, err := exec.Command("unshare", "-rn", "true").CombinedOutput(); err != nil {
			t.Skipf("this user may not make a user and network namespace (unshare -rn: %v %s); see docs/changes/lan-candidates.md for the manual check", err, bytes.TrimSpace(out))
		}
		setup := "ip link set lo up && ip link add fyr0 type dummy && ip link set fyr0 multicast on up && ip addr add 10.77.0.1/24 dev fyr0 && ip route add 224.0.0.0/4 dev fyr0 && exec \"$0\" -test.run '^TestTwoRespondersFindEachOtherInANetworkNamespace$' -test.v"
		cmd := exec.Command("unshare", "-rn", "sh", "-c", setup, os.Args[0])
		cmd.Env = append(os.Environ(), "FYR_MDNS_NETNS=1")
		out, err := cmd.CombinedOutput()
		t.Logf("in the namespace:\n%s", out)
		if err != nil {
			t.Fatalf("real multicast in a network namespace: %v", err)
		}
		if !bytes.Contains(out, []byte("--- PASS: TestTwoRespondersFindEachOtherInANetworkNamespace")) {
			t.Fatal("the run in the namespace did not pass")
		}
		return
	}
	a := &MDNS{ID: "AAAA-AAAA-AAAA-AAAA", Port: 7001, Iface: "fyr0", Every: 200 * time.Millisecond}
	b := &MDNS{ID: "BBBB-BBBB-BBBB-BBBB", Port: 7002, Iface: "fyr0", Every: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("a: no multicast on fyr0: %v", err)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatalf("b: no multicast on fyr0: %v", err)
	}
	found := func(m *MDNS, id, port string) bool {
		cs, _ := m.Candidates(ctx)
		for _, c := range cs {
			if c.ID == id && c.Addr == "10.77.0.1:"+port && c.Source == "mdns" && c.Name == "fyr-"+shortID(id) {
				return true
			}
		}
		return false
	}
	// Each announces at once and again at 0.5 s and 1 s; the deadline only ends a run
	// that would otherwise hang, it is not what the test measures.
	end := time.Now().Add(30 * time.Second)
	for !(found(a, b.ID, "7002") && found(b, a.ID, "7001")) {
		if time.Now().After(end) {
			ca, _ := a.Candidates(ctx)
			cb, _ := b.Candidates(ctx)
			t.Fatalf("no answer over real multicast: a heard %+v, b heard %+v", ca, cb)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Our own answers are not candidates.
func TestOwnAnswerIsNotACandidate(t *testing.T) {
	m := &MDNS{ID: "AAAA-AAAA-AAAA-AAAA", Port: 7001}
	m.hear(buildAnswer("AAAA-AAAA-AAAA-AAAA", 7001, []net.IP{net.ParseIP("10.0.0.2")}), time.Now())
	if cs, _ := m.Candidates(context.Background()); len(cs) != 0 {
		t.Fatalf("%v", cs)
	}
	m.hear(buildAnswer("BBBB-BBBB-BBBB-BBBB", 7002, []net.IP{net.ParseIP("10.0.0.3")}), time.Now())
	cs, _ := m.Candidates(context.Background())
	if len(cs) != 1 || cs[0].Addr != "10.0.0.3:7002" {
		t.Fatalf("%v", cs)
	}
	m.hear(buildAnswer("BBBB-BBBB-BBBB-BBBB", 7002, []net.IP{net.ParseIP("10.0.0.3")}), time.Now().Add(-time.Hour))
	m.expire(time.Now().Add(5 * time.Minute)) // answers live 2 minutes
	if cs, _ := m.Candidates(context.Background()); len(cs) != 0 {
		t.Fatalf("an old answer stayed: %v", cs)
	}
}
