package core

import (
	"fmt"
	"strings"
	"time"
)

// V0.stall-on-row (2): a subagent that waits on its own background work ends
// its task with a summary that says its report comes later, through the CLI's
// SubagentHandback. When that work is gone (ended with the session's run, or
// stopped), the report never comes and the main agent waits for it for ever.

// ReportLostAfter is how long a report may be due, with nothing of the thread
// running, before Fyr calls it lost. Of 33 such reports from 16 to 26 Nov, 32
// came (or the subagent started again) within 30 min; twice that leaves room.
const ReportLostAfter = time.Hour

// reportLaterPrefix opens the CLI's summary of a subagent that has not
// reported yet.
const reportLaterPrefix = "This agent has not reported yet"

func saysReportLater(summary string) bool { return strings.HasPrefix(summary, reportLaterPrefix) }

// reportObserved keeps a task's report state from the CLI's task events: an
// end that says the report comes later starts the wait (the first one counts),
// another end with a summary or a new start ends it.
func reportObserved(t *Thread, a TaskArgs, now time.Time) {
	k := findTask(t, a.Task.ID)
	if k == nil {
		return
	}
	for _, ev := range a.Log {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != "task" || d == nil {
			continue
		}
		sum, _ := d["summary"].(string)
		switch {
		case d["kind"] == "started":
			k.ReportDue, k.ReportLost = nil, false
		case d["kind"] == "ended" && saysReportLater(sum):
			if k.ReportDue == nil {
				k.ReportDue, k.ReportLost = &now, false
			}
		case d["kind"] == "ended" && sum != "":
			k.ReportDue, k.ReportLost = nil, false
		}
	}
}

// reportLostAfter runs at the end of each reduce (the clock's included). A
// report due ReportLostAfter while nothing of the thread runs is lost: one
// event, which tells the agent. The agent's next turn is its answer, so a
// lost report is dropped once a run is live, and any once the thread is
// settled or archived.
func reportLostAfter(t *Thread, now time.Time) []Event {
	var out []Event
	for i := range t.Tasks {
		k := &t.Tasks[i]
		if k.ReportDue == nil {
			continue
		}
		if t.Asleep() { // settled or archived: nothing waits for it any more
			k.ReportDue, k.ReportLost = nil, false
			continue
		}
		if k.ReportLost {
			if t.Running {
				k.ReportDue, k.ReportLost = nil, false
			}
			continue
		}
		if t.RunningTasks() > 0 || now.Sub(*k.ReportDue) < ReportLostAfter {
			continue
		}
		k.ReportLost = true
		out = append(out, Event{"task", map[string]any{"kind": "reportLost", "id": k.ID, "description": k.label(),
			"minutes": int(now.Sub(*k.ReportDue) / time.Minute)}})
	}
	return out
}

// reportCheck is when the clock must re-reduce the thread to call a report
// lost, or nil. While a task runs it needs no clock: the reduce of its end
// reads the bound.
func reportCheck(t *Thread) *time.Time {
	if t.RunningTasks() > 0 {
		return nil
	}
	var out *time.Time
	for _, k := range t.Tasks {
		if k.ReportDue == nil || k.ReportLost {
			continue
		}
		if at := k.ReportDue.Add(ReportLostAfter); out == nil || at.Before(*out) {
			out = &at
		}
	}
	return out
}

// reportWait is the task whose report the thread waits for, or that was lost
// since the agent's last turn; nil without one.
func reportWait(t *Thread) *Task {
	for i := range t.Tasks {
		if t.Tasks[i].ReportDue != nil {
			return &t.Tasks[i]
		}
	}
	return nil
}

func reportText(k *Task) string {
	if k.ReportLost {
		return "No report from " + k.label() + ": it will not come"
	}
	return "Waiting for the report of " + k.label()
}

// reportLostNotices: the agent's one notice, a wake in place of the
// hand-back's that never came.
func reportLostNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != "task" || d["kind"] != "reportLost" {
			continue
		}
		id, _ := d["id"].(string)
		m, _ := d["minutes"].(int)
		out = append(out, Notice{About: "thread", Key: "task.report:" + id, What: fmt.Sprintf(
			"Your subagent %q (task %s) ended %d min ago saying it would hand back its report when its own background work finished. "+
				"Nothing of this thread runs any more and no report came, so it will not come. "+
				"Look at what it left (its files, its worktree) and do or start again what is missing; do not wait for it.", str(d["description"]), id, m)})
	}
	return out
}
