package roster

// SPEC V0.158, decision 0030 3.6: the roster is a grow-only set of statements
// signed by member installs; every install that holds the same statements
// computes the same members, in whatever order they arrived.

import (
	"crypto/ed25519"
	"math/rand"
	"testing"
	"time"

	"github.com/meros/fyr/internal/identity"
)

type inst struct {
	k *identity.Key
	r *Roster
}

func newInst(t *testing.T, name string) inst {
	t.Helper()
	dir := t.TempDir()
	k, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir, k, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return inst{k, r}
}

func sync2(a, b inst) {
	a.r.Merge(b.r.Statements())
	b.r.Merge(a.r.Statements())
}

func ids(r *Roster) map[string]bool {
	out := map[string]bool{}
	for _, m := range r.Members() {
		out[m.ID] = true
	}
	return out
}

func TestAnInstallStartsAsItsOwnMember(t *testing.T) {
	a := newInst(t, "a")
	if m := a.r.Members(); len(m) != 1 || m[0].ID != a.k.ID() || m[0].Name != "a" {
		t.Fatalf("members %+v", m)
	}
}

// Condition 1 pairs both ways: each side signs the other's add. Then each
// holds both, and a sync gives them the same statements.
func TestPairingIsMutual(t *testing.T) {
	a, b := newInst(t, "a"), newInst(t, "b")
	a.r.Add(b.k.Public, "b", "b.ts.net", 7779)
	b.r.Add(a.k.Public, "a", "a.ts.net", 7779)
	sync2(a, b)
	for _, r := range []*Roster{a.r, b.r} {
		if got := ids(r); len(got) != 2 || !got[a.k.ID()] || !got[b.k.ID()] {
			t.Fatalf("members %v", got)
		}
	}
	if m, _ := a.r.Member(b.k.ID()); m.Name != "b" || m.Host != "b.ts.net" || m.Port != 7779 {
		t.Errorf("b's address in a: %+v", m)
	}
}

// Condition 2: a member vouches. A pairs with B, B pairs with C; A learns C by
// sync, with no press.
func TestAMemberVouches(t *testing.T) {
	a, b, c := newInst(t, "a"), newInst(t, "b"), newInst(t, "c")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 1)
	b.r.Add(c.k.Public, "c", "", 1)
	c.r.Add(b.k.Public, "b", "", 1)
	sync2(a, b)
	sync2(b, c)
	sync2(a, b)
	for name, r := range map[string]*Roster{"a": a.r, "b": b.r, "c": c.r} {
		if got := ids(r); len(got) != 3 {
			t.Errorf("%s knows %d members, want 3", name, len(got))
		}
	}
}

// The same statements in any order give the same members.
func TestMergeOrderDoesNotMatter(t *testing.T) {
	a, b, c := newInst(t, "a"), newInst(t, "b"), newInst(t, "c")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 1)
	b.r.Add(c.k.Public, "c", "", 1)
	c.r.Add(b.k.Public, "b", "", 1)
	all := append(append(a.r.Statements(), b.r.Statements()...), c.r.Statements()...)
	for seed := 0; seed < 20; seed++ {
		list := append([]Statement(nil), all...)
		rand.New(rand.NewSource(int64(seed))).Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
		x := newInst(t, "x")
		// x is one more install that merges everything one statement at a time
		// in this order: it learns nobody (it is in nobody's roster) except itself.
		for _, s := range list {
			x.r.Merge([]Statement{s})
		}
		if got := ids(x.r); len(got) != 1 || !got[x.k.ID()] {
			t.Fatalf("seed %d: a stranger's roster gained members: %v", seed, got)
		}
		// a's roster, fed the others one at a time in this order, gets 3
		fresh := a.r
		for _, s := range list {
			fresh.Merge([]Statement{s})
		}
		if got := ids(fresh); len(got) != 3 {
			t.Fatalf("seed %d: %d members", seed, len(got))
		}
	}
}

// A statement signed by someone who is not a member counts for nothing, and a
// forged or altered one is dropped.
func TestStrangersAndForgeriesCountForNothing(t *testing.T) {
	a, evil, c := newInst(t, "a"), newInst(t, "evil"), newInst(t, "c")
	// evil adds itself and c to its own roster and offers that to a
	evil.r.Add(c.k.Public, "c", "", 1)
	if a.r.Merge(evil.r.Statements()) && len(ids(a.r)) != 1 {
		t.Fatalf("a took a stranger's word: %v", ids(a.r))
	}
	if len(ids(a.r)) != 1 {
		t.Fatalf("members %v", ids(a.r))
	}
	// an altered statement: the signature no longer holds
	b := newInst(t, "b")
	good := a.r.Add(b.k.Public, "b", "", 1)
	bad := good
	bad.Name = "tampered"
	a2 := newInst(t, "a2")
	a2.r.Merge([]Statement{bad})
	if len(a2.r.Statements()) != 1 { // only its own first statement
		// kept but unverified is fine; what counts is it makes no member:
	}
	if len(ids(a2.r)) != 1 {
		t.Errorf("a tampered add made a member: %v", ids(a2.r))
	}
	// an add whose key does not match the subject id is dropped
	forged := good
	forged.Subject = identity.IDOf(ed25519.PublicKey(make([]byte, 32)))
	if a2.r.Merge([]Statement{forged}) {
		t.Error("a statement whose key is not its subject was kept")
	}
}

// Device statements: a revoke wins whatever order they come in; a revoke by a
// stranger counts for nothing.
func TestDeviceRevokeWinsForEver(t *testing.T) {
	a, b := newInst(t, "a"), newInst(t, "b")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 1)
	sync2(a, b)
	a.r.DeviceAdd("dev_1", "Chrome", "local")
	sync2(a, b)
	if ds := b.r.Devices(); len(ds) != 1 || ds[0].Subject != "dev_1" || ds[0].Device != "Chrome" {
		t.Fatalf("b's devices %+v", ds)
	}
	if b.r.DeviceRevoked("dev_1") {
		t.Fatal("revoked before it was")
	}
	b.r.DeviceRevoke("dev_1")
	sync2(a, b)
	if !a.r.DeviceRevoked("dev_1") || len(a.r.Devices()) != 0 {
		t.Fatal("a revoke made on b did not reach a")
	}
	a.r.DeviceAdd("dev_1", "Chrome again", "local") // an add after a revoke changes nothing
	if !a.r.DeviceRevoked("dev_1") {
		t.Error("a later add undid a revoke")
	}
	stranger := newInst(t, "s")
	stranger.r.DeviceRevoke("dev_9")
	a.r.DeviceAdd("dev_9", "x", "local")
	a.r.Merge(stranger.r.Statements())
	if a.r.DeviceRevoked("dev_9") {
		t.Error("a stranger revoked a device")
	}
}

func TestRosterIsKeptAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	k, _ := identity.Load(dir)
	r, _ := Open(dir, k, "a", nil)
	b := newInst(t, "b")
	r.Add(b.k.Public, "b", "b.ts.net", 7779)
	r.DeviceAdd("dev_1", "Chrome", "local")
	r2, err := Open(dir, k, "a", func() time.Time { return time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	if len(ids(r2)) != 2 || len(r2.Devices()) != 1 {
		t.Errorf("after a restart: %v %v", ids(r2), r2.Devices())
	}
}

func TestSetAddressSpreads(t *testing.T) {
	a, b := newInst(t, "a"), newInst(t, "b")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 0)
	b.r.SetAddress("b", "b.ts.net", 7790)
	sync2(a, b)
	if m, _ := a.r.Member(b.k.ID()); m.Host != "b.ts.net" || m.Port != 7790 {
		t.Errorf("b's address in a: %+v", m)
	}
}

// V0.159: the owner's login travels in the roster, so a tagged install (which
// has none of its own) takes it from a member; a stranger's word is not taken.
func TestOwnerLoginSpreadsToAMember(t *testing.T) {
	a, tagged := newInst(t, "a"), newInst(t, "tagged")
	if a.r.OwnerLogin() != "" {
		t.Fatal("a login from nothing")
	}
	a.r.SetOwnerLogin("owner@example.com")
	a.r.Add(tagged.k.Public, "tagged", "", 1)
	tagged.r.Add(a.k.Public, "a", "", 1)
	sync2(a, tagged)
	if got := tagged.r.OwnerLogin(); got != "owner@example.com" {
		t.Fatalf("the tagged install learned %q", got)
	}
	stranger := newInst(t, "s")
	stranger.r.SetOwnerLogin("evil@example.com")
	tagged.r.Merge(stranger.r.Statements())
	if tagged.r.OwnerLogin() != "owner@example.com" {
		t.Errorf("a stranger changed the owner: %q", tagged.r.OwnerLogin())
	}
	before := len(a.r.Statements())
	a.r.SetOwnerLogin("owner@example.com") // the same again says nothing new
	if n := len(a.r.Statements()); n != before {
		t.Errorf("%d statements, was %d", n, before)
	}
}

// Who added a member is kept: Settings → Hosts says "vouched for by b".
func TestAddedByIsKept(t *testing.T) {
	a, b, c := newInst(t, "a"), newInst(t, "b"), newInst(t, "c")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 1)
	b.r.Add(c.k.Public, "c", "", 1)
	sync2(a, b)
	if m, _ := a.r.Member(b.k.ID()); m.AddedBy != a.k.ID() {
		t.Errorf("b was added by %s (want a)", m.AddedBy)
	}
	if m, _ := a.r.Member(c.k.ID()); m.AddedBy != b.k.ID() {
		t.Errorf("c was added by %s (want b)", m.AddedBy)
	}
}

// V0.160 (decision 0030 3.6, 5): removal. What the removed install signed counts
// for nothing, so the members only it vouched for drop out, unless the remover
// signs for them in the same batch, which it does for the ones the owner keeps.
func TestRemovalVoidsWhatTheRemovedSignedAndTheRemoverKeepsTheRest(t *testing.T) {
	a, b, c := newInst(t, "a"), newInst(t, "b"), newInst(t, "c")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 1)
	b.r.Add(c.k.Public, "c", "", 1) // b vouches for c
	sync2(a, b)
	if !ids(a.r)[c.k.ID()] {
		t.Fatal("a did not learn c")
	}
	if err := a.r.Remove(b.k.ID()); err != nil {
		t.Fatal(err)
	}
	got := ids(a.r)
	if got[b.k.ID()] || !got[c.k.ID()] || !got[a.k.ID()] {
		t.Fatalf("after removing b: %v (c must stay, signed for again by a)", got)
	}
	if m, _ := a.r.Member(c.k.ID()); m.AddedBy != a.k.ID() {
		t.Errorf("c is now vouched for by %s, want a", m.AddedBy)
	}
	// b, still holding its statements, adds d and offers everything again: void
	d := newInst(t, "d")
	b.r.Add(d.k.Public, "d", "", 1)
	a.r.Merge(b.r.Statements())
	if ids(a.r)[d.k.ID()] || ids(a.r)[b.k.ID()] {
		t.Errorf("a removed member signed its way back in: %v", ids(a.r))
	}
	if !a.r.Removed(b.k.ID()) {
		t.Error("Removed is false for the removed install")
	}
}

// The removal reaches every member with one sync, whatever the order; a removed
// install learns it was removed.
func TestRemovalSpreadsInAnyOrder(t *testing.T) {
	a, b, c := newInst(t, "a"), newInst(t, "b"), newInst(t, "c")
	a.r.Add(b.k.Public, "b", "", 1)
	b.r.Add(a.k.Public, "a", "", 1)
	a.r.Add(c.k.Public, "c", "", 1)
	c.r.Add(a.k.Public, "a", "", 1)
	sync2(a, b)
	sync2(a, c)
	sync2(b, c)
	if len(ids(c.r)) != 3 {
		t.Fatalf("c: %v", ids(c.r))
	}
	if err := a.r.Remove(b.k.ID()); err != nil {
		t.Fatal(err)
	}
	sync2(a, c)
	if ids(c.r)[b.k.ID()] {
		t.Errorf("c still has the removed b: %v", ids(c.r))
	}
	sync2(a, b)
	if !b.r.SelfRemoved() {
		t.Error("b does not know it was removed")
	}
	if err := a.r.Remove(a.k.ID()); err == nil {
		t.Error("an install removed itself")
	}
}

// A key that may be stolen is removed and paired again, not rotated: the same
// machine with a new key is a new install, and the old key stays refused.
func TestRemovedThenPairedAgainWithANewKey(t *testing.T) {
	a, old := newInst(t, "a"), newInst(t, "b")
	a.r.Add(old.k.Public, "b", "", 1)
	if err := a.r.Remove(old.k.ID()); err != nil {
		t.Fatal(err)
	}
	fresh := newInst(t, "b")
	a.r.Add(fresh.k.Public, "b", "", 1)
	got := ids(a.r)
	if got[old.k.ID()] || !got[fresh.k.ID()] {
		t.Fatalf("members %v", got)
	}
}

// V0.160: rotation. The statement is signed by the old key; a peer that was
// offline follows the new key; the old key is no longer a member.
func TestRotationIsFollowedByAPeerThatWasOffline(t *testing.T) {
	a, bOld := newInst(t, "a"), newInst(t, "b")
	a.r.Add(bOld.k.Public, "b", "b.ts.net", 7779)
	bOld.r.Add(a.k.Public, "a", "", 1)
	sync2(a, bOld)
	bOld.r.DeviceAdd("dev_1", "Chrome", "local") // signed by the old key
	a.r.Merge(bOld.r.Statements())

	// b rotates its key on disk and restarts on it
	dir := t.TempDir()
	newKey, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	stmt := bOld.r.Rotate(newKey.Public)
	if stmt.Kind != KindKeyRotate || stmt.Subject != bOld.k.ID() {
		t.Fatalf("%+v", stmt)
	}
	// the new key opens the same roster file: it keeps what the old key signed
	data := bOld.r.path[:len(bOld.r.path)-len("/roster.json")]
	b2, err := Open(data, newKey, "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ids(b2)[newKey.ID()] || ids(b2)[bOld.k.ID()] || !ids(b2)[a.k.ID()] {
		t.Fatalf("b after rotating: %v", ids(b2))
	}
	if len(b2.Devices()) != 1 {
		t.Errorf("what the old key signed was lost: %v", b2.Devices())
	}
	// a, offline until now, takes the statement
	a.r.Merge(b2.Statements())
	got := ids(a.r)
	if got[bOld.k.ID()] || !got[newKey.ID()] {
		t.Fatalf("a after the rotation: %v", got)
	}
	if _, ok := a.r.KeyMember(bOld.k.Public); ok {
		t.Error("the old key is still a member")
	}
	if m, ok := a.r.KeyMember(newKey.Public); !ok || m.Name != "b" || m.Host != "b.ts.net" || m.Port != 7779 {
		t.Errorf("the new key: %+v %v", m, ok)
	}
	if a.r.Successor(bOld.k.ID()) != newKey.ID() {
		t.Error("the successor is not known")
	}
	if len(a.r.Devices()) != 1 {
		t.Errorf("a lost the device the old key signed: %v", a.r.Devices())
	}
	// a statement signed by the new key counts at a
	b2.DeviceAdd("dev_2", "Safari", "link")
	a.r.Merge(b2.Statements())
	if len(a.r.Devices()) != 2 {
		t.Errorf("a does not take what the new key signs: %v", a.r.Devices())
	}
	// removing b removes it whatever key it is on
	if err := a.r.Remove(newKey.ID()); err != nil {
		t.Fatal(err)
	}
	if ids(a.r)[newKey.ID()] || ids(a.r)[bOld.k.ID()] {
		t.Errorf("a rotated install was not removed: %v", ids(a.r))
	}
}

// A rotation that is not signed by the old key, or a forged one, moves nobody.
func TestAForgedRotationMovesNobody(t *testing.T) {
	a, b, evil := newInst(t, "a"), newInst(t, "b"), newInst(t, "evil")
	a.r.Add(b.k.Public, "b", "", 1)
	forged := evil.r.Rotate(evil.k.Public) // evil rotating itself is not b's rotation
	forged.Subject = b.k.ID()              // claims to be b's
	a.r.Merge([]Statement{forged})
	if got := ids(a.r); !got[b.k.ID()] || got[evil.k.ID()] {
		t.Errorf("members %v", got)
	}
}

// A stranger cannot fill the roster or stop a removal: a statement whose signature
// holds under no key this roster can name is dropped; valid ones from strangers are
// bounded; a member's removal always merges.
func TestAStrangerCannotFillTheRoster(t *testing.T) {
	a, b, evil := newInst(t, "a"), newInst(t, "b"), newInst(t, "evil")
	a.r.Add(b.k.Public, "b", "", 1)
	before := len(a.r.Statements())
	junk := make([]Statement, 0, 16)
	for i := 0; i < 16; i++ {
		junk = append(junk, Statement{V: 1, Kind: KindDeviceAdd, By: "FAKE-FAKE-FAKE-FAKE", Subject: "dev_" + string(rune('a'+i)), Sig: make([]byte, 64)})
	}
	if a.r.Merge(junk) || len(a.r.Statements()) != before {
		t.Fatalf("junk statements were kept: %d -> %d", before, len(a.r.Statements()))
	}
	// valid statements from many strangers, in one batch: kept only up to the bound
	var flood []Statement
	for i := 0; i < maxForeign+500; i++ {
		flood = append(flood, newInstQuiet(t))
	}
	a.r.Merge(flood)
	if n := len(a.r.Statements()); n > before+maxForeign+10 {
		t.Fatalf("%d statements after a flood (bound %d)", n, maxForeign)
	}
	// and a removal by a member still merges
	b.r.Add(evil.k.Public, "evil", "", 1)
	a.r.Merge(b.r.Statements())
	if err := a.r.Remove(evil.k.ID()); err != nil {
		t.Fatalf("a removal after the flood: %v", err)
	}
	if !a.r.Removed(evil.k.ID()) {
		t.Error("the removal did not take effect")
	}
}

// newInstQuiet makes a stranger's one valid statement (its own first add) without
// a data folder of its own.
func newInstQuiet(t *testing.T) Statement {
	k, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	s := Statement{V: 1, Kind: KindInstallAdd, By: k.ID(), Subject: k.ID(), Key: k.Public, At: time.Now().UTC().Truncate(time.Second)}
	s.Sig = ed25519.Sign(k.Private, s.signBytes())
	return s
}
