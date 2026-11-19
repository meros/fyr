package push

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/store"
)

type sent struct {
	endpoint string
	payload  Payload
}

func newPusher(t *testing.T, status map[string]int) (*Pusher, *[]sent) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	out := &[]sent{}
	p := &Pusher{DB: st, Subject: "mailto:owner@example.com",
		Send: func(sub Subscription, body []byte, _ Keys) (int, error) {
			var pl Payload
			json.Unmarshal(body, &pl)
			*out = append(*out, sent{sub.Endpoint, pl})
			if c, ok := status[sub.Endpoint]; ok {
				return c, nil
			}
			return 201, nil
		}}
	if err := p.Init(); err != nil {
		t.Fatal(err)
	}
	return p, out
}

func sub(endpoint string) Subscription {
	return Subscription{Endpoint: endpoint, Keys: SubKeys{P256dh: "BNc", Auth: "x"}}
}

// SPEC 0.5 V0.15: the VAPID key pair is made once and kept.
func TestKeysPersist(t *testing.T) {
	p, _ := newPusher(t, nil)
	k1 := p.PublicKey()
	p2 := &Pusher{DB: p.DB}
	p2.Init()
	if k1 == "" || p2.PublicKey() != k1 {
		t.Fatalf("keys changed: %q %q", k1, p2.PublicKey())
	}
}

// SPEC 0.5 V0.15: subscriptions are per user; gone ones (404/410) are deleted.
func TestNotifyAndPruneGone(t *testing.T) {
	p, out := newPusher(t, map[string]int{"https://push/gone": 410})
	p.Subscribe("owner", sub("https://push/a"), "phone")
	p.Subscribe("owner", sub("https://push/gone"), "old")
	p.Subscribe("other", sub("https://push/b"), "theirs")
	p.Subscribe("owner", sub("https://push/a"), "phone") // same endpoint again: one row
	n := p.Notify("owner", Payload{Title: "Fix lasso", Body: "Which gesture?", ThreadID: "t1", State: "asking"})
	if n != 1 || len(*out) != 2 {
		t.Fatalf("delivered %d, sent %+v", n, *out)
	}
	for _, s := range *out {
		if strings.Contains(s.endpoint, "/b") {
			t.Fatal("pushed to another user's subscription")
		}
	}
	if p.Known("https://push/gone") || !p.Known("https://push/a") {
		t.Fatal("Known disagrees with the pruning")
	}
	subs, _ := p.Subscriptions("owner")
	if len(subs) != 1 || subs[0].Endpoint != "https://push/a" {
		t.Fatalf("after prune: %+v", subs)
	}
	p.Unsubscribe("https://push/a")
	if subs, _ := p.Subscriptions("owner"); len(subs) != 0 {
		t.Fatal("unsubscribe kept the row")
	}
}

func TestPayloadFor(t *testing.T) {
	// SPEC 0.5 V0.15, design/mobile rule 12: the meta word comes first.
	pl := PayloadFor("t1", "Fix lasso", "asking", "2 questions", "Which gesture should close the filter sheet?")
	if pl.Title != "Fix lasso" || pl.ThreadID != "t1" || pl.Body != "2 questions · Which gesture should close the filter sheet?" {
		t.Fatalf("%+v", pl)
	}
	if pl := PayloadFor("t1", "x", "failed", "Failed", "API error 500"); pl.Body != "Failed · API error 500" {
		t.Fatalf("%+v", pl)
	}
	long := PayloadFor("t1", "x", "asking", "1 question", strings.Repeat("å", 400))
	if r := []rune(long.Body); len(r) > 180 || r[len(r)-1] != '…' {
		t.Fatalf("not clipped by runes: %d", len(r))
	}
}

// V0.notification-polish: the body is one plain line, and a part that is
// empty leaves no separator.
func TestPayloadForIsOnePlainLine(t *testing.T) {
	for _, c := range []struct{ meta, detail, want string }{
		{"Done", "", "Done"},
		{"", "Add login: Merge", "Add login: Merge"},
		{"Done", "Fixed **both** tests in `rows.ts`.\n\nNext:  the phone.", "Done · Fixed both tests in rows.ts. Next: the phone."},
		{"Done", "kept __init__.py and a*b", "Done · kept __init__.py and a*b"},
		{"", "", ""},
	} {
		if got := PayloadFor("t1", "x", "done", c.meta, c.detail).Body; got != c.want {
			t.Errorf("%q + %q: body %q, want %q", c.meta, c.detail, got, c.want)
		}
	}
}

// V0.notification-polish: a device that turned finished threads off gets
// everything else; the test reaches only the device that asked; a finished
// thread is not urgent and a thread's pushes share one topic.
func TestFinishedIsAChoicePerDevice(t *testing.T) {
	p, out := newPusher(t, nil)
	p.Subscribe("owner", sub("https://push/phone"), "phone")
	p.Subscribe("owner", sub("https://push/desk"), "desk")
	if ok, err := p.SetPrefs("https://push/phone", Prefs{Finished: false}); !ok || err != nil {
		t.Fatalf("SetPrefs: %v %v", ok, err)
	}
	for _, c := range []struct {
		state string
		want  int
	}{{"done", 1}, {"asking", 2}, {"failed", 2}, {"blocked", 2}, {"test", 2}} {
		if n := p.Notify("owner", Payload{Title: "x", ThreadID: "t1", State: c.state}); n != c.want {
			t.Errorf("%s: delivered %d, want %d", c.state, n, c.want)
		}
	}
	p.SetPrefs("https://push/phone", Prefs{Finished: true})
	if n := p.Notify("owner", Payload{Title: "x", State: "done"}); n != 2 {
		t.Fatalf("after turning it on again: delivered %d", n)
	}
	// SetPrefs keeps what the device hides, and SetHidden keeps the choice.
	p.SetHidden("https://push/phone", Hidden{Hosts: []string{"lab"}})
	p.SetPrefs("https://push/phone", Prefs{Finished: false})
	p.SetHidden("https://push/phone", Hidden{Hosts: []string{"lab", "attic"}})
	subs, _ := p.Subscriptions("owner")
	for _, s := range subs {
		if s.Endpoint == "https://push/phone" && (!s.NoFinished || s.Hidden == nil || len(s.Hidden.Hosts) != 2) {
			t.Fatalf("one setting lost the other: %+v", s)
		}
	}
	if ok, _ := p.SetPrefs("https://push/gone", Prefs{}); ok {
		t.Fatal("SetPrefs on an unknown endpoint reported ok")
	}

	*out = nil
	if n := p.NotifyOne("owner", "https://push/desk", Payload{Title: "Fyr", State: "test"}); n != 1 || len(*out) != 1 || (*out)[0].endpoint != "https://push/desk" {
		t.Fatalf("NotifyOne: delivered %d to %+v", n, *out)
	}
	if n := p.NotifyOne("owner", "https://push/nobody", Payload{Title: "Fyr"}); n != 0 {
		t.Fatalf("NotifyOne to an unknown endpoint: %d", n)
	}

	if Urgency(Payload{State: "done"}) != "normal" || Urgency(Payload{State: "asking"}) != "high" || Urgency(Payload{State: "blocked"}) != "high" {
		t.Fatal("urgency: only a finished thread may wait")
	}
	a, b := Topic(Payload{ThreadID: "t_1"}), Topic(Payload{ThreadID: "t_2"})
	if a == b || len(a) != 32 || a != Topic(Payload{ThreadID: "t_1", State: "done"}) || Topic(Payload{}) != "" {
		t.Fatalf("topic: %q %q", a, b)
	}
	// V0.client-refs: one id on two hosts is two threads, so two topics.
	if x, y := Topic(Payload{ThreadID: "t_1", Host: "AAAA"}), Topic(Payload{ThreadID: "t_1", Host: "BBBB"}); x == y || x == a {
		t.Fatalf("topic by host: %q %q", x, y)
	}
}

// V0.207: a device that hides a host or project gets no push about it; a push
// about no thread still reaches it, and showing it again restores the pushes.
func TestHiddenLeavesTheDeviceOut(t *testing.T) {
	p, out := newPusher(t, nil)
	p.Subscribe("owner", sub("https://push/phone"), "phone")
	p.Subscribe("owner", sub("https://push/desk"), "desk")
	if ok, err := p.SetHidden("https://push/phone", Hidden{Hosts: []string{"lab"}, Projects: []HiddenProject{{Host: "desk", ID: "p_wayout"}}}); !ok || err != nil {
		t.Fatalf("SetHidden: %v %v", ok, err)
	}
	for _, c := range []struct {
		name string
		o    *Origin
		want int
	}{
		{"hidden project", &Origin{Host: "desk", ProjectID: "p_wayout"}, 1},
		{"same id on another host", &Origin{Host: "other", ProjectID: "p_wayout"}, 2},
		{"hidden host", &Origin{Host: "lab", ProjectID: "p_x"}, 1},
		{"shown", &Origin{Host: "desk", ProjectID: "p_fyr"}, 2},
		{"about no thread", nil, 2},
	} {
		if n := p.Notify("owner", Payload{Title: "x", Origin: c.o}); n != c.want {
			t.Errorf("%s: delivered %d, want %d", c.name, n, c.want)
		}
	}
	*out = nil
	p.SetHidden("https://push/phone", Hidden{})
	if n := p.Notify("owner", Payload{Title: "x", Origin: &Origin{Host: "lab"}}); n != 2 {
		t.Fatalf("after showing again: delivered %d", n)
	}
	if ok, _ := p.SetHidden("https://push/gone", Hidden{}); ok {
		t.Fatal("SetHidden on an unknown endpoint reported ok")
	}
	subs, _ := p.Subscriptions("owner")
	if len(subs) != 2 || subs[0].Keys.Auth == "" {
		t.Fatalf("SetHidden damaged the subscriptions: %+v", subs)
	}
}
