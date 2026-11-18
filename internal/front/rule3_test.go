package front

// SPEC V0.157, decision 0030 5.1 rule 3 (Q2): a way in that ran three clean
// days in observe enforces itself, tells the devices, and writes an audit
// entry. The clock is the test's.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/push"
)

type capturePush struct {
	mu   sync.Mutex
	sent []push.Payload
}

func (p *capturePush) PublicKey() string                                 { return "" }
func (p *capturePush) Subscribe(string, push.Subscription, string) error { return nil }
func (p *capturePush) Unsubscribe(string) error                          { return nil }
func (p *capturePush) Known(string) bool                                 { return false }
func (p *capturePush) SetHidden(string, push.Hidden) (bool, error)       { return false, nil }
func (p *capturePush) SetPrefs(string, push.Prefs) (bool, error)         { return false, nil }
func (p *capturePush) NotifyOne(string, string, push.Payload) int        { return 0 }
func (p *capturePush) Notify(_ string, pl push.Payload) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, pl)
	return 1
}
func (p *capturePush) bodies() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, s := range p.sent {
		out = append(out, s.Body)
	}
	return out
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestRule3EnforcesThroughTheFrontAndTellsTheDevices(t *testing.T) {
	clk := &fakeClock{t: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
	a, err := auth.Open(&memKV{}, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	a.SetPeerGate(func() (bool, string) { return false, "a host is on the whois link" }, nil) // this test is about the browser way
	a.Mint(auth.HowLocal, "laptop")
	a.Success(auth.WayBrowser, auth.HowLocal)
	pu := &capturePush{}
	f := New(Options{Name: "front", BuildID: "b", Auth: a, Push: pu, AuthTick: 10 * time.Millisecond, MinBackoff: 50 * time.Millisecond,
		Web: fstest.MapFS{"index.html": {Data: []byte("x")}}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)

	ticks := func(n int64) { // wait until rule 3 has looked n more times
		from := f.authTicks.Load()
		eventually(t, "rule 3 to look again", func() bool { return f.authTicks.Load() >= from+n })
	}
	ticks(3) // several looks on a clock that has not moved
	if a.Mode(auth.WayBrowser) != auth.ModeObserve {
		t.Fatal("enforced before three days")
	}
	a.WouldRefuse(auth.WayBrowser)
	clk.add(2 * 24 * time.Hour)
	ticks(3)
	if a.Mode(auth.WayBrowser) != auth.ModeObserve {
		t.Fatal("enforced two days after a would-refuse")
	}
	clk.add(25 * time.Hour)
	eventually(t, "rule 3 to enforce the browser way", func() bool { return a.Mode(auth.WayBrowser) == auth.ModeEnforce })
	eventually(t, "the devices to be told", func() bool { return len(pu.bodies()) == 1 })
	if b := pu.bodies()[0]; !strings.Contains(b, "fyr auth observe") {
		t.Errorf("the notice does not name the way back: %q", b)
	}
	var found bool
	for _, e := range a.Audit() {
		found = found || (e.By == "auto" && strings.Contains(e.Detail, "enforce"))
	}
	if !found {
		t.Errorf("no audit entry: %+v", a.Audit())
	}
}
