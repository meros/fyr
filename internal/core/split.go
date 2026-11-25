package core

import (
	"fmt"
	"slices"
	"strings"
)

// V0.thread-focus: one thread is one topic and about one PR. A thread that
// grows into many unrelated changes or across repos is told once to start
// the next topic in a thread of its own (mcp__fyr__thread_new). Changes
// that belong together are one piece of work: a stack (after) or a group.

// SplitAtPieces is how many separate pieces of work a thread holds when Fyr
// tells it to split: the first is the topic, a second may be a follow-up
// fix, the third is a new topic.
const SplitAtPieces = 3

// EventSplitNudge is the event of the one-time nudge.
const EventSplitNudge = "thread.splitNudge"

// splitAfter runs at the end of each reduce: when this reduce added a
// change and the thread now holds SplitAtPieces separate pieces of work, or
// the new change is the first in a second repo and not tied to the others,
// it marks the thread and says why. Once per thread.
func splitAfter(before, t *Thread) []Event {
	if t.SplitNudged {
		return nil
	}
	var added *Change
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.Kind == KindCode && c.Phase != PhaseAbandoned && !slices.ContainsFunc(before.Changes, func(b Change) bool { return b.ID == c.ID }) {
			added = c
		}
	}
	if added == nil {
		return nil
	}
	pieces := splitPieces(t)
	if len(pieces) < 2 {
		return nil
	}
	var why string
	repos := piecesRepos(pieces)
	mine := pieceOf(pieces, added.ID)
	switch {
	case len(repos) >= 2 && newRepo(pieces, mine):
		why = fmt.Sprintf("This thread now works in %d repos (%s) with changes that are not tied together", len(repos), strings.Join(repos, ", "))
	case len(pieces) >= SplitAtPieces:
		why = fmt.Sprintf("This thread now holds %d separate pieces of work", len(pieces))
	default:
		return nil
	}
	t.SplitNudged = true
	var titles []string
	for _, p := range pieces {
		titles = append(titles, fmt.Sprintf("%q", p[0].Title))
	}
	return []Event{{EventSplitNudge, map[string]any{"why": why, "pieces": strings.Join(titles, ", "), "change": added.ID}}}
}

// splitPieces are the thread's code changes that are not abandoned, joined
// into pieces of work by after and group, in the order they were made.
func splitPieces(t *Thread) [][]*Change {
	var cs []*Change
	for i := range t.Changes {
		if c := &t.Changes[i]; c.Kind == KindCode && c.Phase != PhaseAbandoned {
			cs = append(cs, c)
		}
	}
	parent := make([]int, len(cs))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	join := func(i, j int) {
		if a, b := find(i), find(j); a != b {
			parent[max(a, b)] = min(a, b)
		}
	}
	index := func(c *Change) int { return slices.Index(cs, c) }
	for i, c := range cs {
		for _, ref := range c.After {
			if o := FindChange(t, ref); o != nil {
				if j := index(o); j >= 0 {
					join(i, j)
				}
			}
		}
		for j := range i {
			if c.Group != "" && cs[j].Group == c.Group {
				join(i, j)
			}
		}
	}
	var out [][]*Change
	at := map[int]int{}
	for i, c := range cs {
		r := find(i)
		k, ok := at[r]
		if !ok {
			k = len(out)
			at[r] = k
			out = append(out, nil)
		}
		out[k] = append(out[k], c)
	}
	return out
}

// changeRepo is the repo a change works in, as owner/name in lower case,
// or "" when Fyr does not know it.
func changeRepo(c *Change) string {
	r := ""
	if c.PR != nil && c.PR.Repo != "" {
		r = c.PR.Repo
	} else {
		r = RecordRepo(c)
	}
	r = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r), ".git"))
	for _, p := range []string{"https://", "http://", "github.com/", "github.com:", "git@"} {
		r = strings.TrimPrefix(r, p)
	}
	return strings.TrimPrefix(r, "github.com:")
}

func piecesRepos(pieces [][]*Change) []string {
	var out []string
	for _, p := range pieces {
		for _, c := range p {
			if r := changeRepo(c); r != "" && !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

func pieceOf(pieces [][]*Change, id string) int {
	return slices.IndexFunc(pieces, func(p []*Change) bool {
		return slices.ContainsFunc(p, func(c *Change) bool { return c.ID == id })
	})
}

// newRepo: piece k works in a repo no other piece works in.
func newRepo(pieces [][]*Change, k int) bool {
	if k < 0 {
		return false
	}
	mine := piecesRepos(pieces[k : k+1])
	if len(mine) == 0 {
		return false
	}
	others := piecesRepos(slices.Concat(pieces[:k], pieces[k+1:]))
	return len(others) > 0 && !slices.ContainsFunc(mine, func(r string) bool { return slices.Contains(others, r) })
}

// SplitNudgeText is what the agent is told.
func SplitNudgeText(why, pieces string) string {
	return why + " (" + pieces + "). One thread is one topic and about one PR (or one app and its hosting change). " +
		"Finish the work this thread is about here, and start each other topic with mcp__fyr__thread_new and a full brief " +
		"(the goal, what you found, the repo and branch, links, what is done and what is left) instead of doing it in this thread. " +
		"A change that belongs with another one is not a new topic: open it with after (a stack) or the same group. Fyr says this once."
}

// split: the one-time nudge (V0.thread-focus).
func splitNotices(r Reduced) []Notice {
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != EventSplitNudge {
			continue
		}
		return []Notice{{About: "thread", Key: "split", What: SplitNudgeText(str(d["why"]), str(d["pieces"]))}}
	}
	return nil
}
