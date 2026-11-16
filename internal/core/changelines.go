package core

import (
	"fmt"
	"strings"
	"time"
)

// ListCap is the most characters change_list, and the changes part of
// fyr_state, return (SPEC V0.96, decision 0024 section 7): one overflowing
// reply cost an agent about 25,000 tokens.
const ListCap = 4000

// ListedOpen reports whether a change is open for change_list, fyr_state,
// handover and compaction (SPEC V0.96 (1)): it has not ended and is not at
// landed or live, or it has a failed condition while landed or live.
func ListedOpen(c *Change) bool {
	if c.Phase.Ended() {
		return false
	}
	if c.Phase != PhaseLanded && c.Phase != PhaseLive {
		return true
	}
	for _, k := range c.Conditions {
		if k.Status == CondFail {
			return true
		}
	}
	return false
}

// ChangeLine is one change in one line (SPEC V0.96 (2)): full id, the PR as
// owner/repo#N or else the key, the title, the phase, and the failed or
// pending facts in a few words.
func ChangeLine(c *Change) string {
	var b strings.Builder
	b.WriteString(c.ID)
	switch {
	case c.PR != nil && c.PR.Repo != "" && c.PR.Number > 0:
		fmt.Fprintf(&b, " · %s#%d", c.PR.Repo, c.PR.Number)
	case c.Key != "" && c.Key != c.ID:
		fmt.Fprintf(&b, " · %s", c.Key)
	}
	fmt.Fprintf(&b, " · %q · %s", c.Title, c.Phase)
	var facts []string
	for _, k := range c.Conditions {
		switch k.Status {
		case CondFail:
			f := string(k.Type) + " failed"
			if k.Reason != "" {
				f += " (" + TruncHead(k.Reason, 60) + ")"
			}
			facts = append(facts, f)
		case CondPending:
			facts = append(facts, string(k.Type)+" pending")
		}
	}
	if c.Parked != nil {
		facts = append(facts, "parked")
	}
	if len(facts) > 0 {
		b.WriteString(" · " + strings.Join(facts, ", "))
	}
	return b.String()
}

// CapLines joins lines under head within ListCap characters. When lines
// are cut it ends with "+N more; pass id for one change".
func CapLines(head string, lines []string) string {
	var b strings.Builder
	if head != "" {
		b.WriteString(head)
	}
	for i, l := range lines {
		sep := 0
		if b.Len() > 0 {
			sep = 1
		}
		need := b.Len() + sep + len(l)
		if i < len(lines)-1 {
			need += 1 + len(moreTail(len(lines)-i-1))
		}
		if need > ListCap {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(moreTail(len(lines) - i))
			return b.String()
		}
		if sep == 1 {
			b.WriteString("\n")
		}
		b.WriteString(l)
	}
	return b.String()
}

func moreTail(n int) string { return fmt.Sprintf("+%d more; pass id for one change", n) }

// OpenChangesText is one line per open change of the thread (full id,
// title, phase) for the first message of a handover and the context after
// a compaction (SPEC V0.96 (4), (5)), or "" when none is open.
func OpenChangesText(t *Thread) string {
	var lines []string
	for i := range t.Changes {
		c := &t.Changes[i]
		if ListedOpen(c) {
			lines = append(lines, fmt.Sprintf("- %s %q · %s", c.ID, TruncHead(c.Title, 120), c.Phase))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return CapLines("[Fyr] Open changes of this thread (change_list {id} gives one in full):", lines)
}

// stateOpenChanges is fyr_state's changes part (SPEC V0.96 (3)): the open
// changes in fyr_state's own form within ListCap characters, and how many
// done changes are left out.
func stateOpenChanges(b *strings.Builder, cs []Change, now time.Time) {
	if len(cs) == 0 {
		stateChanges(b, nil, now)
		return
	}
	var blocks []string
	hidden := 0
	for i := range cs {
		if !ListedOpen(&cs[i]) {
			hidden++
			continue
		}
		var one strings.Builder
		stateChanges(&one, cs[i:i+1], now)
		blocks = append(blocks, strings.TrimSuffix(strings.TrimPrefix(one.String(), "changes:\n"), "\n"))
	}
	head := fmt.Sprintf("changes: %d open", len(blocks))
	if hidden > 0 {
		head += fmt.Sprintf("; %d done not shown (change_list {done: true} lists them)", hidden)
	}
	b.WriteString(CapLines(head, blocks))
	b.WriteString("\n")
}
