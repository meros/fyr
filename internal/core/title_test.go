package core

import (
	"errors"
	"strings"
	"testing"
)

// V0.108 (1): the owner's Regenerate title asks Fyr for a title; the agent
// cannot send it.
func TestRegenerateTitleIsTheOwners(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "Fix the login", TitleBy: ByOwner, ActivityAt: ct0}, &View{SeenAt: ct0}
	a := Registry["thread.regenerateTitle"]
	if a == nil || a.Label != "Regenerate title" {
		t.Fatalf("action %+v", a)
	}
	effs := step(t, th, v, Input{Source: FromOwner, Kind: "thread.regenerateTitle"}, ct0).Effects
	if len(effs) != 1 || effs[0].Kind != EffectTitle || !effs[0].ByOwner {
		t.Fatalf("effects %+v", effs)
	}
	if th.Title != "Fix the login" {
		t.Fatalf("the press changed the title before the model answered: %q", th.Title)
	}
	_, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: "thread.regenerateTitle", Thread: "t_1"}, ct0)
	var r *Rejection
	if !errors.As(err, &r) {
		t.Fatalf("the agent sent the owner's regenerate: %v", err)
	}
}

// V0.108 (1): the title Fyr made for the owner replaces even a name the
// owner gave, and is kept as the owner's.
func TestOwnerRegeneratedTitleReplacesTheOwnersName(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "Fix the login", TitleBy: ByOwner, ActivityAt: ct0}, &View{SeenAt: ct0}
	nt, _, res, err := Reduce(th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{Title: "  Move the login to OAuth  ", By: ByOwner}}, ct0)
	if err != nil {
		t.Fatal(err)
	}
	if nt.Title != "Move the login to OAuth" || nt.TitleBy != ByOwner {
		t.Fatalf("title %q by %q", nt.Title, nt.TitleBy)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "thread.renamed" || res.Events[0].Data.(map[string]any)["regenerated"] != true {
		t.Fatalf("events %+v", res.Events)
	}
}

// V0.108 (3): a failed call keeps the title and says why in the thread.
func TestFailedTitleKeepsTheTitle(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "Fix the login", ActivityAt: ct0}, &View{SeenAt: ct0}
	_, _, res, err := Reduce(th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{By: ByOwner, Error: "the provider needs a login"}}, ct0)
	if err != nil {
		t.Fatal(err)
	}
	if th.Title != "Fix the login" {
		t.Fatalf("title %q", th.Title)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "effect.error" || res.Events[0].Data.(map[string]any)["kind"] != "title" ||
		!strings.Contains(res.Events[0].Data.(map[string]any)["error"].(string), "the provider needs a login") {
		t.Fatalf("events %+v", res.Events)
	}
	// Only an observer reports a made title.
	if _, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{Title: "x", By: ByOwner}}, ct0); err == nil {
		t.Fatal("the agent reported a made title")
	}
}

// V0.108 (2): CleanTitle makes one line of at most MaxTitle characters from
// what the model wrote.
func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Move the login to OAuth":                   "Move the login to OAuth",
		"\n\"Move the login to OAuth.\"\nextra":     "Move the login to OAuth",
		"Title: Move   the login":                   "Move the login",
		"**Regenerate titles in Fyr**":              "Regenerate titles in Fyr",
		"  ":                                        "",
		strings.Repeat("a", 90):                     strings.Repeat("a", 79) + "…",
		"«Byt inloggningen till OAuth för å, ä, ö»": "Byt inloggningen till OAuth för å, ä, ö",
	} {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
