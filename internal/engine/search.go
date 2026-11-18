package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meros/fyr/internal/store"
)

// AI search (V0.ai-search): the owner describes a thread in their own words
// and a small model picks the threads that fit, from each thread's title,
// project, branch and the start and end of its conversation.

// Searcher picks threads for a query from their digest (the query, the
// digest). *agent.Manager's SearchThreads implements it.
type Searcher func(ctx context.Context, query, digest string) (string, error)

// ErrSearchBusy: this host already runs as many searches as it allows.
var ErrSearchBusy = errors.New("another search is running on this host: try again in a moment")

const (
	// searchThreads is the most threads one call reads; the client sends its
	// threads in list order, so these are the ones the owner sees first.
	searchThreads = 120
	// searchHeadEvents and searchTailEvents are how much of a thread's log is
	// read: its first events (what it was started for) and its newest.
	searchHeadEvents = 30
	searchTailEvents = 120
	// searchHeadChars and searchTailChars are what the digest keeps of them.
	searchHeadChars = 300
	searchTailChars = 500
	// searchHits is the most threads an answer gives; searchWhyChars bounds
	// the reason shown under one.
	searchHits     = 12
	searchWhyChars = 120
	searchQueryMax = 500
	// searchTimeout stays under the host's write deadline (60 s).
	searchTimeout = 50 * time.Second
	// searchCalls is how many searches may run at once on this host.
	searchCalls = 2
)

// SearchHit is one thread the model picked and why.
type SearchHit struct {
	ID  string `json:"id"`
	Why string `json:"why,omitempty"`
}

// searchEntry is one thread as the call reads it.
type searchEntry struct {
	id                          string
	title, project, branch, cwd string
}

// SearchThreads asks the model which of the given threads fit the query, best
// first. Only the ids given are read, so what the owner's client may not show
// (a quiet project, V0.114) never reaches the model; an id this host does not
// have is skipped.
func (e *Engine) SearchThreads(ctx context.Context, query string, ids []string) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("the search has no text")
	}
	if r := []rune(query); len(r) > searchQueryMax {
		query = string(r[:searchQueryMax])
	}
	if e.Searcher == nil {
		return nil, errors.New("this host has no AI search")
	}
	entries := e.searchEntries(ids)
	if len(entries) == 0 {
		return []SearchHit{}, nil
	}
	select {
	case e.searchSlots <- struct{}{}:
		defer func() { <-e.searchSlots }()
	default:
		return nil, ErrSearchBusy
	}
	var b strings.Builder
	for i, t := range entries {
		e.writeSearchEntry(&b, i+1, t)
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	out, err := e.Searcher(ctx, query, b.String())
	if err != nil {
		return nil, err
	}
	return parseSearchHits(out, entries)
}

// searchEntries reads the given threads under the lock, at most searchThreads.
func (e *Engine) searchEntries(ids []string) []searchEntry {
	e.mu.Lock()
	defer e.mu.Unlock()
	seen := map[string]bool{}
	var out []searchEntry
	for _, id := range ids {
		t := e.threads[id]
		if t == nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, searchEntry{id: id, title: t.Title, project: e.projectName(t), branch: t.Branch, cwd: t.Cwd})
		if len(out) == searchThreads {
			break
		}
	}
	return out
}

// writeSearchEntry adds one numbered thread to the digest. A thread whose log
// cannot be read still counts by its title.
func (e *Engine) writeSearchEntry(b *strings.Builder, n int, t searchEntry) {
	fmt.Fprintf(b, "[%d] title: %s\n", n, oneLine(t.title))
	if t.project != "" {
		fmt.Fprintf(b, "project: %s\n", oneLine(t.project))
	}
	if t.branch != "" {
		fmt.Fprintf(b, "branch: %s\n", oneLine(t.branch))
	}
	if t.cwd != "" {
		fmt.Fprintf(b, "folder: %s\n", oneLine(t.cwd))
	}
	head, _ := e.st.EventsFor(t.id, 0, searchHeadEvents, false)
	if s := clipStart(oneLine(titleText(head)), searchHeadChars); s != "" {
		fmt.Fprintf(b, "start: %s\n", s)
	}
	if len(head) == searchHeadEvents { // a shorter log is all in the start
		var tail []store.Event
		tail, _ = e.st.EventsFor(t.id, 0, searchTailEvents, true)
		if s := clipEnd(oneLine(titleText(tail)), searchTailChars); s != "" {
			fmt.Fprintf(b, "latest: %s\n", s)
		}
	}
	b.WriteString("\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clipStart(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func clipEnd(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return "…" + string(r[len(r)-n:])
	}
	return s
}

// parseSearchHits reads the model's answer. A number that is no thread of the
// digest is dropped, so the answer can only name threads the owner asked
// about; an answer that is no JSON is an error, not "nothing found".
func parseSearchHits(out string, entries []searchEntry) ([]SearchHit, error) {
	from, to := strings.IndexByte(out, '{'), strings.LastIndexByte(out, '}')
	if from < 0 || to < from {
		return nil, errors.New("the model gave no usable answer")
	}
	var a struct {
		Hits []struct {
			N   int    `json:"n"`
			Why string `json:"why"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out[from:to+1]), &a); err != nil {
		return nil, errors.New("the model gave no usable answer")
	}
	hits := []SearchHit{}
	seen := map[int]bool{}
	for _, h := range a.Hits {
		if h.N < 1 || h.N > len(entries) || seen[h.N] {
			continue
		}
		seen[h.N] = true
		hits = append(hits, SearchHit{ID: entries[h.N-1].id, Why: clipStart(oneLine(h.Why), searchWhyChars)})
		if len(hits) == searchHits {
			break
		}
	}
	return hits, nil
}
