package core

import (
	"strings"
	"testing"
	"time"
)

// SPEC 0.5 V0.69: the agent names its thread; a name the owner gave wins;
// a title is one trimmed line of at most 80 characters.
func TestAgentRename(t *testing.T) {
	now := time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1", Title: "Find the norban token"}
	in := func(title string) Input {
		return Input{Source: FromAgent, Kind: KindThreadRename, Thread: "t1", Args: map[string]any{"title": title}}
	}
	th, _, _, err := Reduce(th, &View{}, in("  Norban token\n for Fyr  "), now)
	if err != nil {
		t.Fatal(err)
	}
	if th.Title != "Norban token for Fyr" || th.TitleBy != ByAgent {
		t.Fatalf("title %q by %q", th.Title, th.TitleBy)
	}
	if _, _, _, err := Reduce(th, &View{}, in(" "), now); err == nil {
		t.Fatal("an empty title was accepted")
	}
	if _, _, _, err := Reduce(th, &View{}, in(strings.Repeat("x", 81)), now); err == nil {
		t.Fatal("a title over 80 characters was accepted")
	}
	own := Input{Source: FromOwner, Kind: "thread.rename", Thread: "t1", Args: map[string]any{"title": "My name"}}
	if th, _, _, err = Reduce(th, &View{}, own, now); err != nil {
		t.Fatal(err)
	}
	if th.TitleBy != ByOwner {
		t.Fatalf("owner rename by %q", th.TitleBy)
	}
	if _, _, _, err := Reduce(th, &View{}, in("Agent name"), now); err == nil || !strings.Contains(err.Error(), "owner named") {
		t.Fatalf("the agent renamed over the owner: %v, %q", err, th.Title)
	}
}
