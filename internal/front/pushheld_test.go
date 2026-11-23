package front

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/push"
)

type heldKV map[string]string

func (m heldKV) GetKV(k string) (string, error) { return m[k], nil }
func (m heldKV) SetKV(k, v string) error        { m[k] = v; return nil }

// heldFront is a front with one host "a" that it does not follow: the test
// writes the host's threads itself.
func heldFront(t *testing.T, kv heldKV, p *fakePush) *Front {
	t.Helper()
	f := New(Options{Push: p, Store: kv, Logf: t.Logf}, []HostConfig{{Name: "a", URL: "http://127.0.0.1:1"}})
	f.all()[0].raw = map[string]map[string]any{}
	return f
}

// setThread writes thread id in state, quiet until until when it is not zero.
func setThread(f *Front, id, state string, until time.Time) {
	m := map[string]any{"id": id, "title": "Thread " + id, "projectId": "p1", "project": "Acme",
		"attention": map[string]any{"state": state}}
	if !until.IsZero() {
		m["quietUntil"] = until.UTC().Format(time.RFC3339Nano)
	}
	f.mu.Lock()
	f.all()[0].raw[id] = m
	f.mu.Unlock()
}

func (p *fakePush) take() []push.Payload {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.sent
	p.sent = nil
	return out
}

// V0.push-summary-batch: pushes refused for quiet hours are held across a
// restart, and the window's end sends one summary of what still needs the
// owner; a thread seen meanwhile is left out.
func TestQuietHoursEndWithOneSummary(t *testing.T) {
	kv, p := heldKV{}, &fakePush{}
	f := heldFront(t, kv, p)
	now := time.Now()
	end := now.Add(time.Hour)
	for id, st := range map[string]string{"t1": "done", "t2": "done", "t3": "asking", "t4": "done"} {
		setThread(f, id, st, end)
		f.send("a", "", f.all()[0].raw[id])
	}
	if got := p.take(); len(got) != 0 {
		t.Fatalf("pushed during quiet hours: %+v", got)
	}

	f = heldFront(t, kv, p) // a restart during the window
	for id, st := range map[string]string{"t1": "done", "t2": "done", "t3": "asking", "t4": "seen"} {
		setThread(f, id, st, end)
	}
	f.flushHeld(end.Add(-time.Second))
	if got := p.take(); len(got) != 0 {
		t.Fatalf("summary before the end: %+v", got)
	}
	for id, st := range map[string]string{"t1": "done", "t2": "done", "t3": "asking", "t4": "seen"} {
		setThread(f, id, st, time.Time{}) // the host's tick: the window ended
	}
	f.flushHeld(end)
	got := p.take()
	if len(got) != 1 || got[0].Title != "Acme" || got[0].Body != "1 asking, 2 done" || got[0].ThreadID != "" || got[0].State != "summary" {
		t.Fatalf("summary: %+v", got)
	}
	f.flushHeld(end.Add(time.Minute))
	if got := p.take(); len(got) != 0 {
		t.Fatalf("summary sent twice: %+v", got)
	}
}

// V0.push-summary-batch: nothing at the window's end when all of it was seen
// or settled; one thread left is its own push, which opens it.
func TestQuietHoursEndQuietWhenAllSeen(t *testing.T) {
	p := &fakePush{}
	f := heldFront(t, heldKV{}, p)
	end := time.Now().Add(time.Hour)
	for _, id := range []string{"t1", "t2"} {
		setThread(f, id, "done", end)
		f.send("a", "", f.all()[0].raw[id])
	}
	setThread(f, "t1", "seen", time.Time{})
	setThread(f, "t2", "settled", time.Time{})
	f.flushHeld(end)
	if got := p.take(); len(got) != 0 {
		t.Fatalf("summary of what was seen: %+v", got)
	}

	setThread(f, "t3", "asking", end)
	f.send("a", "", f.all()[0].raw["t3"])
	setThread(f, "t3", "asking", time.Time{})
	f.flushHeld(end)
	if got := p.take(); len(got) != 1 || got[0].ThreadID != "t3" || got[0].State != "asking" {
		t.Fatalf("one thread: %+v", got)
	}
}

// V0.push-summary-batch: after AwayAfter without a beat, done and blocked go
// into one push BatchEvery later; asking still pushes at once; a beat drops
// what was collected before it.
func TestAwayBatch(t *testing.T) {
	p := &fakePush{}
	f := heldFront(t, heldKV{}, p)
	now := time.Now()
	f.presence = &notify.Presence{}
	f.presence.Seen(now.Add(-notify.AwayAfter))
	for id, st := range map[string]string{"t1": "done", "t2": "done", "t3": "blocked", "t4": "asking"} {
		setThread(f, id, st, time.Time{})
		f.send("a", "", f.all()[0].raw[id])
	}
	if got := p.take(); len(got) != 1 || got[0].ThreadID != "t4" {
		t.Fatalf("asking while away is not at once: %+v", got)
	}
	f.flushHeld(now.Add(notify.BatchEvery - time.Second))
	if got := p.take(); len(got) != 0 {
		t.Fatalf("batch early: %+v", got)
	}
	f.flushHeld(now.Add(notify.BatchEvery + time.Second))
	if got := p.take(); len(got) != 1 || got[0].Title != "Fyr" || got[0].Body != "1 blocked, 2 done" || got[0].ThreadID != "" {
		t.Fatalf("batch: %+v", got)
	}

	f.send("a", "", f.all()[0].raw["t1"])
	f.presence.Beat("phone", "", time.Now()) // the owner is back
	f.flushHeld(time.Now().Add(notify.BatchEvery + time.Second))
	if got := p.take(); len(got) != 0 {
		t.Fatalf("a batch after the owner came back: %+v", got)
	}
	if !f.presence.Away(time.Now().Add(notify.AwayAfter)) {
		t.Fatal("not away AwayAfter after the beat")
	}
	f.send("a", "", f.all()[0].raw["t2"])
	if got := p.take(); len(got) != 1 || got[0].ThreadID != "t2" {
		t.Fatalf("done while present is not at once: %+v", got)
	}
}
