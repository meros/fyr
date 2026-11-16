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

// KindTitleMade is the result of a title call the owner started.
const KindTitleMade = "title.made"

// ObsTitle is the observer that reports a made title.
const ObsTitle = "title"

// EffectTitle asks the engine to make a title from the conversation.
const EffectTitle EffectKind = "title"

// TitleMade is a title call's result: the title, or the reason it failed.
// By is who asked (only the owner's press lands here).
type TitleMade struct {
	Title string `json:"title,omitempty"`
	By    string `json:"by"`
	Error string `json:"error,omitempty"`
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
		return Result{Events: []Event{{"effect.error", map[string]any{"kind": "title", "error": TitleFailed(why)}}}}, nil
	}
	if x.By != ByOwner {
		return Result{}, &Rejection{Code: RejectBadArgs, Reason: "only the owner's regenerate lands as a made title"}
	}
	r.T.Title, r.T.TitleBy = title, ByOwner
	return Result{Events: []Event{{"thread.renamed", map[string]any{"title": title, "by": ByOwner, "regenerated": true}}}}, nil
}

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
