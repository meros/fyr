package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Args are the arguments of one command, as decoded from JSON.
type Args map[string]any

func (a Args) Str(k string) string { s, _ := a[k].(string); return s }

// Effect is work an Action asks the host to do after its state change is
// stored. Apply never performs effects itself, so it stays pure.
type Effect struct {
	Kind   string  `json:"kind"` // send, answer, interrupt, stop, setModel, refresh
	Text   string  `json:"text,omitempty"`
	Images []Image `json:"images,omitempty"`
	// From is the thread whose agent sent this message (V0.22).
	From string `json:"from,omitempty"`
	// Attachments are ids of stored files (SPEC 0.5 V0.18).
	Attachments []string          `json:"attachments,omitempty"`
	DecisionID  string            `json:"decisionId,omitempty"`
	Answers     map[string]string `json:"answers,omitempty"`
	Allow       bool              `json:"allow,omitempty"`
	Model       string            `json:"model,omitempty"`
	// Request is the approved effect for Kind "effect".
	Request *EffectReq `json:"request,omitempty"`
	// MessageID names the message of a send, or the one to cancel (V0.33).
	MessageID string `json:"messageId,omitempty"`
}

// Image is a pasted image, base64 encoded, sent to the agent as a content block.
type Image struct {
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// Action is one entry in the registry. Every surface (keys, buttons,
// palette, bulk selection) calls Actions through one command path.
type Action struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
	Bulk  bool   `json:"bulk,omitempty"`
	// Available returns "" when the action can run, else the reason it cannot.
	Available func(t *Thread, v *View, now time.Time) string `json:"-"`
	// Apply changes t and v in place and returns the effects to run.
	Apply func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) `json:"-"`
}

var ErrBadArgs = errors.New("bad arguments")

// ErrReportLater tells the engine that an effect reports its own result
// later (a deploy reports from the new daemon, SPEC 0.5 V0.13).
var ErrReportLater = errors.New("the effect reports its result later")

func always(*Thread, *View, time.Time) string { return "" }

func notArchived(t *Thread, _ *View, _ time.Time) string {
	if t.ArchivedAt != nil {
		return "The thread is archived"
	}
	return ""
}

func images(a Args) []Image {
	raw, _ := a["images"].([]any)
	var out []Image
	for _, r := range raw {
		m, _ := r.(map[string]any)
		mt, _ := m["mediaType"].(string)
		d, _ := m["data"].(string)
		if mt != "" && d != "" {
			out = append(out, Image{mt, d})
		}
	}
	return out
}

// revive is what any message from the user does to a thread: it clears the
// failure, the settle and the snooze, and counts as seeing the thread.
func revive(t *Thread, v *View, now time.Time) {
	t.Failure = nil
	t.SettledAt = nil
	t.SettleWhenDone = false
	v.SnoozeUntil, v.SnoozedAt = nil, nil
	v.SeenAt = now
}

func sendAction(name, label string) *Action {
	return &Action{
		Name: name, Label: label,
		Available: func(t *Thread, v *View, now time.Time) string {
			if r := notArchived(t, v, now); r != "" {
				return r
			}
			if len(t.Decisions) > 0 {
				return "Answer the open question first"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			text := strings.TrimSpace(a.Str("text"))
			imgs := images(a)
			atts := AttachmentIDs(a)
			if text == "" && len(imgs) == 0 && len(atts) == 0 {
				return nil, fmt.Errorf("%w: empty message", ErrBadArgs)
			}
			if name == "thread.create" {
				t.Cwd = a.Str("cwd")
				if t.Cwd == "" {
					return nil, fmt.Errorf("%w: cwd is required", ErrBadArgs)
				}
				t.Project = ProjectName(t.Cwd)
				t.Model = a.Str("model")
				t.Profile = a.Str("profile")
				t.Title = a.Str("title")
				t.Parent = a.Str("parent")
				t.LiveView, _ = a["view"].(bool)
				if t.Title == "" {
					t.Title = TitleFrom(text)
				}
			}
			revive(t, v, now)
			return []Effect{{Kind: "send", Text: text, Images: imgs, Attachments: atts, From: a.Str("from")}}, nil
		},
	}
}

// Registry is the one list of Actions.
var Registry = map[string]*Action{}

// Ordered lists the registry in a stable order for clients.
func Ordered() []*Action {
	out := make([]*Action, 0, len(Registry))
	for _, a := range Registry {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func register(a *Action) {
	if a.Available == nil {
		a.Available = always
	}
	Registry[a.Name] = a
}

func init() {
	register(sendAction("thread.create", "New thread"))
	register(sendAction("thread.send", "Send"))

	register(&Action{Name: "thread.answer", Label: "Answer",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if len(t.Decisions) == 0 {
				return "Nothing to answer"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			id := a.Str("decisionId")
			idx := -1
			for i, d := range t.Decisions {
				if d.ID == id {
					idx = i
				}
			}
			if idx < 0 {
				return nil, fmt.Errorf("%w: no open decision %q", ErrBadArgs, id)
			}
			d := t.Decisions[idx]
			e := Effect{Kind: "answer", DecisionID: id}
			if d.Kind == "ask" {
				raw, _ := a["answers"].(map[string]any)
				e.Answers = map[string]string{}
				for _, q := range d.Questions {
					s, _ := raw[q.Question].(string)
					if strings.TrimSpace(s) == "" {
						return nil, fmt.Errorf("%w: question %q has no answer", ErrBadArgs, q.Question)
					}
					e.Answers[q.Question] = s
				}
				e.Allow = true
			} else {
				e.Allow, _ = a["allow"].(bool)
			}
			if d.Kind == "effect" {
				// SPEC 5.10: the agent is not waiting; Fyr runs the effect itself.
				e.Kind, e.Request = "effect", d.Effect
			}
			t.Decisions = append(t.Decisions[:idx:idx], t.Decisions[idx+1:]...)
			if len(t.Decisions) == 0 {
				t.Decisions = nil
			}
			v.SeenAt = now
			return []Effect{e}, nil
		}})

	register(&Action{Name: "thread.interrupt", Label: "Stop run", Key: "Escape",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if !t.Running {
				return "No run is going"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			return []Effect{{Kind: "interrupt"}}, nil
		}})

	register(&Action{Name: "thread.retry", Label: "Retry", Key: "r",
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Failure == nil {
				return "The last run did not fail"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			msg := "The last run stopped with an error (" + t.Failure.Message + "). Continue from where you stopped."
			revive(t, v, now)
			return []Effect{{Kind: "send", Text: msg}}, nil
		}})

	register(&Action{Name: "thread.dismissPR", Label: "Dismiss PR",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.PR == nil {
				return "No PR"
			}
			if t.PR.State == "OPEN" {
				return "The PR is still open"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			if t.PR.URL != "" {
				t.DismissedPRs = append(t.DismissedPRs, t.PR.URL)
			}
			t.PR = nil
			return nil, nil
		}})

	register(&Action{Name: "thread.dismiss", Label: "Dismiss failure",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.Failure == nil {
				return "No failure to dismiss"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			t.Failure = nil
			v.SeenAt = now
			return nil, nil
		}})

	register(&Action{Name: "thread.settle", Label: "Settle", Key: "e", Bulk: true,
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.SettledAt != nil {
				return "Already settled"
			}
			if t.SettleWhenDone {
				return "Settles when the run ends"
			}
			return notArchived(t, v, now)
		},
		// SPEC 13.1 `e`: running → settle when done; idle with an open PR →
		// settle when merged; else now. Args{"now": true} (bulk, U11) settles now.
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			force, _ := a["now"].(bool)
			if t.Running { // also for bulk: a live run is never hidden as settled
				t.SettleWhenDone = true
				return nil, nil
			}
			if !force && t.PR != nil && t.PR.State == "OPEN" && !t.SettleWhenMerged {
				t.SettleWhenMerged = true
				v.SeenAt = now
				return nil, nil
			}
			t.SettleWhenDone = false
			t.SettledAt = &now
			t.Failure = nil
			t.SettleWhenMerged = false
			v.SeenAt = now
			v.SnoozeUntil, v.SnoozedAt = nil, nil
			return nil, nil
		}})

	register(&Action{Name: "thread.unsettle", Label: "Unsettle", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.SettledAt == nil && !t.SettleWhenDone && !t.SettleWhenMerged {
				return "Not settled"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			t.SettledAt = nil
			t.SettleWhenDone = false
			t.SettleWhenMerged = false
			return nil, nil
		}})

	register(&Action{Name: "thread.settleWhenMerged", Label: "Settle when merged", Key: "m",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.PR == nil || t.PR.State != "OPEN" {
				return "No open PR"
			}
			if t.SettleWhenMerged {
				return "Already set"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			t.SettleWhenMerged = true
			v.SeenAt = now
			return nil, nil
		}})

	register(&Action{Name: "thread.snooze", Label: "Snooze", Key: "s", Bulk: true,
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.SettledAt != nil {
				return "Settled threads do not snooze"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			until, err := time.Parse(time.RFC3339, a.Str("until"))
			if err != nil || !until.After(now) {
				return nil, fmt.Errorf("%w: until must be a future RFC 3339 time", ErrBadArgs)
			}
			v.SnoozeUntil, v.SnoozedAt = &until, &now
			v.SeenAt = now
			return nil, nil
		}})

	register(&Action{Name: "thread.unsnooze", Label: "Unsnooze", Bulk: true,
		Available: func(_ *Thread, v *View, now time.Time) string {
			if v.SnoozeUntil == nil || !v.SnoozeUntil.After(now) {
				return "Not snoozed"
			}
			return ""
		},
		Apply: func(_ *Thread, v *View, _ Args, _ time.Time) ([]Effect, error) {
			v.SnoozeUntil, v.SnoozedAt = nil, nil
			return nil, nil
		}})

	register(&Action{Name: "thread.archive", Label: "Archive", Key: "#", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.ArchivedAt != nil {
				return "Already archived"
			}
			if t.Running || len(t.Decisions) > 0 {
				return "Stop the run first" // L3: archived threads have no live work
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			t.ArchivedAt = &now
			v.SnoozeUntil, v.SnoozedAt = nil, nil
			return []Effect{{Kind: "stop"}}, nil
		}})

	register(&Action{Name: "thread.unarchive", Label: "Unarchive", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.ArchivedAt == nil {
				return "Not archived"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			t.ArchivedAt = nil
			return nil, nil
		}})

	register(&Action{Name: "thread.markSeen", Label: "Mark seen",
		// SPEC 6.4: up to the activityAt the client showed, never past now.
		Apply: func(_ *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			upTo := now
			if s := a.Str("upTo"); s != "" {
				t, err := time.Parse(time.RFC3339Nano, s)
				if err != nil {
					return nil, fmt.Errorf("%w: upTo must be RFC 3339", ErrBadArgs)
				}
				if t.Before(now) {
					upTo = t
				}
			}
			if upTo.After(v.SeenAt) {
				v.SeenAt = upTo
			}
			return nil, nil
		}})

	register(&Action{Name: "thread.markUnseen", Label: "Mark unseen", Key: "u", Bulk: true,
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			if t.ActivityAt.IsZero() {
				t.ActivityAt = now
			}
			v.SeenAt = t.ActivityAt.Add(-time.Millisecond)
			return nil, nil
		}})

	register(&Action{Name: "thread.setProject", Label: "Move to project",
		// SPEC 0.5 V0.23: the engine checks that the project exists.
		// V0.30: "" unfiles the thread, still by hand, so matching leaves it.
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			if _, ok := a["projectId"].(string); !ok {
				return nil, fmt.Errorf("%w: projectId is required (\"\" for no project)", ErrBadArgs)
			}
			t.ProjectID, t.ProjectManual = a.Str("projectId"), true
			return nil, nil
		}})

	register(&Action{Name: "thread.rename", Label: "Rename",
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			s := strings.TrimSpace(a.Str("title"))
			if s == "" {
				return nil, fmt.Errorf("%w: empty title", ErrBadArgs)
			}
			t.Title = s
			return nil, nil
		}})

	register(&Action{Name: "thread.setView", Label: "Live view",
		// SPEC 0.5 V0.37: the agent's prompt and tools change with the next
		// process, so the host restarts an idle one.
		Available: notArchived,
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			on, ok := a["on"].(bool)
			if !ok {
				return nil, fmt.Errorf("%w: on must be true or false", ErrBadArgs)
			}
			if t.LiveView == on {
				return nil, nil
			}
			t.LiveView = on
			return []Effect{{Kind: "refresh"}}, nil
		}})

	register(&Action{Name: "thread.setModel", Label: "Change model",
		Available: notArchived,
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			m := a.Str("model")
			if m == "" {
				return nil, fmt.Errorf("%w: empty model", ErrBadArgs)
			}
			t.Model = m
			return []Effect{{Kind: "setModel", Model: m}}, nil
		}})
}

// ProjectName is the last path element of a working directory.
func ProjectName(cwd string) string {
	cwd = strings.TrimRight(cwd, "/")
	if i := strings.LastIndex(cwd, "/"); i >= 0 {
		return cwd[i+1:]
	}
	return cwd
}

// TitleFrom makes a first title from the first line of a prompt.
func TitleFrom(text string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(text), "\n", 2)[0])
	if line == "" {
		return "Image"
	}
	r := []rune(line)
	if len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return line
}

// AttachmentIDs reads args.attachments, a list of stored file ids.
func AttachmentIDs(a Args) []string {
	raw, _ := a["attachments"].([]any)
	var out []string
	for _, x := range raw {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
