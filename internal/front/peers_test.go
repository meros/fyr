package front

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peerlink"
)

// V0.retire-discovery: the old tailnet discovery is gone, so a host comes into a
// front's list from the config or from the roster (UsePeerLink). An install that
// has a peer link to its host keeps following it when the host's old listener
// (the whois port, the loopback hostListen) is gone: the front reaches the host
// over the peer link only, and hosts[].link says so.
func TestAPairedHostIsFollowedOverThePeerLinkWithTheOldListenerGone(t *testing.T) {
	hostKey, frontKey := mustKey(t), mustKey(t)
	b := newHostID(t, "server", hostKey.ID()) // a host reports its key id
	bu, _ := url.Parse(b.hs.URL)
	toHost := httputil.NewSingleHostReverseProxy(bu)
	// The host's peer port, and its old listener beside it.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ps, err := peerlink.Serve(ln, hostKey, toHost)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ps.Close() })
	old := httptest.NewServer(toHost)
	t.Cleanup(old.Close)

	// "server" is configured at its old listener (config hosts:) and is paired;
	// "paired" is a second host the roster alone brings.
	cKey := mustKey(t)
	c := newHostID(t, "paired", cKey.ID())
	cu, _ := url.Parse(c.hs.URL)
	cln, _ := net.Listen("tcp", "127.0.0.1:0")
	cps, err := peerlink.Serve(cln, cKey, httputil.NewSingleHostReverseProxy(cu))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cps.Close() })

	f := New(Options{Name: "front", MinBackoff: 20 * time.Millisecond}, []HostConfig{{Name: "server", URL: old.URL, Remote: true}})
	dial := &peerlink.Dialer{Key: frontKey}
	f.UsePeerLink(hostKey.ID(), "server", PeerEndpoint{ID: hostKey.ID(), Addr: ln.Addr().String(), Dial: dial})
	f.UsePeerLink(cKey.ID(), "paired", PeerEndpoint{ID: cKey.ID(), Addr: cln.Addr().String(), Dial: dial})
	old.Close() // the old listener is gone (enforce, or a build without it)

	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	fs := httptest.NewServer(f.Handler())
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close() })
	online(t, fs.URL, 2)

	_, rep := post(t, b.hs.URL+"/api/command", `{"commandId":"c-peer","action":"thread.create","args":{"cwd":"/tmp","text":"on the server"}}`)
	id, _ := rep["threadId"].(string)
	if id == "" {
		t.Fatalf("create on the host: %v", rep)
	}
	eventually(t, "the host's thread in the state", func() bool { return threadOf(getState(t, fs.URL), id) != nil })
	if code, rep := post(t, fs.URL+"/api/h/"+hostKey.ID()+"/command", `{"commandId":"c-ren-peer","action":"thread.rename","threadId":"`+id+`","args":{"title":"B"}}`); code != 200 {
		t.Fatalf("a command through the front: %d %v", code, rep)
	}
	for _, h := range getState(t, fs.URL).Hosts {
		link, _ := h["link"].(map[string]any)
		if h["online"] != true || link["via"] != "peer" || link["state"] != "up" {
			t.Errorf("host %v: online %v, link %v (want up, via peer)", h["name"], h["online"], link)
		}
	}
	if n := f.WhoisLinkUses(); n != 0 {
		t.Errorf("the whois link was used %d times", n)
	}
	if ok, why := f.AllOnPeerLink(); !ok {
		t.Errorf("not all on the peer link: %s", why)
	}
}

// The front answers /.well-known/fyr, which the pairing reads (condition 1).
func TestFrontAnswersWellKnown(t *testing.T) {
	f := New(Options{Name: "front", WellKnown: func() peer.WellKnown {
		return peer.WellKnown{Fyr: 1, Name: "laptop", Role: "both", Owner: "", ID: "k1", Peer: 7780}
	}}, nil)
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/.well-known/fyr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var w map[string]any
	json.NewDecoder(resp.Body).Decode(&w)
	if w["fyr"] != float64(1) || w["name"] != "laptop" || w["id"] != "k1" || w["peer"] != float64(7780) {
		t.Fatalf("%v", w)
	}
}
