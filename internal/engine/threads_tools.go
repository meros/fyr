package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/store"
)

// window allows max calls per key in a sliding span: the runaway limits of
// agent tools (V0.22 threads, V0.19 screenshots). When st and key are set,
// timestamps survive fyrd restarts (V0.persist-runtime-state).
type window struct {
	mu   sync.Mutex
	st   *store.Store
	key  string
	hits map[string][]time.Time
	max  int
	span time.Duration
}

func (w *window) load(now time.Time) {
	if w.st == nil || w.key == "" {
		return
	}
	raw, _ := w.st.GetKV(w.key)
	if raw == "" {
		return
	}
	var m map[string][]time.Time
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hits = map[string][]time.Time{}
	for k, ts := range m {
		var keep []time.Time
		for _, t := range ts {
			if now.Sub(t) < w.span {
				keep = append(keep, t)
			}
		}
		if len(keep) > 0 {
			w.hits[k] = keep
		}
	}
}

func (w *window) saveLocked(now time.Time) {
	if w.st == nil || w.key == "" {
		return
	}
	out := map[string][]time.Time{}
	for k, ts := range w.hits {
		var keep []time.Time
		for _, t := range ts {
			if now.Sub(t) < w.span {
				keep = append(keep, t)
			}
		}
		if len(keep) > 0 {
			out[k] = keep
		} else {
			delete(w.hits, k)
		}
	}
	if len(out) == 0 {
		w.st.SetKV(w.key, "")
		return
	}
	if b, err := json.Marshal(out); err == nil {
		w.st.SetKV(w.key, string(b))
	}
}

func (w *window) allow(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hits == nil {
		w.hits = map[string][]time.Time{}
	}
	keep := w.hits[key][:0]
	for _, t := range w.hits[key] {
		if now.Sub(t) < w.span {
			keep = append(keep, t)
		}
	}
	if len(keep) >= w.max {
		w.hits[key] = keep
		return false
	}
	w.hits[key] = append(keep, now)
	w.saveLocked(now)
	return true
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d d", int(d.Hours()/24))
}

// lastMessages reads a thread's log back from its end until it holds want
// messages (user turns and agent text blocks), so a run of tool calls cannot
// push the words out of the window (V0.thread-read-messages). It reads at most
// readScanCap events; more reports that older events were left unread.
func (e *Engine) lastMessages(id string, want int) (evs []store.Event, more bool, err error) {
	const page = 500
	before := int64(math.MaxInt64)
	n := 0
	for len(evs) < readScanCap {
		got, err := e.EventsBefore(id, before, page)
		if err != nil {
			return nil, false, err
		}
		if len(got) == 0 {
			return evs, false, nil
		}
		before = got[0].Seq
		// Walk the page back from its end; cut where the want-th message starts.
		for i := len(got) - 1; i >= 0; i-- {
			n += messages(got[i])
			if n >= want {
				evs = append(got[i:], evs...)
				older, err := e.EventsBefore(id, got[i].Seq, 1)
				return evs, err == nil && len(older) > 0, err
			}
		}
		evs = append(got, evs...)
		if len(got) < page {
			return evs, false, nil
		}
	}
	return evs, true, nil
}

// readScanCap bounds how far back thread_read looks for messages.
const readScanCap = 20000

// messages counts the words an event holds for thread_read: a user turn, or
// the text blocks of a top-level agent message.
func messages(ev store.Event) int {
	n := 0
	for _, t := range readTurns([]store.Event{ev}) {
		switch t.Kind {
		case turnUser:
			n++
		case turnAgent:
			for _, p := range t.Parts {
				if p.Tool == "" {
					n++
				}
			}
		}
	}
	return n
}

// threadText renders a thread's last turns as plain text for thread_read.
func threadText(evs []store.Event) string {
	var b strings.Builder
	tools := 0
	flush := func() {
		if tools > 0 {
			fmt.Fprintf(&b, "  (%d tool calls)\n", tools)
			tools = 0
		}
	}
	for _, t := range readTurns(evs) {
		switch t.Kind {
		case turnUser:
			flush()
			fmt.Fprintf(&b, "%s: %s\n", speaker("User", t.From), strings.TrimSpace(t.Text))
		case turnAgent:
			for _, p := range t.Parts {
				if p.Tool != "" {
					tools++
					continue
				}
				flush()
				fmt.Fprintf(&b, "Agent: %s\n", strings.TrimSpace(p.Text))
			}
		case turnEnded:
			flush()
		}
	}
	flush()
	return core.TruncTail(b.String(), 8000)
}

func (e *Engine) threadTools(s *mcp.Server) {

	s.Add(mcp.Tool{Name: "threads",
		Description: "List the user's Fyr threads (other agent sessions): id, pinned (the owner's pin), state, title, project, PR and age, newest first, at most 30. Use it to find related work before starting new work.",
		Schema:      obj(map[string]any{"query": str("Filter on title, project or PR."), "state": str("Only this state: asking, failed, blocked, done, working, waiting, seen, snoozed, settled.")}),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			q := strings.ToLower(strings.TrimSpace(fmt.Sprint(orEmpty(a["query"]))))
			want, _ := a["state"].(string)
			var b strings.Builder
			n := 0
			for _, d := range e.Snapshot() {
				if d.ArchivedAt != nil || n >= 30 {
					continue
				}
				pr := "" // V0.threads-row-pr: the PR the inbox row shows, not the first one the thread got
				if l := core.RowPRText(d.Changes, d.PR); l != "" {
					pr = " · " + l
				}
				pin := "" // V0.104: the owner's pin; agents read it, never set it
				if d.PinnedAt != nil {
					pin = "pinned · "
				}
				line := fmt.Sprintf("%s · %s%s · %s · %s%s · %s", d.ID, pin, d.Attention.State, d.Title, d.Project, pr, age(d.UpdatedAt))
				if q != "" && !strings.Contains(strings.ToLower(line), q) {
					continue
				}
				if want != "" && string(d.Attention.State) != want {
					continue
				}
				if d.ID == self {
					line += " (this thread)"
				}
				b.WriteString(line + "\n")
				n++
			}
			if n == 0 {
				return "No threads match.", nil
			}
			return b.String(), nil
		}})

	s.Add(mcp.Tool{Name: "thread_read",
		Description: "Read the last turns of another Fyr thread as plain text: the user's and the agent's words, with tool calls counted. At most 8,000 characters.",
		Schema:      obj(map[string]any{"id": str("The thread id from threads."), "last": map[string]any{"type": "integer", "description": "How many messages (the user's and the agent's words) to read, default 30, at most 500. Tool calls between them are counted, not limited."}}, "id"),
		Call: func(_ context.Context, _ string, a map[string]any) (string, error) {
			id, _ := a["id"].(string)
			t, ok := e.Thread(id)
			if !ok {
				return "", fmt.Errorf("no thread %q", id)
			}
			last := 30
			if f, ok := a["last"].(float64); ok && f >= 1 {
				last = min(int(f), 500)
			}
			evs, more, err := e.lastMessages(id, last)
			if err != nil {
				return "", err
			}
			head := fmt.Sprintf("Thread %s · %s · %s\n", t.ID, t.Title, t.Cwd)
			if more {
				// V0.thread-read-messages: say that older events were left out.
				head += fmt.Sprintf("(older events not read; this is the last %d messages, ask for more with last)\n", last)
			}
			return head + threadText(evs), nil
		}})

	s.Add(mcp.Tool{Name: "thread_send",
		Description: "Send a message to another Fyr thread's agent; it starts or steers that thread's run, and the receiver sees which thread wrote it. You cannot answer another thread's questions or approvals: those belong to the user.",
		Schema:      obj(map[string]any{"id": str("The thread id."), "text": str("The message.")}, "id", "text"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			id, _ := a["id"].(string)
			text, _ := a["text"].(string)
			if id == self {
				return "", errors.New("that is this thread")
			}
			t, ok := e.Thread(id)
			if !ok {
				return "", fmt.Errorf("no thread %q", id)
			}
			if len(t.Decisions) > 0 {
				return "", errors.New("that thread has an open question or approval for the user; only the user can answer it")
			}
			if w := core.QuotaWaitText(&t, e.Now()); w != "" {
				// V0.quota-held-wakes: the provider would refuse the turn again.
				return "", fmt.Errorf("that thread is stopped by the provider's quota (%s); it continues by itself at the reset, so do not message it now", strings.ToLower(w))
			}
			if !e.sendLimit.allow(self, e.Now()) {
				return "", errors.New("send limit: 30 messages an hour per thread")
			}
			// #48: the receiving thread's Input is the agent's, naming this thread.
			r := e.submit(core.Input{Source: core.FromAgent, From: self, Kind: "thread.send", Thread: id, Args: core.Args{"text": text}, CmdID: NewID("agent_")})
			if !r.OK {
				return "", errors.New(r.Error)
			}
			return fmt.Sprintf("Sent to %q (%s).", t.Title, id), nil
		}})

	s.Add(mcp.Tool{Name: "thread_rename",
		Description: "Rename this Fyr thread, or a thread this thread started (id), so the owner's list says what the work is. Fyr titles a thread by itself after its first run and when its goal is clear; rename it when that title is wrong. Give a short, specific title (3 to 8 words, at most 50 characters, in the language the owner writes in): the goal of the work, like an issue title (\"Header overlaps the menu on phones\"), not the step you are at now. Your title stops Fyr's own. Or pass regenerate: true instead of a title, and Fyr writes one from the thread's conversation so far with a small model. A name the owner gave is kept.",
		Schema: obj(map[string]any{"title": str("The new title."), "id": str("Optional: a thread this thread started; default this thread."),
			"regenerate": map[string]any{"type": "boolean", "description": "Fyr writes the title from the conversation so far; give no title."}}),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			id := sArg(a, "id")
			if id == "" {
				id = self
			}
			t, ok := e.Thread(id)
			if !ok {
				return "", fmt.Errorf("no thread %q", id)
			}
			if id != self && t.Parent != self {
				return "", errors.New("you can rename this thread or a thread it started")
			}
			title := sArg(a, "title")
			if regen, _ := a["regenerate"].(bool); regen {
				// V0.108: no call for a name the owner keeps.
				if err := core.CanWrite(core.FieldThreadTitle, core.ByAgent, t.TitleBy, "this thread"); err != nil {
					return "", err
				}
				made, err := e.makeTitle(id, false)
				if err != nil {
					return "", errors.New(core.TitleFailed(err.Error()))
				}
				title = made
			}
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindThreadRename, Thread: id, Args: map[string]any{"title": title}}); err != nil {
				return "", err
			}
			t, _ = e.Thread(id)
			return fmt.Sprintf("The thread is now %q.", t.Title), nil
		}})

	s.Add(mcp.Tool{Name: "issue_link",
		Description: "Link a GitHub issue to this Fyr thread, so the owner sees it in the thread's header beside its PRs with its title and state. Link the issue the work is about (the one the owner asked you to fix, or the one you file a follow-up in). Fyr links some by itself: an issue a PR of this thread closes (Closes #N), one you file with gh issue create, and one whose URL the owner wrote. unlink: true removes a link that was wrong.",
		Schema: obj(map[string]any{"issue": str("The issue's URL: https://github.com/owner/repo/issues/N."),
			"unlink": map[string]any{"type": "boolean", "description": "Remove the link instead."}}, "issue"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			u, ok := core.CanonicalIssue(sArg(a, "issue"))
			if !ok {
				return "", errors.New("issue must be a GitHub issue URL: https://github.com/owner/repo/issues/N")
			}
			unlink, _ := a["unlink"].(bool)
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindIssueLink, Thread: self, Args: core.IssueLinkArgs{URL: u, Unlink: unlink}}); err != nil {
				return "", err
			}
			if unlink {
				return fmt.Sprintf("Unlinked %s.", u), nil
			}
			return fmt.Sprintf("Linked %s; the thread's header shows it, and Fyr reads its title and state at the next poll.", u), nil
		}})

	s.Add(mcp.Tool{Name: "thread_new",
		Description: "Start a new Fyr thread (a separate agent session) in a folder with a first message, for work that should run on its own. The user sees it in the inbox, marked as started by this thread. At most 12 an hour.",
		Schema: obj(map[string]any{"cwd": str("The folder to work in (absolute path)."), "text": str("The first message: a full brief."),
			"title": str("A short title."), "model": str("Optional model id.")}, "cwd", "text"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			if strings.TrimSpace(sArg(a, "text")) == "" { // V0.empty-thread-start is the owner's: an agent's thread carries its brief
				return "", errors.New("text is required: the new thread's first message, a full brief")
			}
			if !e.newLimit.allow(self, e.Now()) {
				return "", errors.New("limit: 12 new threads an hour per thread")
			}
			args := core.Args{}
			for _, k := range []string{"cwd", "text", "title", "model"} {
				if v, ok := a[k].(string); ok && v != "" {
					args[k] = v
				}
			}
			args = childArgs(e, self, args)
			r := e.submit(core.Input{Source: core.FromAgent, From: self, Kind: "thread.create", Args: args, CmdID: NewID("agent_")})
			if !r.OK {
				return "", errors.New(r.Error)
			}
			return fmt.Sprintf("Started thread %s.", r.ThreadID), nil
		}})
}
