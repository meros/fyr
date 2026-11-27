package core

import (
	"testing"
	"time"
)

// autoTitles returns the TitleAuto of each title effect in res.
func autoTitles(res Result) []string {
	var out []string
	for _, e := range res.Effects {
		if e.Kind == EffectTitle {
			out = append(out, e.TitleAuto)
		}
	}
	return out
}

func runEnded(t *testing.T, th *Thread, v *View) Result {
	t.Helper()
	th.Running = true
	return step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, ct0)
}

func openChange(t *testing.T, th *Thread, v *View) Result {
	t.Helper()
	return step(t, th, v, Input{Source: FromAgent, Kind: KindChangeOpen, Thread: th.ID, Args: ChangeOpenArgs{Open: ChangeOpen{Key: "x", Kind: KindCode, Title: "X", Branch: "x"}}}, ct0)
}

// V0.auto-title (1), (2): a thread titled from its first message is titled
// by Fyr when its first run ends, and once more when its goal is clear.
func TestAutoTitleAtFirstRunEndAndGoal(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	if _, err := Registry["thread.create"].Apply(th, v, Args{"text": "Stämmer det?", "cwd": "/repo"}, ct0); err != nil {
		t.Fatal(err)
	}
	if th.Title != "Stämmer det?" || th.TitleAuto != TitleAutoFirst {
		t.Fatalf("title %q auto %q", th.Title, th.TitleAuto)
	}
	if got := autoTitles(runEnded(t, th, v)); len(got) != 1 || got[0] != TitleAutoStart {
		t.Fatalf("first run's end asked %v", got)
	}
	if got := autoTitles(runEnded(t, th, v)); len(got) != 0 {
		t.Fatalf("a second run's end asked %v", got)
	}
	if got := autoTitles(openChange(t, th, v)); len(got) != 1 || got[0] != TitleAutoGoal {
		t.Fatalf("the first change asked %v", got)
	}
	if got := autoTitles(step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle"}, ct0)); len(got) != 0 {
		t.Fatalf("settling after the goal title asked %v", got)
	}
}

// V0.auto-title (2): a thread that settles with no change gets its goal
// title then; a goal that comes in the first run makes the start's moot.
func TestAutoTitleOnSettleAndGoalFirst(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "Image", TitleAuto: TitleAutoStart}, &View{}
	if got := autoTitles(step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle"}, ct0)); len(got) != 1 || got[0] != TitleAutoGoal {
		t.Fatalf("settle asked %v", got)
	}
	th, v = &Thread{ID: "t_2", Title: "fix 4711", TitleAuto: TitleAutoFirst}, &View{}
	th.Running = true
	openChange(t, th, v)
	if th.TitleAuto != TitleAutoDone {
		t.Fatalf("auto %q", th.TitleAuto)
	}
	if got := autoTitles(step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, ct0)); len(got) != 0 {
		t.Fatalf("the run's end after the goal asked %v", got)
	}
}

// V0.auto-title (1): a name the owner or the agent gave, or one thread_new
// gave at the start, gets no automatic title.
func TestNoAutoTitleForAGivenName(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	if _, err := Registry["thread.create"].Apply(th, v, Args{"text": "go", "cwd": "/repo", "title": "Fix the flaky test"}, ct0); err != nil {
		t.Fatal(err)
	}
	if th.TitleAuto != "" {
		t.Fatalf("a given title is due an automatic one: %q", th.TitleAuto)
	}
	if got := autoTitles(runEnded(t, th, v)); len(got) != 0 {
		t.Fatalf("asked %v", got)
	}
	for _, by := range []string{ByOwner, ByAgent} {
		th, v := &Thread{ID: "t_1", Title: "Named", TitleBy: by, TitleAuto: TitleAutoFirst}, &View{}
		if got := autoTitles(runEnded(t, th, v)); len(got) != 0 {
			t.Fatalf("%s's name: asked %v", by, got)
		}
		if got := autoTitles(openChange(t, th, v)); len(got) != 0 {
			t.Fatalf("%s's name: the goal asked %v", by, got)
		}
	}
}

// V0.auto-title (3), (4): Fyr's title lands while nobody named the thread
// and stays Fyr's; a name given while the call ran is kept; a failure is
// quiet.
func TestAutoTitleLands(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "Image", TitleAuto: TitleAutoStart}, &View{}
	res := step(t, th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{Title: "Toast unreadable in dark mode.", By: ByFyr, Auto: TitleAutoStart}}, ct0)
	if th.Title != "Toast unreadable in dark mode" || th.TitleBy != "" {
		t.Fatalf("title %q by %q", th.Title, th.TitleBy)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "thread.renamed" || res.Events[0].Data.(map[string]any)["by"] != ByFyr {
		t.Fatalf("events %+v", res.Events)
	}
	th.Title, th.TitleBy = "Mine", ByOwner
	if res := step(t, th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{Title: "Other", By: ByFyr}}, ct0); th.Title != "Mine" || len(res.Events) != 0 {
		t.Fatalf("the owner's name was replaced: %q %+v", th.Title, res.Events)
	}
	th, v = &Thread{ID: "t_1", Title: "Image", TitleAuto: TitleAutoStart}, &View{}
	if res := step(t, th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{By: ByFyr, Error: "no login"}}, ct0); th.Title != "Image" || len(res.Events) != 0 {
		t.Fatalf("a failed automatic title showed: %+v", res.Events)
	}
	// A thread from before automatic titles is never retitled by one.
	th, v = &Thread{ID: "t_1", Title: "Old"}, &View{}
	if step(t, th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{Title: "New", By: ByFyr}}, ct0); th.Title != "Old" {
		t.Fatalf("title %q", th.Title)
	}
}

// V0.auto-title-timely (1): the start title is asked StartTitleAfter into
// the first run, on the agent's next message, not only at the run's end.
func TestAutoTitleEarlyInTheFirstRun(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "order snoozed first", TitleAuto: TitleAutoFirst}, &View{}
	start := ct0
	th.Running, th.RunStartedAt = true, &start
	digest := func(at time.Time) Result {
		return step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindDigest, Args: DigestArgs{Summary: "Reading"}}, at)
	}
	if got := autoTitles(digest(ct0.Add(StartTitleAfter - time.Second))); len(got) != 0 {
		t.Fatalf("a message before StartTitleAfter asked %v", got)
	}
	if got := autoTitles(digest(ct0.Add(StartTitleAfter))); len(got) != 1 || got[0] != TitleAutoStart {
		t.Fatalf("a message at StartTitleAfter asked %v", got)
	}
	if got := autoTitles(digest(ct0.Add(2 * StartTitleAfter))); len(got) != 0 {
		t.Fatalf("a later message asked %v", got)
	}
	if got := autoTitles(runEnded(t, th, v)); len(got) != 0 {
		t.Fatalf("the run's end after the start title asked %v", got)
	}
}

// V0.press-thread-title (2): a thread a press started is first named by
// the press's label, not by Fyr's notice line.
func TestPressThreadPlaceholderIsTheLabel(t *testing.T) {
	text := PressNotice("Start #12 also", "this thread", "Fix the header that overlaps the menu.\nCloses #12.")
	if label, prompt, ok := PressBrief(text); !ok || label != "Start #12 also" || prompt != "Fix the header that overlaps the menu.\nCloses #12." {
		t.Fatalf("brief %q %q %v", label, prompt, ok)
	}
	if _, _, ok := PressBrief("[Fyr] Handover: the owner handed this thread over"); ok {
		t.Fatal("another notice read as a press")
	}
	th, v := &Thread{ID: "t_1"}, &View{}
	if _, err := Registry["thread.create"].Apply(th, v, Args{"text": text, "cwd": "/repo"}, ct0); err != nil {
		t.Fatal(err)
	}
	if th.Title != "Start #12 also" || th.TitleAuto != TitleAutoFirst {
		t.Fatalf("title %q auto %q", th.Title, th.TitleAuto)
	}
	if got := PlaceholderTitle(text); got != th.Title {
		t.Fatalf("placeholder %q", got)
	}
}

// V0.press-thread-title (3): a goal title that found no conversation stays
// due and is asked again at the end of the next run, once.
func TestGoalTitleWithoutConversationIsAskedAgain(t *testing.T) {
	th, v := &Thread{ID: "t_1", Title: "Start #12 also", TitleAuto: TitleAutoFirst}, &View{}
	th.Running = true
	if got := autoTitles(openChange(t, th, v)); len(got) != 1 || got[0] != TitleAutoGoal {
		t.Fatalf("the first change asked %v", got)
	}
	res := step(t, th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_1", Args: TitleMade{By: ByFyr, Auto: TitleAutoGoal, Error: "the thread has no conversation yet", Again: true}}, ct0)
	if len(res.Events) != 0 || th.TitleAuto != TitleAutoAgain {
		t.Fatalf("events %+v auto %q", res.Events, th.TitleAuto)
	}
	if got := autoTitles(runEnded(t, th, v)); len(got) != 1 || got[0] != TitleAutoGoal {
		t.Fatalf("the run's end asked %v", got)
	}
	if th.TitleAuto != TitleAutoDone {
		t.Fatalf("auto %q", th.TitleAuto)
	}
	if got := autoTitles(runEnded(t, th, v)); len(got) != 0 {
		t.Fatalf("a later run's end asked %v", got)
	}
	// A failure for another reason stays quiet and done (V0.auto-title (4)).
	th = &Thread{ID: "t_2", Title: "x", TitleAuto: TitleAutoDone}
	step(t, th, v, Input{Source: Observer(ObsTitle), Kind: KindTitleMade, Thread: "t_2", Args: TitleMade{By: ByFyr, Auto: TitleAutoGoal, Error: "timeout"}}, ct0)
	if th.TitleAuto != TitleAutoDone {
		t.Fatalf("auto %q", th.TitleAuto)
	}
}
