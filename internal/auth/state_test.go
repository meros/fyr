package auth

// SPEC V0.157, decision 0030 slice 2, on a fake clock.

import (
	"strings"
	"testing"
	"time"
)

// Part 5.1 rule 2: a way in has a stored mode that no release changes. A
// store that has none is in observe; a stored one is read back as it is.
func TestModesAreStored(t *testing.T) {
	s, kv, c := newTest(t)
	if s.Mode(WayBrowser) != ModeObserve || s.Mode(WayPeer) != ModeObserve {
		t.Fatal("a fresh install observes")
	}
	if err := s.SetMode(WayBrowser, ModeEnforce, "owner.sock"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(kv, c.now) // "the next release"
	if err != nil || s2.Mode(WayBrowser) != ModeEnforce || s2.Mode(WayPeer) != ModeObserve {
		t.Fatalf("after a restart: %v %v", err, s2.Modes())
	}
	if a := s2.Audit(); len(a) != 1 || a[0].By != "owner.sock" {
		t.Errorf("audit %+v", a)
	}
}

// Slice 5 builds the peer link; until then peer cannot enforce.
func TestPeerCannotEnforceYet(t *testing.T) {
	s, _, _ := newTest(t)
	if err := s.SetMode(WayPeer, ModeEnforce, "owner.sock"); err == nil {
		t.Fatal("peer enforced")
	}
	if err := s.SetMode("nope", ModeEnforce, "owner.sock"); err == nil {
		t.Fatal("an unknown way in")
	}
}

// Rule 3 (Q2): three clean days with a success of each way in use, and the
// install moves the way in to enforce by itself.
func TestRule3EnforcesAfterThreeCleanDays(t *testing.T) {
	s, _, c := newTest(t)
	s.Mint(HowLocal, "laptop")
	s.Mint(HowTailnet, "phone")
	s.Success(WayBrowser, HowLocal)
	s.Success(WayBrowser, HowTailnet)
	c.t = c.t.Add(ReadyAfter - time.Hour)
	if moved := s.Tick(); len(moved) != 0 || s.Mode(WayBrowser) != ModeObserve {
		t.Fatalf("enforced early: %v", moved)
	}
	c.t = c.t.Add(2 * time.Hour)
	moved := s.Tick()
	if len(moved) != 1 || s.Mode(WayBrowser) != ModeEnforce {
		t.Fatalf("not enforced after 3 clean days: %v %v", moved, s.Modes())
	}
	a := s.Audit()
	if last := a[len(a)-1]; last.By != "auto" || !strings.Contains(last.Detail, "fyr auth observe") {
		t.Errorf("the audit entry %+v", last)
	}
	if s.Tick() != nil {
		t.Error("it moved again")
	}
}

// ... and not after a would-refuse: the clean days run from the last one.
func TestRule3WaitsAfterAWouldRefuse(t *testing.T) {
	s, _, c := newTest(t)
	s.Mint(HowLocal, "laptop")
	s.Success(WayBrowser, HowLocal)
	c.t = c.t.Add(2 * 24 * time.Hour)
	s.WouldRefuse(WayBrowser) // fyr-app with no cookie, say
	c.t = c.t.Add(2 * 24 * time.Hour)
	if len(s.Tick()) != 0 {
		t.Fatal("enforced 2 days after a would-refuse")
	}
	if ok, why := s.Ready(WayBrowser); ok || !strings.Contains(why, "clean days") {
		t.Errorf("ready %v %q", ok, why)
	}
	c.t = c.t.Add(25 * time.Hour)
	if len(s.Tick()) != 1 {
		t.Fatal("not enforced 3 days after the last would-refuse")
	}
}

// ... and not while a way in in use has had no success.
func TestRule3NeedsASuccessOfEachWayInUse(t *testing.T) {
	s, _, c := newTest(t)
	s.Mint(HowLocal, "laptop")
	s.Mint(HowTailnet, "phone")
	s.Success(WayBrowser, HowLocal)
	c.t = c.t.Add(10 * 24 * time.Hour)
	if len(s.Tick()) != 0 {
		t.Fatal("enforced with the phone never seen")
	}
	if _, why := s.Ready(WayBrowser); !strings.Contains(why, "tailnet") {
		t.Errorf("why %q", why)
	}
	s.Success(WayBrowser, HowTailnet)
	if len(s.Tick()) != 1 {
		t.Fatal("not enforced once the phone was seen")
	}
}

// A mode set back by hand stays: observe by the owner is not undone by rule 3
// however long it runs, until the owner enforces again.
func TestAModeSetBackByHandStays(t *testing.T) {
	s, _, c := newTest(t)
	s.SetMode(WayBrowser, ModeEnforce, "owner.sock")
	s.SetMode(WayBrowser, ModeObserve, "owner.sock")
	c.t = c.t.Add(30 * 24 * time.Hour)
	if len(s.Tick()) != 0 || s.Mode(WayBrowser) != ModeObserve {
		t.Fatal("rule 3 undid a hand-set observe")
	}
	if m := s.Modes()[0]; !m.Hand {
		t.Error("the hand flag is not shown")
	}
	s.SetMode(WayBrowser, ModeEnforce, "device:dev_x")
	s.SetMode(WayBrowser, ModeObserve, "auto") // only the install's own rule says auto
	c.t = c.t.Add(5 * 24 * time.Hour)
	if len(s.Tick()) != 1 {
		t.Error("an observe that was not set by hand did not enforce")
	}
}

// Q4 and 3.4: the first node of an install with no device is let in at once;
// the second waits in enforce. Observe serves a new node the old way and
// approves it; a blocked node gets nothing.
func TestEnrolTailnet(t *testing.T) {
	s, _, _ := newTest(t)
	sess, d, res := s.EnrolTailnet("nAAA", "a.ts.net", "Chrome", true)
	if res != EnrolSession || sess == "" || d.Node != "nAAA" || s.NodeState("nAAA") != "approved" {
		t.Fatalf("the first node of an empty install in enforce: %v %+v", res, d)
	}
	if _, d2, res := s.EnrolTailnet("nBBB", "b.ts.net", "Safari", true); res != EnrolPending || !d2.Pending || len(d2.Code) != 8 {
		t.Fatalf("the second node in enforce: %v %+v", res, d2)
	}
	if s.NodeState("nBBB") != "" {
		t.Error("a pending node was approved")
	}
	if _, _, res := s.EnrolTailnet("nAAA", "a.ts.net", "Chrome", true); res != EnrolSession {
		t.Error("an approved node is not let in silently")
	}
	if _, _, res := s.EnrolTailnet("nCCC", "c.ts.net", "Firefox", false); res != EnrolSession || s.NodeState("nCCC") != "approved" {
		t.Errorf("observe serves a new node the old way and approves it: %v", res)
	}
	s.Revoke(d.ID, "owner.sock")
	if _, _, res := s.EnrolTailnet("nAAA", "a.ts.net", "Chrome", false); res != EnrolRefused {
		t.Errorf("a revoked device's node enrolled again: %v", res)
	}
}

// Approval codes: used once, ended by five wrong tries, ended by time, and
// rate limited. A replayed code works no more.
func TestApprovalCodes(t *testing.T) {
	s, _, c := newTest(t)
	_, signed := s.Mint(HowLocal, "laptop")
	c.t = c.t.Add(time.Minute)
	_, pend, err := s.Pending(HowCode, "Safari on iPhone", "")
	if err != nil || len(pend.Code) != 8 || strings.ContainsAny(pend.Code, "01OI") {
		t.Fatalf("code %+v %v", pend, err)
	}
	d, err := s.ApproveCode(strings.ToLower(pend.Code[:4]+"-"+pend.Code[4:]), signed.ID)
	if err != nil || d.Pending || d.How != HowCode || d.By != signed.ID {
		t.Fatalf("approve: %+v %v", d, err)
	}
	if _, err := s.ApproveCode(pend.Code, signed.ID); err == nil {
		t.Error("a code worked twice")
	}

	// five wrong tries end every open code
	_, p2, _ := s.Pending(HowCode, "Firefox", "")
	for i := 0; i < 5; i++ {
		c.t = c.t.Add(10 * time.Second)
		if _, err := s.ApproveCode("WRONGONE", ""); err == nil {
			t.Fatal("a wrong code was accepted")
		}
	}
	if _, err := s.ApproveCode(p2.Code, ""); err == nil {
		t.Error("the sixth try with the right code worked after five wrong ones")
	}

	// time ends a code
	c.t = c.t.Add(time.Hour)
	_, p3, _ := s.Pending(HowCode, "Edge", "")
	c.t = c.t.Add(codeTTL + time.Second)
	if _, err := s.ApproveCode(p3.Code, ""); err == nil {
		t.Error("an expired code worked")
	}
	for _, d := range s.List() {
		if d.Pending {
			t.Errorf("an expired pending device is listed: %+v", d)
		}
	}

	// ten a minute
	c.t = c.t.Add(time.Hour)
	var busy bool
	for i := 0; i < 12; i++ {
		if _, _, err := s.Pending(HowCode, "x", ""); err == ErrBusy {
			busy = true
		}
	}
	if !busy {
		t.Error("no rate limit on code requests")
	}
}

// Step-up (0028): a device approves only one that came after its own session.
func TestApprovalNeedsAnOlderSession(t *testing.T) {
	s, _, c := newTest(t)
	_, old := s.Mint(HowLocal, "old")
	c.t = c.t.Add(time.Hour)
	_, pend, _ := s.Pending(HowCode, "new", "")
	c.t = c.t.Add(time.Minute)
	_, newer := s.Mint(HowLink, "newer")
	if _, err := s.ApproveCode(pend.Code, newer.ID); err == nil {
		t.Fatal("a newer session approved an older request")
	}
	if _, err := s.ApproveCode(pend.Code, old.ID); err != nil {
		t.Fatalf("an older session could not approve: %v", err)
	}
	_, p1, _ := s.Pending(HowCode, "p1", "")
	_, p2, _ := s.Pending(HowCode, "p2", "")
	if _, err := s.ApproveDevice(p2.ID, p1.ID); err == nil {
		t.Error("a device approved by a pending one")
	}
}

// Revoke ends the session, the devices it added, theirs, and blocks the nodes.
func TestRevokeCascadeAndNodeBlock(t *testing.T) {
	s, _, c := newTest(t)
	_, a := s.MintFor(HowTailnet, "A", "nA", "")
	c.t = c.t.Add(time.Second)
	sessB, b := s.MintFor(HowLink, "B", "", a.ID)
	_, cc := s.MintFor(HowLink, "C", "nC", b.ID)
	_, other := s.MintFor(HowLocal, "other", "", "")
	var heard []string
	s.OnRevoke(func(ids []string) { heard = ids })
	gone := s.Revoke(a.ID, "device:"+other.ID)
	if len(gone) != 3 || len(heard) != 3 {
		t.Fatalf("revoked %v, heard %v", gone, heard)
	}
	if _, ok := s.Lookup(sessB); ok {
		t.Error("the cascaded session still works")
	}
	if s.NodeState("nA") != "blocked" || s.NodeState("nC") != "blocked" {
		t.Error("the nodes are not blocked")
	}
	var left []string
	for _, d := range s.List() {
		left = append(left, d.ID)
	}
	if len(left) != 1 || left[0] != other.ID {
		t.Errorf("left %v (want only %s); cc=%s", left, other.ID, cc.ID)
	}
	if s.Revoke("dev_nope", "x") != nil {
		t.Error("revoked a device that is not there")
	}
}

// A V0.130 record (no node, no by, no pending) is read as it is, and its
// session works.
func TestV130RecordsAreUsedAsTheyAre(t *testing.T) {
	kv := &memKV{m: map[string]string{}}
	kv.m["auth.devices"] = `[{"id":"dev_old","name":"Chrome on Android","how":"tailnet","created":"2026-11-10T10:00:00Z","lastSeen":"2026-11-16T10:00:00Z","hash":"` + hash("cookie-value") + `"}]`
	c := &clock{t: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
	s, err := Open(kv, c.now)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := s.Lookup("cookie-value")
	if !ok || d.ID != "dev_old" || d.Pending {
		t.Fatalf("%+v %v", d, ok)
	}
	s.BindNode("dev_old", "nPHONE", "phone.ts.net")
	if s.NodeState("nPHONE") != "approved" {
		t.Error("the phone's node is not approved after it signed in during observe")
	}
}
