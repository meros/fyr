package peer

// SPEC V0.180 (#261): a joiner without a peer port is paired as one that dials out
// only (the mesh is two-way since V0.174); a refusal names the bad field, and the
// joiner says which side refused, with the inviter's status.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/roster"
)

type install struct {
	env  *Env
	addr string // the peer port, "" for none
}

func newInstall(t *testing.T, name string, listen bool) install {
	t.Helper()
	dir := t.TempDir()
	key, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ros, err := roster.Open(dir, key, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{Key: key, Roster: ros, Name: name, Drive: OpenDrive(dir), Invites: &Invites{}, Dial: &peerlink.Dialer{Key: key},
		Logf: t.Logf}
	in := install{env: env}
	if listen {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		env.Port = func() int { return port }
		srv, err := peerlink.Serve(ln, key, env.Handler(http.NotFoundHandler()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { srv.Close() })
		in.addr = ln.Addr().String()
	}
	return in
}

func TestAJoinerWithoutAPeerPortIsPairedAsDialOutOnly(t *testing.T) {
	a := newInstall(t, "a", true)
	b := newInstall(t, "b", false) // no peerListen: Port is nil, so 0
	code, _ := a.env.NewInvite()
	res, err := b.env.Join(context.Background(), code, a.addr)
	if err != nil {
		t.Fatalf("a join with no peer port: %v", err)
	}
	if res.ID != a.env.Key.ID() {
		t.Errorf("joined %s, want %s", res.ID, a.env.Key.ID())
	}
	m, ok := a.env.Roster.Member(b.env.Key.ID())
	if !ok {
		t.Fatal("the inviter did not add the joiner")
	}
	if m.Port != 0 {
		t.Errorf("the joiner's port: %d, want 0 (it dials out only)", m.Port)
	}
	if _, ok := b.env.Roster.Member(a.env.Key.ID()); !ok {
		t.Error("the joiner did not add the inviter")
	}
}

func TestJoinInviteNamesTheBadField(t *testing.T) {
	a := newInstall(t, "a", true)
	stranger, _ := identity.Load(t.TempDir())
	c := (&peerlink.Dialer{Key: stranger}).Client(a.env.Key.ID(), a.addr)
	for _, tc := range []struct{ body, want string }{
		{`not json`, "JSON"},
		{`{"secret":"x","port":70000}`, "port"},
		{`{"secret":"x","port":-1}`, "port"},
		{`{"port":7779}`, "secret"},
	} {
		resp, err := c.Post("https://peer/pair/invite", "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 512)
		n, _ := resp.Body.Read(b)
		resp.Body.Close()
		if resp.StatusCode != 400 || !strings.Contains(string(b[:n]), tc.want) {
			t.Errorf("%s: %d %q, want 400 naming %q", tc.body, resp.StatusCode, b[:n], tc.want)
		}
	}
}

func TestARefusedJoinCarriesTheInvitersStatusAndWords(t *testing.T) {
	a := newInstall(t, "a", true)
	b := newInstall(t, "b", true)
	code, _ := a.env.NewInvite()
	wrong := code[:len(code)-10] + "ABCDEFGH23"
	_, err := b.env.Join(context.Background(), wrong, a.addr)
	var r *JoinRefused
	if !errors.As(err, &r) {
		t.Fatalf("a wrong secret: %v (%T), want a *JoinRefused", err, err)
	}
	if r.Status != http.StatusForbidden {
		t.Errorf("status %d, want the inviter's 403", r.Status)
	}
	if !strings.Contains(err.Error(), "the install that made the invite refused") || !strings.Contains(err.Error(), "invite") {
		t.Errorf("the words: %q", err)
	}
}

// An inviter older than V0.180 refuses port 0 with a bare "bad request": the joiner
// explains it.
func TestAnOlderInviterRefusingPortZeroIsExplained(t *testing.T) {
	a := newInstall(t, "a", false)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv, err := peerlink.Serve(ln, a.env.Key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	b := newInstall(t, "b", false)
	code, _ := a.env.NewInvite()
	_, err = b.env.Join(context.Background(), code, ln.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "no peer port") || !strings.Contains(err.Error(), "peerListen") {
		t.Fatalf("an older inviter's refusal of port 0: %v", err)
	}
}
