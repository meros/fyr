package core

import (
	"strings"
	"testing"
	"time"
)

// V0.stall-on-row (2): a subagent that ended saying its report comes later.
func reportLater(t *testing.T, th *Thread, id string, now time.Time) *Thread {
	t.Helper()
	k := Task{ID: id, Status: TaskCompleted, Summary: "This agent has not reported yet: it is waiting on its own background work and will deliver its report through SubagentHandback when that finishes."}
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindTask, Args: TaskArgs{Task: k, Log: []Event{{"task", map[string]any{"kind": "ended", "id": id, "summary": k.Summary}}}}}, now)
	return th
}

func agentThread() *Thread {
	at := ct0.Add(-30 * time.Minute)
	th := &Thread{ID: "t", Title: "x", ActivityAt: at}
	th.Tasks = []Task{{ID: "a1", Description: "Retry lookups", Type: "local_agent", Status: TaskRunning, Background: true, StartedAt: &at}}
	return th
}

// V0.stall-on-row (2): while the report is due the thread waits for it and
// says so; after ReportLostAfter with nothing of the thread running, the
// agent is told once that it will not come, and the row says so.
func TestReportThatNeverComesIsToldOnce(t *testing.T) {
	th := reportLater(t, agentThread(), "a1", ct0)
	k := findTask(th, "a1")
	if k == nil || k.ReportDue == nil || !k.ReportDue.Equal(ct0) || k.ReportLost {
		t.Fatalf("report due not kept: %+v", k)
	}
	if a := Compute(th, &View{}, ct0); a.State != Waiting || a.Reason != "Waiting for the report of Retry lookups" {
		t.Fatalf("pending report: %+v", a)
	}
	if !WorkGoingAt(th, ct0) {
		t.Fatal("a report on its way is work going on")
	}
	// The CLI says it again before the report: the first time counts.
	th = reportLater(t, th, "a1", ct0.Add(5*time.Minute))
	if !findTask(th, "a1").ReportDue.Equal(ct0) {
		t.Fatalf("a repeat moved the bound: %+v", findTask(th, "a1"))
	}
	if at := NextCheck(th, nil, ct0); at == nil || !at.Equal(ct0.Add(ReportLostAfter)) {
		t.Fatalf("clock at %v, want %v", at, ct0.Add(ReportLostAfter))
	}
	// Not yet: nothing said.
	th, res := fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(ReportLostAfter-time.Minute))
	if wakes(res) || findTask(th, "a1").ReportLost {
		t.Fatal("told before the bound")
	}
	th, res = fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(ReportLostAfter))
	n := queued(t, th, "task.report:a1")
	if n.Urgency != Wake || !n.Budget || !wakes(res) || !strings.Contains(n.What, `"Retry lookups" (task a1)`) || !strings.Contains(n.What, "will not come") {
		t.Fatalf("notice %+v wakes=%v", n, wakes(res))
	}
	if a := Compute(th, &View{}, ct0.Add(ReportLostAfter)); a.Reason != "No report from Retry lookups: it will not come" {
		t.Fatalf("lost report row: %+v", a)
	}
	if NextCheck(th, nil, ct0.Add(ReportLostAfter)) != nil {
		t.Fatal("the clock comes back for a report already told")
	}
	th.Feed.Queue = nil
	th, res = fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(2*ReportLostAfter))
	if wakes(res) || len(th.Feed.Queue) != 0 {
		t.Fatalf("told twice: %+v", th.Feed.Queue)
	}
	// The agent's next turn is its answer: the row leaves the lost report.
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, ct0.Add(2*ReportLostAfter))
	if k := findTask(th, "a1"); k.ReportDue != nil || k.ReportLost {
		t.Fatalf("lost report kept after the agent's turn: %+v", k)
	}
}

// V0.stall-on-row (2): the report that comes, or the subagent started again,
// ends the wait; work of the thread that still runs holds the bound.
func TestReportThatComesEndsTheWait(t *testing.T) {
	th := reportLater(t, agentThread(), "a1", ct0)
	done := Task{ID: "a1", Status: TaskCompleted, Summary: "This agent's report was delivered to you as a message from \"a1\" (its SubagentHandback call)."}
	th2, _ := fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindTask, Args: TaskArgs{Task: done, Log: []Event{{"task", map[string]any{"kind": "ended", "id": "a1", "summary": done.Summary}}}}}, ct0.Add(time.Minute))
	if k := findTask(th2, "a1"); k.ReportDue != nil {
		t.Fatalf("delivered report still due: %+v", k)
	}
	again := Task{ID: "a1", Description: "Retry lookups", Type: "local_agent", Status: TaskRunning}
	th3, _ := fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindTask, Args: TaskArgs{Task: again, Log: []Event{{"task", map[string]any{"kind": "started", "id": "a1"}}}}}, ct0.Add(time.Minute))
	if k := findTask(th3, "a1"); k.ReportDue != nil {
		t.Fatalf("resumed subagent still due: %+v", k)
	}
	// Settled: nothing waits for it, nothing is told.
	sl := *th
	sl.Tasks = append([]Task(nil), th.Tasks...)
	sl.Settle.At = &ct0
	th4, res4 := fstep(t, &sl, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(ReportLostAfter))
	if wakes(res4) || findTask(th4, "a1").ReportDue != nil {
		t.Fatalf("a settled thread waits for a report: %+v", findTask(th4, "a1"))
	}
	// Its own background command still runs: no clock, no notice.
	start := ct0
	th.Tasks = append(th.Tasks, Task{ID: "b1", Description: "fetch", Type: "local_bash", Status: TaskRunning, Background: true, StartedAt: &start})
	if NextCheck(th, nil, ct0) != nil {
		t.Fatal("clock armed while the thread's work runs")
	}
	th, res := fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(2*ReportLostAfter))
	if wakes(res) || findTask(th, "a1").ReportLost {
		t.Fatal("told while the thread's work runs")
	}
}

// V0.stall-on-row (1): a background command Fyr found quiet leads the
// Waiting reason as a wait that does not move.
func TestQuietWaitLeadsTheReason(t *testing.T) {
	at := ct0.Add(-40 * time.Minute)
	quiet := ct0.Add(-35 * time.Minute)
	th := &Thread{ID: "t", Title: "x"}
	th.Tasks = []Task{{ID: "b1", Description: "Wait until the host is quiet", Type: "local_bash", Status: TaskRunning, Background: true, StartedAt: &at}}
	if a := Compute(th, &View{}, ct0); a.Reason != "Running in background: Wait until the host is quiet" {
		t.Fatalf("moving wait: %+v", a)
	}
	th.Tasks[0].QuietSince = &quiet
	if a := Compute(th, &View{}, ct0); a.Reason != "Waiting on Wait until the host is quiet: no progress" {
		t.Fatalf("quiet wait: %+v", a)
	}
	th.Tasks = append(th.Tasks, Task{ID: "b2", Description: "other", Type: "local_bash", Status: TaskRunning, Background: true, StartedAt: &at})
	if a := Compute(th, &View{}, ct0); a.Reason != "Waiting on Wait until the host is quiet: no progress" {
		t.Fatalf("quiet wait among two: %+v", a)
	}
}
