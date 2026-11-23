package agent

import (
	"strings"
	"testing"
	"time"
)

// V0.agent-local-time: the prompt names the owner's time zone and asks for
// local time; a setup without a zone says nothing about it.
func TestPromptZone(t *testing.T) {
	if p := Prompt(FullSetup); strings.Contains(p, "time zone") {
		t.Error("a setup without a zone mentions one")
	}
	p := Prompt(Setup{Git: true, GitHub: true, OS: "linux", Zone: "Europe/Stockholm (CET, UTC+01:00)"})
	for _, w := range []string{"The user's time zone is Europe/Stockholm (CET, UTC+01:00)", "Use local time", "unless there is a reason not to"} {
		if !strings.Contains(p, w) {
			t.Errorf("lacks %q", w)
		}
	}
}

func TestZoneLabel(t *testing.T) {
	old := time.Local
	defer func() { time.Local = old }()
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Skip("no tzdata")
	}
	time.Local = loc
	got := ZoneLabel(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	if got != "Europe/Stockholm (CET, UTC+01:00)" {
		t.Errorf("got %q", got)
	}
}
