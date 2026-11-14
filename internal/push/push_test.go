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
	pl := PayloadFor("t1", "Fix lasso", "asking", "Which gesture should close the filter sheet?")
	if pl.Title != "Fix lasso" || pl.ThreadID != "t1" || !strings.HasPrefix(pl.Body, "Asks: Which gesture") {
		t.Fatalf("%+v", pl)
	}
	if pl := PayloadFor("t1", "x", "failed", "API error 500"); pl.Body != "Failed: API error 500" {
		t.Fatalf("%+v", pl)
	}
}
