package core

import (
	"strings"
	"testing"
)

func askIn(id string, qs ...Question) Input {
	return Input{Source: FromAgent, Kind: KindAsk, Args: AskArgs{ID: id, Questions: qs}}
}

// V0.ask-tool: a question asked with Fyr's tool is an ask Decision nobody
// waits on in a tool call: the answer is logged and wakes the agent.
func TestAskOpensAndAnswerWakes(t *testing.T) {
	th := feedThread()
	q1 := Question{Question: " Which database? ", Options: []Option{{Label: "Postgres"}, {Label: "SQLite"}}}
	q2 := Question{Question: "What is the key?"}
	th, res := fstep(t, th, askIn("d_1", q1, q2), ct0)
	if len(th.Decisions) != 1 || !th.Decisions[0].Async || th.Decisions[0].Kind != DecisionAsk || th.Decisions[0].Questions[0].Question != "Which database?" {
		t.Fatalf("decisions: %+v", th.Decisions)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "decision.opened" {
		t.Fatalf("events: %+v", res.Events)
	}
	// The same questions again are the same card.
	th, res = fstep(t, th, askIn("d_2", q1, q2), ct0)
	if len(th.Decisions) != 1 || len(res.Events) != 0 {
		t.Fatalf("asked twice: %+v", th.Decisions)
	}

	th, res = fstep(t, th, Input{Source: FromOwner, Kind: "thread.answer", Args: Args{"decisionId": "d_1", "answers": map[string]any{"Which database?": "SQLite", "What is the key?": "abc"}}}, ct0)
	if len(th.Decisions) != 0 {
		t.Fatalf("still open: %+v", th.Decisions)
	}
	for _, e := range res.Effects {
		if e.Kind == EffectAnswer || e.Kind == EffectAnswered {
			t.Fatalf("an answer effect went to the process: %+v", e)
		}
	}
	n := queued(t, th, "result:ask:d_1")
	want := "The owner answered your questions:\nWhich database? → SQLite\nWhat is the key? → abc"
	if n.What != want || n.Urgency != Wake || n.Self || !wakes(res) {
		t.Fatalf("notice %+v wakes=%v", n, wakes(res))
	}
}

// V0.ask-tool: the question lives in the thread, not in the process.
func TestAskSurvivesTheProcess(t *testing.T) {
	th := feedThread()
	th, _ = fstep(t, th, askIn("d_1", Question{Question: "Go on?"}), ct0)
	th.Decisions = append(th.Decisions, Decision{ID: "d_cli", Kind: DecisionAsk})
	th.Running = true
	if _, err := reduceRestarted(Rec{T: th}, Input{Args: RestartArgs{}}, ct0); err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 1 || th.Decisions[0].ID != "d_1" {
		t.Fatalf("after a restart: %+v", th.Decisions)
	}
	Restarted(th, ct0)
	ProcessLost(th, "exit", "gone", ct0)
	if len(th.Decisions) != 1 || th.Decisions[0].ID != "d_1" {
		t.Fatalf("after the process went: %+v", th.Decisions)
	}
}

func TestAskRefusesBadQuestions(t *testing.T) {
	five := []Option{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}}
	for name, qs := range map[string][]Question{
		"none":         nil,
		"empty":        {{Question: "  "}},
		"twice":        {{Question: "A?"}, {Question: "A?"}},
		"five options": {{Question: "A?", Options: five}},
		"same labels":  {{Question: "A?", Options: []Option{{Label: "x"}, {Label: "x"}}}},
		"five":         {{Question: "1"}, {Question: "2"}, {Question: "3"}, {Question: "4"}, {Question: "5"}},
	} {
		th := feedThread()
		in := askIn("d_1", qs...)
		in.Thread = th.ID
		if _, _, _, err := Reduce(th, nil, in, ct0); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	th := feedThread()
	for i := 0; i < maxOpenAsks; i++ {
		th, _ = fstep(t, th, askIn("d", Question{Question: strings.Repeat("?", i+1)}), ct0)
	}
	in := askIn("d", Question{Question: "one more"})
	in.Thread = th.ID
	if _, _, _, err := Reduce(th, nil, in, ct0); err == nil {
		t.Error("a fifth open question was accepted")
	}
}
