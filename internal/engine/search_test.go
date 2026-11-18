package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// V0.ai-search (4): the call reads only the threads asked about, with their
// conversation, and its answer can only name those.
func TestSearchReadsTheGivenThreadsAndKeepsToThem(t *testing.T) {
	h := newHarness(t, time.Minute)
	var digest, query string
	h.e.Searcher = func(_ context.Context, q, d string) (string, error) {
		query, digest = q, d
		return "Here:\n```json\n" + `{"hits":[{"n":1,"why":" the   login\nbug "},{"n":1,"why":"again"},{"n":2,"why":"no such thread"},{"n":0}]}` + "\n```", nil
	}
	guardDeps(t, h.e)
	asked := created(h, "the login page loses the session")
	other := created(h, "secret of another thread")
	hits, err := h.e.SearchThreads(context.Background(), "  sign-in trouble ", []string{asked, "t_gone"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "sign-in trouble" || !strings.Contains(digest, "loses the session") || strings.Contains(digest, "secret of another") {
		t.Fatalf("query %q, digest:\n%s", query, digest)
	}
	if len(hits) != 1 || hits[0].ID != asked || hits[0].Why != "the login bug" || hits[0].ID == other {
		t.Fatalf("hits: %+v", hits)
	}
}

// V0.ai-search: an answer that cannot be read is an error, never "nothing found".
func TestSearchSaysWhenTheAnswerIsUnusable(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.Searcher = func(context.Context, string, string) (string, error) { return "I could not say", nil }
	guardDeps(t, h.e)
	id := created(h, "hello")
	if _, err := h.e.SearchThreads(context.Background(), "x", []string{id}); err == nil {
		t.Fatal("an unreadable answer passed as a result")
	}
}

// V0.ai-search (5): a host runs at most searchCalls at once and says so.
func TestSearchIsBoundedPerHost(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.Searcher = func(context.Context, string, string) (string, error) { return `{"hits":[]}`, nil }
	guardDeps(t, h.e)
	id := created(h, "hello")
	for range searchCalls {
		h.e.searchSlots <- struct{}{}
	}
	defer func() {
		for range searchCalls {
			<-h.e.searchSlots
		}
	}()
	if _, err := h.e.SearchThreads(context.Background(), "x", []string{id}); !errors.Is(err, ErrSearchBusy) {
		t.Fatalf("err = %v", err)
	}
}
