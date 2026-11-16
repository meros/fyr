package peers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fyr is a fake Fyr page origin that answers /.well-known/fyr.
func fyr(t *testing.T, name, owner string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(Handler(func() WellKnown { return WellKnown{Fyr: 1, Name: name, Role: "both", Build: "b1", Owner: owner} }))
	t.Cleanup(s.Close)
	return s
}

func byOrigin(ps []Peer, origin string) *Peer {
	for i := range ps {
		if ps[i].Origin == origin {
			return &ps[i]
		}
	}
	return nil
}

// SPEC 0.5 V0.123 (1): a static peer that answers with the owner's login is
// a Fyr host; one that names another owner, or is no Fyr, is not.
func TestRoundKeepsOnlyTheOwnersFyr(t *testing.T) {
	mine := fyr(t, "server", "me@x")
	other := fyr(t, "theirs", "you@x")
	nofyr := httptest.NewServer(http.NotFoundHandler())
	defer nofyr.Close()
	d := New(Options{Owner: "me@x", Static: []string{mine.URL, other.URL, nofyr.URL}})
	d.Round(context.Background())
	ps := d.List()
	if len(ps) != 1 {
		t.Fatalf("peers %+v", ps)
	}
	p := ps[0]
	if p.Origin != mine.URL || p.Name != "server" || !p.Online || p.Role != "both" || p.Build != "b1" || p.LastSeen.IsZero() {
		t.Fatalf("peer %+v", p)
	}
}

// SPEC 0.5 V0.123 (1): a tailnet candidate is probed at
// https://<MagicDNS name>:<port>/.well-known/fyr; its answer names where it
// serves the host protocol.
func TestRoundProbesTailnetCandidates(t *testing.T) {
	ts := httptest.NewTLSServer(Handler(func() WellKnown {
		return WellKnown{Fyr: 1, Name: "server", Role: "both", Owner: "me@x", Host: "http://100.64.0.2:7778"}
	}))
	defer ts.Close()
	var asked atomic.Value
	client := ts.Client()
	tr := client.Transport.(*http.Transport)
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		asked.Store(addr)
		return (&net.Dialer{}).DialContext(ctx, network, ts.Listener.Addr().String())
	}
	tr.TLSClientConfig.ServerName = "example.com" // the test certificate's name
	d := New(Options{Owner: "me@x", Ports: []int{7443}, Client: client,
		Nodes: func(context.Context) ([]string, error) { return []string{"server.tail.ts.net"}, nil }})
	d.Round(context.Background())
	if a, _ := asked.Load().(string); a != "server.tail.ts.net:7443" {
		t.Fatalf("probed %q", a)
	}
	ps := d.List()
	if len(ps) != 1 || ps[0].Origin != "https://server.tail.ts.net:7443" || ps[0].Name != "server" || !ps[0].Online || ps[0].Host != "http://100.64.0.2:7778" {
		t.Fatalf("peers %+v", ps)
	}
}

// SPEC 0.5 V0.123 (1): a host that stops answering stays, offline, with its
// lastSeen; after 30 days without an answer it is forgotten. OnChange runs
// when the list changes, not on an unchanged round.
func TestHostsComeAndGo(t *testing.T) {
	var down atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "gone", 502)
			return
		}
		Handler(func() WellKnown { return WellKnown{Fyr: 1, Name: "server", Owner: "me@x"} }).ServeHTTP(w, r)
	}))
	defer s.Close()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	var changes atomic.Int32
	d := New(Options{Owner: "me@x", Static: []string{s.URL}, Now: func() time.Time { return now },
		OnChange: func([]Peer) { changes.Add(1) }})
	d.Round(context.Background())
	d.Round(context.Background())
	if changes.Load() != 1 {
		t.Fatalf("OnChange ran %d times for one change", changes.Load())
	}
	seen := now
	down.Store(true)
	now = now.Add(time.Hour)
	d.Round(context.Background())
	p := byOrigin(d.List(), s.URL)
	if p == nil || p.Online || !p.LastSeen.Equal(seen) || p.Since.IsZero() {
		t.Fatalf("an unanswering host: %+v", p)
	}
	if changes.Load() != 2 {
		t.Fatalf("going offline is a change: %d", changes.Load())
	}
	now = seen.Add(31 * 24 * time.Hour)
	d.Round(context.Background())
	if len(d.List()) != 0 {
		t.Fatalf("a host gone 30 days stays: %+v", d.List())
	}
	down.Store(false)
	d.Round(context.Background())
	if p := byOrigin(d.List(), s.URL); p == nil || !p.Online {
		t.Fatal("a host that answers again is back")
	}
}

// SPEC 0.5 V0.123 (1): this install's own origin is not its own peer.
func TestSelfIsNotAPeer(t *testing.T) {
	s := fyr(t, "me", "me@x")
	d := New(Options{Owner: "me@x", Self: s.URL, Static: []string{s.URL}})
	d.Round(context.Background())
	if len(d.List()) != 0 {
		t.Fatalf("self listed: %+v", d.List())
	}
}

// SPEC 0.5 V0.123 (1): /.well-known/fyr answers {fyr, name, role, build, owner}.
func TestWellKnownAnswer(t *testing.T) {
	s := fyr(t, "box", "me@x")
	resp, err := http.Get(s.URL + "/.well-known/fyr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var w map[string]any
	json.NewDecoder(resp.Body).Decode(&w)
	if w["fyr"] != float64(1) || w["name"] != "box" || w["owner"] != "me@x" || w["role"] != "both" || w["build"] != "b1" {
		t.Fatalf("%v", w)
	}
}
