package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StateText is the current state of the thread's records in one compact
// form, for the agent's fyr_state tool (decision 0020 part 5, #47): a notice
// is one sentence and never carries the state, so the agent reads it here.
// about is "" (everything), "thread", "changes", a change's id or key
// ("change:<id>" too), "view", "rules" or "settle".
func StateText(t *Thread, about string, now time.Time) (string, error) {
	about = strings.TrimSpace(about)
	var b strings.Builder
	switch about {
	case "":
		stateThread(&b, t, now)
		stateOpenChanges(&b, t.Changes, now)
		stateView(&b, t, now)
		stateRules(&b, t.Rules, "")
	case "thread", "settle":
		stateThread(&b, t, now)
	case "changes":
		stateOpenChanges(&b, t.Changes, now)
	case "view":
		stateView(&b, t, now)
	case "rules":
		stateRules(&b, t.Rules, "")
	default:
		if id, ok := strings.CutPrefix(about, "rule:"); ok {
			stateRules(&b, t.Rules, id)
			break
		}
		c := FindChange(t, strings.TrimPrefix(about, "change:"))
		if c == nil {
			return "", fmt.Errorf("%w: about is thread, changes, view, rules, settle or a change's id or key; no change %q", ErrBadArgs, about)
		}
		stateChanges(&b, []Change{*c}, now)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "Nothing here yet.", nil
	}
	return out, nil
}

func ago(at, now time.Time) string {
	if at.IsZero() {
		return "never"
	}
	d := now.Sub(at).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return at.UTC().Format(time.RFC3339)
}

func stateThread(b *strings.Builder, t *Thread, now time.Time) {
	run := "idle"
	if t.Running {
		run = "a run is live"
	}
	fmt.Fprintf(b, "thread %s %q · %s", t.ID, t.Title, run)
	if w := ProviderWaitText(t, now); w != "" {
		fmt.Fprintf(b, " · %s", w)
	} else if t.Failure != nil {
		fmt.Fprintf(b, " · last run failed (%s): %s", t.Failure.Class, FailureReason(t, now))
	}
	if t.ProjectID != "" {
		fmt.Fprintf(b, " · project %s", t.ProjectID)
	}
	if t.ArchivedAt != nil {
		b.WriteString(" · archived")
	}
	switch {
	case t.Settle.At != nil:
		fmt.Fprintf(b, " · settled %s", ago(*t.Settle.At, now))
	case SettlesWhenDone(t):
		b.WriteString(" · settles when this run is done")
	case SettlesWhenMerged(t):
		b.WriteString(" · settles when its PR merges")
	case SettleWhenChange(t) != nil:
		s := SettleWhenChange(t)
		fmt.Fprintf(b, " · settles when %s is %s", s.Change, s.Phase)
	}
	if n := len(t.Decisions); n > 0 {
		fmt.Fprintf(b, " · %d question(s) wait for the owner", n)
	}
	b.WriteString("\n")
	stateActions(b, "  thread actions", t.Actions)
}

func stateChanges(b *strings.Builder, cs []Change, now time.Time) {
	if len(cs) == 0 {
		b.WriteString("changes: none\n")
		return
	}
	b.WriteString("changes:\n")
	for _, c := range cs {
		fmt.Fprintf(b, "- %s %q %s · %s", c.ID, c.Title, c.Kind, c.Phase)
		if c.PhaseBy == ByAgent && c.Phase != PhaseWorking {
			b.WriteString(" (reported)")
		}
		if c.Label != "" {
			fmt.Fprintf(b, ", %s", c.Label)
		}
		if c.Branch != "" {
			fmt.Fprintf(b, " · branch %s", c.Branch)
		}
		fmt.Fprintf(b, " · updated %s\n", ago(c.UpdatedAt, now))
		if p := ParkedText(&c); p != "" {
			fmt.Fprintf(b, "  %s\n", p)
		}
		if p := c.PR; p != nil {
			fmt.Fprintf(b, "  PR #%d %s · checks %s", p.Number, p.State, p.Checks)
			if p.Review != "" {
				fmt.Fprintf(b, " · review %s", p.Review)
			}
			if p.UnresolvedThreads > 0 {
				fmt.Fprintf(b, " · %s", ThreadsReason(p.UnresolvedThreads)) // V0.review-thread-notice
			}
			if p.Conflict {
				b.WriteString(" · conflict")
			}
			fmt.Fprintf(b, " · %s\n", p.URL)
		}
		if len(c.Conditions) > 0 {
			parts := make([]string, len(c.Conditions))
			for i, k := range c.Conditions {
				parts[i] = fmt.Sprintf("%s %s", k.Type, k.Status)
				if k.Reason != "" {
					parts[i] += " (" + k.Reason + ")"
				}
				if k.Source != ByObserved {
					parts[i] += " by " + k.Source
				}
			}
			fmt.Fprintf(b, "  conditions: %s\n", strings.Join(parts, ", "))
		}
		for _, a := range c.Annotations {
			fmt.Fprintf(b, "  annotation: %s\n", oneLine(a))
		}
		for _, e := range c.Evidence {
			fmt.Fprintf(b, "  evidence %s: %s\n", e.Kind, firstOf(e.URL, e.Attachment, e.Title))
		}
		stateActions(b, "  actions", c.Actions)
	}
}

func oneLine(v any) string {
	j, _ := json.Marshal(v)
	return string(j)
}

func stateActions(b *strings.Builder, head string, a Actions) {
	if len(a.Offers) == 0 && len(a.Runs) == 0 {
		return
	}
	var parts []string
	for _, o := range a.Offers {
		parts = append(parts, fmt.Sprintf("%s %q (%s)", o.ID, o.Label, o.Run))
	}
	fmt.Fprintf(b, "%s: %s\n", head, strings.Join(parts, ", "))
	if n := len(a.Runs); n > 0 {
		r := a.Runs[n-1]
		status := "in progress"
		switch {
		case !r.Running && r.OK:
			status = "succeeded"
		case !r.Running:
			status = fmt.Sprintf("failed (exit %d)", r.Exit)
		}
		fmt.Fprintf(b, "  last run: %q by %s, %s", r.Label, r.By, status)
		if out := strings.TrimSpace(r.Output); out != "" {
			fmt.Fprintf(b, ", output tail: %s", TruncTail(out, 400))
		}
		b.WriteString("\n")
	}
}

func stateView(b *strings.Builder, t *Thread, now time.Time) {
	if !t.ViewOn() {
		b.WriteString("view: off\n")
		return
	}
	l := t.Live
	if l.PageID == "" {
		b.WriteString("view: on, no page yet\n")
		return
	}
	fmt.Fprintf(b, "view: on · page %s · updated %s · checked %s · state %s\n", l.PageID, ago(l.UpdatedAt, now), ago(l.CheckedAt, now), oneLine(l.State))
	stateActions(b, "  view actions", l.Actions)
}

func stateRules(b *strings.Builder, rs []Rule, id string) {
	n := 0
	for _, r := range rs {
		if (id != "" && r.ID != id) || (id == "" && r.Purpose == PurposeBuiltins) {
			continue // the built-in actions' sync is Fyr's own bookkeeping
		}
		if n == 0 {
			b.WriteString("rules:\n")
		}
		n++
		fmt.Fprintf(b, "- %s %s by %s · %s · when %s then %s", r.ID, r.Purpose, r.CreatedBy, r.Status, oneLine(r.When), r.Then.Kind)
		if note := str(r.Then.Args["note"]); note != "" {
			fmt.Fprintf(b, " (%q)", note)
		}
		b.WriteString("\n")
	}
	if n == 0 && id != "" {
		fmt.Fprintf(b, "no rule %s\n", id)
	}
}
