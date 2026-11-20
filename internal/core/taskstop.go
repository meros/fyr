package core

import "fmt"

// V0.stop-ends-background: the CLI's interrupt (the owner's Stop) kills
// every background task of the session, subagents included, and tells the
// model nothing. A task the Stop ended is named to the agent with the next
// message, so it knows the work is gone and can start it again.

// stopNoticeText is the sentence for one task the owner's Stop ended.
func stopNoticeText(k Task) string {
	what := "background task"
	switch {
	case k.Type == "local_agent":
		what = "background agent"
	case k.Type == "local_bash":
		what = "background command"
	}
	name := k.Description
	if name == "" {
		name = k.ID
	}
	return fmt.Sprintf("The owner's Stop also ended your %s %q (task %s) before it finished: its result will not come and SendMessage cannot reach it. Start it again if that work should go on.", what, name, k.ID)
}

// taskStopNotices: a background task that went from running to stopped in
// this reduce, by the owner's Stop.
func taskStopNotices(r Reduced) []Notice {
	if r.In.Kind != KindTask {
		return nil
	}
	a, err := argsOf[TaskArgs](r.In)
	if err != nil || !a.ByStop {
		return nil
	}
	was := findTask(r.Before, a.Task.ID)
	now := findTask(r.After, a.Task.ID)
	if was == nil || now == nil || was.Status != TaskRunning || now.Status != TaskStopped || !now.Background {
		return nil
	}
	return []Notice{{About: "thread", Key: "task.stopped:" + now.ID, What: stopNoticeText(*now)}}
}

func findTask(t *Thread, id string) *Task {
	if t == nil {
		return nil
	}
	for i := range t.Tasks {
		if t.Tasks[i].ID == id {
			return &t.Tasks[i]
		}
	}
	return nil
}
