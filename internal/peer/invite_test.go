package peer

// SPEC V0.159, decision 0030 3.6 condition 3: the owner's invite. Single use,
// ten minutes, five wrong tries end it; the code carries the install id.

import (
	"strings"
	"testing"
	"time"
)

type tick struct{ t time.Time }

func (c *tick) now() time.Time { return c.t }

func TestInviteWorksOnce(t *testing.T) {
	c := &tick{time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
	iv := &Invites{Now: c.now}
	code, until := iv.New("F7QK-2M9X-AB34-CD56")
	id, secret, err := ParseInvite(code)
	if err != nil || id != "F7QK-2M9X-AB34-CD56" || len(secret) != 10 {
		t.Fatalf("%q -> %q %q %v", code, id, secret, err)
	}
	if !until.Equal(c.t.Add(10 * time.Minute)) {
		t.Errorf("until %v", until)
	}
	if err := iv.Redeem(strings.ToLower(secret)); err != nil { // case is forgiven
		t.Fatalf("redeem: %v", err)
	}
	if err := iv.Redeem(secret); err == nil {
		t.Error("an invite worked twice")
	}
	if _, ok := iv.Open(); ok {
		t.Error("a used invite is still open")
	}
}

func TestInviteEndsWithTime(t *testing.T) {
	c := &tick{time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
	iv := &Invites{Now: c.now}
	code, _ := iv.New("F7QK-2M9X-AB34-CD56")
	_, secret, _ := ParseInvite(code)
	c.t = c.t.Add(10*time.Minute + time.Second)
	if err := iv.Redeem(secret); err == nil {
		t.Error("an expired invite worked")
	}
}

// Five wrong tries end it: the right secret after them does not work.
func TestInviteEndsAfterFiveWrongTries(t *testing.T) {
	iv := &Invites{}
	code, _ := iv.New("F7QK-2M9X-AB34-CD56")
	_, secret, _ := ParseInvite(code)
	for i := 0; i < MaxInviteTries; i++ {
		if err := iv.Redeem("WRONGWRONG"); err == nil {
			t.Fatal("a wrong secret worked")
		}
	}
	if err := iv.Redeem(secret); err == nil {
		t.Error("the right secret worked after five wrong tries")
	}
	// four wrong ones do not
	code, _ = iv.New("F7QK-2M9X-AB34-CD56")
	_, secret, _ = ParseInvite(code)
	for i := 0; i < MaxInviteTries-1; i++ {
		iv.Redeem("WRONGWRONG")
	}
	if err := iv.Redeem(secret); err != nil {
		t.Errorf("four wrong tries ended it: %v", err)
	}
}

func TestANewInviteEndsTheOldOne(t *testing.T) {
	iv := &Invites{}
	old, _ := iv.New("F7QK-2M9X-AB34-CD56")
	_, oldSecret, _ := ParseInvite(old)
	iv.New("F7QK-2M9X-AB34-CD56")
	if err := iv.Redeem(oldSecret); err == nil {
		t.Error("an old invite worked after a new one was made")
	}
}

func TestParseInvite(t *testing.T) {
	good := "fyr1-F7QK-2M9X-AB34-CD56-ABCDEFGH23"
	if id, sec, err := ParseInvite("  " + strings.ToLower(good) + " "); err != nil || id != "F7QK-2M9X-AB34-CD56" || sec != "ABCDEFGH23" {
		t.Errorf("%q %q %v", id, sec, err)
	}
	for _, bad := range []string{"", "fyr2-F7QK-2M9X-AB34-CD56-ABCDEFGH23", "fyr1-F7QK-2M9X-AB34-CD56", "fyr1-F7QK2M9XAB34CD56-ABCDEFGH23", "fyr1-F7QK-2M9X-AB34-CD56-ABCDEFGH2", "fyr1-F7QK-2M9X-AB34-CD56-ABCDEFGH234", "hello"} {
		if _, _, err := ParseInvite(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
