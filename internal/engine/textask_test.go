package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

type fakeJudge struct {
	mu    sync.Mutex
	out   string
	err   error
	calls int
	text  string
	block chan struct{}
}

func (f *fakeJudge) judge(_ context.Context, _, text string) (string, error) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.text = text
	return f.out, f.err
}

func (f *fakeJudge) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// SPEC V0.210 (1), (2), (3): after a real run the job reads the end of the agent's text, and a yes lands.
func TestTextAskRunsAfterTheTurn(t *testing.T) {
	h := newHarness(t, time.Minute)
	f := &fakeJudge{out: `{"needs":true,"q":"Which logo?"}`}
	h.e.TextJudge = f.judge
	guardDeps(t, h.e)
	id := created(h, "make a logo")
	th := h.wait(id, "the flag", func(th core.Thread, _ core.Attention) bool { return th.TextAsk != nil })
	f.mu.Lock()
	text := f.text
	f.mu.Unlock()
	if th.TextAsk.Question != "Which logo?" || text == "" || len([]rune(text)) > textAskChars {
		t.Fatalf("flag %+v, text %q", th.TextAsk, text)
	}
	h.wait(id, "Needs you", func(_ core.Thread, a core.Attention) bool {
		return a.Section == core.NeedsYou && a.Reason == "Which logo?"
	})
	if !hasEvent(h.events(id), "textAsk.raised", "Which logo?") {
		t.Fatal("no raised event")
	}
}

// SPEC V0.210 (2): a failed or unreadable call changes nothing, and the run's end never waits for it.
func TestTextAskFailureChangesNothing(t *testing.T) {
	for name, f := range map[string]*fakeJudge{"error": {err: errors.New("no model")}, "garbage": {out: "maybe"}} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Minute)
			f.block = make(chan struct{})
			h.e.TextJudge = f.judge
			guardDeps(t, h.e)
			id := created(h, "hello")
			h.wait(id, "the run's end while the call hangs", func(th core.Thread, _ core.Attention) bool {
				return th.RunEnds > 0 && !th.Running
			})
			close(f.block)
			deadline := time.Now().Add(5 * time.Second)
			for f.n() == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if f.n() == 0 {
				t.Fatal("the job never ran")
			}
			time.Sleep(50 * time.Millisecond)
			if th, _ := h.e.Thread(id); th.TextAsk != nil {
				t.Fatalf("flag %+v", th.TextAsk)
			}
		})
	}
}

// SPEC V0.210 (2): at most the last 500 characters.
func TestTextAskTail(t *testing.T) {
	got := clipTextAsk(strings.Repeat("a", 600) + "end")
	if len([]rune(got)) != textAskChars || !strings.HasSuffix(got, "end") {
		t.Fatalf("%d", len(got))
	}
}
