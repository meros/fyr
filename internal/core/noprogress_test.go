package core

import (
	"testing"
	"time"
)

func noProgressQueued(th *Thread) int {
	n := 0
	for _, q := range th.Feed.Queue {
		if q.Key == "no-progress" {
			n++
		}
	}
	return n
}

// V0.no-progress (#448): a live run silent for NoProgressAfter is blocked
// at the clock, told once, and cleared by its next progress.
func TestNoProgressMarksAndClears(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t"}, &View{}
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, t0)
	bash := 0
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindDigest, Args: DigestArgs{Activity: "Reading go.mod", OpenBash: &bash}}, t0.Add(time.Minute))
	if at := NextCheck(th, v, t0); at == nil || !at.Equal(t0.Add(time.Minute+NoProgressAfter)) {
		t.Fatalf("next check %v", at)
	}
	step(t, th, v, clockIn(), t0.Add(NoProgressAfter))
	if th.NoProgress != nil {
		t.Fatal("blocked before its time")
	}
	res := step(t, th, v, clockIn(), t0.Add(time.Minute+NoProgressAfter))
	a := Compute(th, v, t0.Add(time.Minute+NoProgressAfter))
	if th.NoProgress == nil || a.State != Blocked || a.Group != GroupUnblock || a.Reason != "No progress for 20 min: last: Reading go.mod at 09:01" {
		t.Fatalf("attention %+v", a)
	}
	if noProgressQueued(th) != 1 || NextCheck(th, v, t0) != nil {
		t.Fatalf("queue %+v", th.Feed.Queue)
	}
	_ = res
	// A later reduce with no progress says nothing again.
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, t0.Add(2*time.Hour))
	if noProgressQueued(th) != 1 {
		t.Fatalf("told twice: %+v", th.Feed.Queue)
	}
	res = step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindDigest, Args: DigestArgs{Activity: "Thinking"}}, t0.Add(2*time.Hour))
	if th.NoProgress != nil || Compute(th, v, t0.Add(2*time.Hour)).State != Working {
		t.Fatalf("progress did not clear: %+v", th.NoProgress)
	}
	if len(res.Events) == 0 || res.Events[len(res.Events)-1].Type != EventProgress {
		t.Fatalf("events %+v", res.Events)
	}
	// The next silence counts from the progress.
	if at := NextCheck(th, v, t0); at == nil || !at.Equal(t0.Add(2*time.Hour+NoProgressAfter)) {
		t.Fatalf("next check %v", at)
	}
}

// V0.no-progress: waits Fyr knows are long do not count as silence, and the
// silence after one counts from its end.
func TestNoProgressLongWaits(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)
	running := func() *Thread {
		th := &Thread{ID: "t"}
		RunStarted(th, t0)
		th.ProgressAt = &t0
		return th
	}
	one := 1
	waits := map[string]func(*Thread){
		"bash": func(th *Thread) { th.OpenBash = one },
		"task": func(th *Thread) { th.Tasks = []Task{{ID: "k", Status: TaskRunning}} },
		"ask":  func(th *Thread) { th.Decisions = []Decision{{ID: "d", Kind: DecisionAsk}} },
		"run":  func(th *Thread) { th.Offers = []ActionSpec{{ID: "r", Request: true, Why: "build"}} },
		"slot": func(th *Thread) { th.SlotQueued = true },
		"watch": func(th *Thread) {
			at := t0.Add(time.Hour)
			th.Rules = []Rule{{ID: "w", Purpose: PurposeWatch, CreatedBy: ByAgent, Status: RuleArmed, When: When{Kind: WhenAt, At: &at}}}
		},
		"machine": func(th *Thread) { th.MachineWait = &MachineWait{Since: t0, Why: "memory"} },
	}
	for name, set := range waits {
		th, v := running(), &View{}
		set(th)
		step(t, th, v, clockIn(), t0.Add(NoProgressAfter+time.Minute))
		if th.NoProgress != nil {
			t.Errorf("%s: blocked", name)
		}
		if th.ProgressAt == nil || !th.ProgressAt.Equal(t0.Add(NoProgressAfter+time.Minute)) {
			t.Errorf("%s: the wait did not move the last progress: %v", name, th.ProgressAt)
		}
	}
	// A Bash call that ends after an hour: the silence counts from its end.
	th, v := running(), &View{}
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindDigest, Args: DigestArgs{OpenBash: &one}}, t0)
	zero := 0
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindDigest, Args: DigestArgs{OpenBash: &zero}}, t0.Add(time.Hour))
	step(t, th, v, clockIn(), t0.Add(time.Hour+NoProgressAfter-time.Second))
	if th.NoProgress != nil {
		t.Fatal("blocked right after a long command")
	}
	// A run that ends drops the state.
	th.NoProgress = &NoProgress{Since: t0, At: t0}
	RunEnded(th, nil, t0)
	step(t, th, v, clockIn(), t0.Add(2*time.Hour))
	if th.NoProgress != nil || th.ProgressAt != nil || NextCheck(th, v, t0) != nil {
		t.Fatalf("ended run keeps %+v %v", th.NoProgress, th.ProgressAt)
	}
}
