package core

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Regenerate title (V0.108): Fyr asks a small model for a new thread title
// from the conversation so far. The owner's press replaces any title; the
// agent's request (thread_rename {regenerate: true}) keeps a name the owner
// gave. The engine makes the call; its result comes back as KindTitleMade.

// Automatic titles (V0.auto-title): a thread whose title came from its first
// message gets a made title twice, by itself: once when its first run ends
// (TitleAutoStart), so a vague or image-only first message is named from
// what the agent did, and once when its goal is clear (TitleAutoGoal): its
// first change appears, or it settles. A name the owner or the agent gave
// stops both; a thread named at its start (thread_new's title) has none.
const (
	// TitleAutoFirst: the title is the first message's line; the start
	// title is due.
	TitleAutoFirst = "first"
	// TitleAutoStart: the start title was asked; the goal title is due.
	TitleAutoStart = "start"
	// TitleAutoDone: both were asked (or the goal came first).
	TitleAutoDone = "done"
)

// ByFyr is the made title Fyr asked for by itself (V0.auto-title).
const ByFyr = "fyr"

// KindTitleMade is the result of a title call the owner or Fyr started.
const KindTitleMade = "title.made"

// ObsTitle is the observer that reports a made title.
const ObsTitle = "title"

// EffectTitle asks the engine to make a title from the conversation.
const EffectTitle EffectKind = "title"

// TitleMade is a title call's result: the title, or the reason it failed.
// By is who asked: the owner's press, or Fyr itself (ByFyr).
type TitleMade struct {
	Title string `json:"title,omitempty"`
	By    string `json:"by"`
	Error string `json:"error,omitempty"`
	// Auto is the moment of an automatic title: TitleAutoStart or TitleAutoGoal.
	Auto string `json:"auto,omitempty"`
}

func init() {
	register(&Action{Name: "thread.regenerateTitle", Label: "Regenerate title",
		Apply: func(_ *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			return []Effect{{Kind: EffectTitle, ByOwner: true}}, nil
		}})
	RegisterKind(&InputKind{Name: KindTitleMade, From: observed, Reduce: reduceTitleMade})
}

func reduceTitleMade(r Rec, in Input, _ time.Time) (Result, error) {
	x, err := argsOf[TitleMade](in)
	if err != nil {
		return Result{}, err
	}
	title := CleanTitle(x.Title)
	if x.Error != "" || title == "" {
		why := x.Error
		if why == "" {
			why = "the model gave no title"
		}
		if x.By == ByFyr {
			return Result{}, nil // V0.auto-title (4): nobody asked, so a failure is quiet; the title stays
		}
		return Result{Events: []Event{{"effect.error", map[string]any{"kind": "title", "error": TitleFailed(why)}}}}, nil
	}
	if x.By == ByFyr {
		return autoTitleMade(r.T, x, title), nil
	}
	if x.By != ByOwner {
		return Result{}, &Rejection{Code: RejectBadArgs, Reason: "only the owner's regenerate or Fyr's own lands as a made title"}
	}
	r.T.Title, r.T.TitleBy = title, ByOwner
	return Result{Events: []Event{{"thread.renamed", map[string]any{"title": title, "by": ByOwner, "regenerated": true}}}}, nil
}

// autoTitleMade lands an automatic title (V0.auto-title (3)): only while
// nobody has named the thread, also when they did while the call ran. The
// title stays Fyr's (titleBy ""), so the agent's and the owner's rename and
// the next automatic title may still replace it.
func autoTitleMade(t *Thread, x TitleMade, title string) Result {
	if t.TitleBy != "" || t.TitleAuto == "" || t.Title == title {
		return Result{}
	}
	t.Title = title
	return Result{Events: []Event{{"thread.renamed", map[string]any{"title": title, "by": ByFyr, "auto": x.Auto}}}}
}

// titleAutoAfter asks for the automatic titles (V0.auto-title (1), (2)): at
// the end of the thread's first run, and when its first change appears or
// it settles. Each is asked once; the goal's makes the start's moot.
func titleAutoAfter(before, t *Thread, in Input) []Effect {
	if t.TitleBy != "" || t.TitleAuto == "" || t.TitleAuto == TitleAutoDone {
		return nil
	}
	goal := len(before.Changes) == 0 && len(t.Changes) > 0 || before.Settle.At == nil && t.Settle.At != nil
	switch {
	case goal:
		t.TitleAuto = TitleAutoDone
		return []Effect{{Kind: EffectTitle, TitleAuto: TitleAutoGoal}}
	case t.TitleAuto == TitleAutoFirst && in.Kind == KindRunEnded && before.Running && !t.Running:
		t.TitleAuto = TitleAutoStart
		return []Effect{{Kind: EffectTitle, TitleAuto: TitleAutoStart}}
	}
	return nil
}

// TitleAutoGoal names the goal title in an Effect's TitleAuto.
const TitleAutoGoal = "goal"

// TitleFailed is the line the thread and the agent get when a title call
// fails.
func TitleFailed(why string) string {
	return fmt.Sprintf("Fyr could not regenerate the title: %s. The title stays.", strings.TrimSuffix(strings.TrimSpace(why), "."))
}

// CleanTitle makes a title of what a model wrote: the first line that is not
// empty, without quotes, Markdown emphasis, a "Title:" prefix or a final
// full stop, with its spaces collapsed, cut to MaxTitle characters.
func CleanTitle(s string) string {
	line := ""
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	line = strings.Join(strings.Fields(line), " ")
	for {
		before := line
		for _, p := range []string{"Title:", "title:", "TITLE:"} {
			line = strings.TrimSpace(strings.TrimPrefix(line, p))
		}
		line = strings.Trim(line, "\"'`*_«»“”‘’ ")
		line = strings.TrimSuffix(line, ".")
		if line == before {
			break
		}
	}
	if utf8.RuneCountInString(line) > MaxTitle {
		return string([]rune(line)[:MaxTitle-1]) + "…"
	}
	return line
}
