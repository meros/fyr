package peer

// #259 (V0.lan-candidates): a found install is one row with every address it was
// heard at; one that claims a member's address or name says so; Ignore hides it, is
// kept across a restart, and is undone from the ignored list.

import (
	"context"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/discovery"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/roster"
)

type lanList []discovery.Candidate

func (l *lanList) Candidates(context.Context) ([]discovery.Candidate, error) { return *l, nil }

func lanEnv(t *testing.T, dir string) (*Env, *lanList) {
	t.Helper()
	key, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ros, err := roster.Open(dir, key, "desk", nil)
	if err != nil {
		t.Fatal(err)
	}
	l := &lanList{}
	return &Env{Key: key, Roster: ros, Name: "desk", Drive: OpenDrive(dir), Ignore: OpenIgnored(dir), LAN: l, Logf: t.Logf}, l
}

func lanOnly(cs []Candidate) []Candidate {
	var out []Candidate
	for _, c := range cs {
		if c.Kind == "lan" {
			out = append(out, c)
		}
	}
	return out
}

const (
	idX = "XXXX-2222-3333-4444"
	idY = "YYYY-2222-3333-4444"
)

func TestOneIdAtTwoAddressesIsOneRowMarkedDup(t *testing.T) {
	e, l := lanEnv(t, t.TempDir())
	*l = lanList{
		{ID: idX, Addr: "192.0.2.7:7778", Source: "mdns", Name: "fyr-xxxx2222"},
		{ID: idX, Addr: "192.0.2.9:7778", Source: "mdns", Name: "fyr-xxxx2222"},
		{ID: idY, Addr: "192.0.2.8:7778", Source: "hand"},
	}
	cs := lanOnly(e.Candidates(context.Background()))
	if len(cs) != 2 {
		t.Fatalf("want 2 rows, got %+v", cs)
	}
	x, y := cs[0], cs[1]
	if x.ID != idX || !x.Dup || len(x.Addrs) != 2 || x.Addr != "192.0.2.7:7778" || x.Source != "mdns" || x.Spoof != "" {
		t.Errorf("the install at two addresses: %+v", x)
	}
	if y.ID != idY || y.Dup || y.Source != "hand" || y.Name != "fyr-yyyy2222" {
		t.Errorf("the install added by hand: %+v", y)
	}
}

func TestAFoundInstallClaimingAMembersAddressOrNameIsASpoof(t *testing.T) {
	e, l := lanEnv(t, t.TempDir())
	lab, _ := identity.Load(t.TempDir())
	e.Roster.Add(lab.Public, "lab", "192.0.2.20", 7778)
	*l = lanList{
		{ID: idX, Addr: "192.0.2.20:7778", Source: "mdns"},                   // lab's address, another id
		{ID: idY, Addr: "192.0.2.30:7778", Source: "mdns", Name: "lab"},      // lab's name, another id
		{ID: lab.ID(), Addr: "192.0.2.21:7778", Source: "mdns"},              // lab itself: a member, not listed
		{ID: "ZZZZ-2222-3333-4444", Addr: "192.0.2.20:7779", Source: "mdns"}, // lab's host, another port: a second install there
	}
	cs := lanOnly(e.Candidates(context.Background()))
	if len(cs) != 3 {
		t.Fatalf("want 3 rows, got %+v", cs)
	}
	want := map[string]string{idX: "address:lab:" + lab.ID(), idY: "name:lab:" + lab.ID(), "ZZZZ-2222-3333-4444": ""}
	for _, c := range cs {
		if c.Spoof != want[c.ID] {
			t.Errorf("%s: spoof %q, want %q", c.ID, c.Spoof, want[c.ID])
		}
	}
	// a found install going by a member's short id is one too
	*l = lanList{{ID: idX, Addr: "192.0.2.40:7778", Name: "fyr-" + shortID(lab.ID())}}
	if c := lanOnly(e.Candidates(context.Background())); len(c) != 1 || !strings.HasPrefix(c[0].Spoof, "name:lab:") {
		t.Errorf("a member's short id: %+v", c)
	}
}

func TestIgnoreHidesAFoundInstallAcrossARestartAndIsUndone(t *testing.T) {
	dir := t.TempDir()
	e, l := lanEnv(t, dir)
	*l = lanList{{ID: idX, Addr: "192.0.2.7:7778", Source: "mdns"}, {ID: idY, Addr: "192.0.2.8:7778", Source: "mdns"}}
	ctx := context.Background()
	if err := e.SetIgnored(idX, true); err != nil {
		t.Fatal(err)
	}
	if cs := lanOnly(e.Candidates(ctx)); len(cs) != 1 || cs[0].ID != idY {
		t.Fatalf("after Ignore: %+v", cs)
	}
	if ig := e.IgnoredList(ctx); len(ig) != 1 || ig[0].ID != idX || ig[0].Addr != "192.0.2.7:7778" || ig[0].At.IsZero() {
		t.Fatalf("the ignored list: %+v", ig)
	}

	e2, l2 := lanEnv(t, dir) // a restart reads the list back
	*l2 = *l
	if cs := lanOnly(e2.Candidates(ctx)); len(cs) != 1 || cs[0].ID != idY {
		t.Fatalf("after a restart: %+v", cs)
	}
	if err := e2.SetIgnored(idX, false); err != nil {
		t.Fatal(err)
	}
	if cs := lanOnly(e2.Candidates(ctx)); len(cs) != 2 {
		t.Fatalf("after Show again: %+v", cs)
	}
	if ig := e2.IgnoredList(ctx); len(ig) != 0 {
		t.Fatalf("still ignored: %+v", ig)
	}
}

func TestAMemberOrThisInstallIsNotIgnored(t *testing.T) {
	e, _ := lanEnv(t, t.TempDir())
	lab, _ := identity.Load(t.TempDir())
	e.Roster.Add(lab.Public, "lab", "192.0.2.20", 7778)
	if err := e.SetIgnored(lab.ID(), true); err == nil || !strings.Contains(err.Error(), "remove it") {
		t.Errorf("ignoring a member: %v", err)
	}
	if err := e.SetIgnored(e.Key.ID(), true); err == nil {
		t.Error("ignoring this install worked")
	}
	if err := e.SetIgnored("", true); err == nil {
		t.Error("ignoring no id worked")
	}
}
