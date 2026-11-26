package core

import (
	"strings"
	"testing"
)

// SPEC V0.149: a command the agent wants the owner to run is a run request,
// a thread action with the exact shell text, why and root.

func requestThread() *Thread { return &Thread{ID: "t", Title: "x", Cwd: "/home/o/src"} }

func runReq(t *testing.T, th *Thread, a RunRequestArgs) (*Thread, Result, error) {
	t.Helper()
	nt, _, res, err := Reduce(th, nil, Input{Source: FromAgent, Kind: KindRunRequest, Thread: th.ID, Args: a}, at(1))
	return nt, res, err
}

func TestRunRequestDeclares(t *testing.T) {
	th, _, err := runReq(t, requestThread(), RunRequestArgs{Command: "udisksctl mount -b /dev/sdc1", Why: "Mount the SD card"})
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Offers) != 1 {
		t.Fatalf("offers: %+v", th.Offers)
	}
	o := th.Offers[0]
	if !o.Request || o.ID != "run-1" || o.Run != RunScript || o.On != OnThread || o.Cwd != "/home/o/src" || o.Why != "Mount the SD card" || o.Root {
		t.Fatalf("request: %+v", o)
	}
	if strings.Join(o.Argv, " ") != "bash -euo pipefail -c udisksctl mount -b /dev/sdc1" {
		t.Fatalf("argv: %q", o.Argv)
	}
	if o.Command != "udisksctl mount -b /dev/sdc1" || o.Digest == "" {
		t.Fatalf("command: %+v", o)
	}
	// Needs you until it runs.
	if a := Compute(th, &View{SeenAt: at(9)}, at(10)); a.Section != NeedsYou || a.Reason != "Run: Mount the SD card" {
		t.Fatalf("attention: %+v", a)
	}
	// The next one gets the next id; a root one differs in the digest.
	th2, _, err := runReq(t, th, RunRequestArgs{Command: "nixos-rebuild switch", Why: "Apply the config", Root: true})
	if err != nil {
		t.Fatal(err)
	}
	if r := th2.Offers[1]; r.ID != "run-2" || !r.Root || r.Digest == th2.Offers[0].Digest {
		t.Fatalf("second: %+v", r)
	}
}

// V0.run-cards-in-conversation: the log says where the agent asked, so the
// card can stand there.
func TestRunRequestLogsWhereItWasAsked(t *testing.T) {
	_, res, err := runReq(t, requestThread(), RunRequestArgs{Command: "true", Why: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].Type != EventRunRequested {
		t.Fatalf("events: %+v", res.Events)
	}
	if d := res.Events[0].Data.(map[string]any); d["action"] != "run-1" || d["on"] != OnThread {
		t.Fatalf("data: %+v", d)
	}
}

func TestRunRequestRefusesSudoAndEmpty(t *testing.T) {
	for _, c := range []struct {
		a    RunRequestArgs
		want string
	}{
		{RunRequestArgs{Command: "sudo nixos-rebuild switch", Why: "x"}, "root: true"},
		{RunRequestArgs{Command: "ls\nsudo mount /dev/sdc1 /mnt", Why: "x", Root: true}, "Drop sudo"},
		{RunRequestArgs{Command: "  ", Why: "x"}, "command"},
		{RunRequestArgs{Command: "ls", Why: ""}, "why"},
		{RunRequestArgs{Command: strings.Repeat("x", 8001), Why: "x"}, "8000"},
	} {
		if _, _, err := runReq(t, requestThread(), c.a); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.a.Command, err, c.want)
		}
	}
	// An agent's offer_action cannot make a request by itself.
	h, _ := HolderOf(requestThread(), OnThread)
	if err := h.Declare(ActionSpec{ID: "x", Label: "x", Run: RunScript, Argv: []string{"ls"}, Request: true}); err == nil {
		t.Fatal("offer_action declared a run request")
	}
}

func pressReq(t *testing.T, th *Thread, kind string, args Args) (*Thread, Result, error) {
	t.Helper()
	nt, _, res, err := Reduce(th, nil, Input{Source: FromOwner, Kind: kind, Thread: th.ID, Args: args}, at(3))
	return nt, res, err
}

func TestRunRequestPressStreamStop(t *testing.T) {
	th, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "nixos-rebuild switch", Why: "Apply", Root: true})
	o := th.Offers[0]
	th, res, err := pressReq(t, th, KindActionPress, Args{"on": OnThread, "action": o.ID, "digest": o.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 1 || res.Effects[0].Script == nil || !res.Effects[0].Script.Root {
		t.Fatalf("the press runs as root: %+v", res.Effects)
	}
	if ev := res.Events[0].Data.(map[string]any); ev["command"] != "nixos-rebuild switch" || ev["root"] != true {
		t.Fatalf("the press event records the command: %+v", ev)
	}
	// It is no longer Needs you while it runs.
	if a := Compute(th, &View{SeenAt: at(9)}, at(10)); a.Section == NeedsYou {
		t.Fatalf("running request in Needs you: %+v", a)
	}
	// Output streams into the run.
	th, _, _, err = Reduce(th, nil, Input{Source: Observer(ObsProcess), Kind: KindActionOutput, Thread: th.ID, Args: ActionOutputArgs{On: OnThread, Action: o.ID, Output: "building…\n"}}, at(4))
	if err != nil || th.Runs[0].Output != "building…\n" || !th.Runs[0].Running {
		t.Fatalf("output: %v %+v", err, th.Runs)
	}
	// Stop asks the runner to stop it.
	_, res, err = pressReq(t, th, KindActionStop, Args{"on": OnThread, "action": o.ID})
	if err != nil || len(res.Effects) != 1 || res.Effects[0].Kind != EffectScriptStop || res.Effects[0].Press.Action.ID != o.ID {
		t.Fatalf("stop: %v %+v", err, res.Effects)
	}
	// Nothing to stop when nothing runs.
	idle := requestThread()
	idle, _, _ = runReq(t, idle, RunRequestArgs{Command: "ls", Why: "x"})
	if _, _, err := pressReq(t, idle, KindActionStop, Args{"on": OnThread, "action": "run-1"}); err == nil {
		t.Fatal("stopped a request that does not run")
	}
}

func TestRunRequestNotNow(t *testing.T) {
	th, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "ls", Why: "List it"})
	nt, res, err := pressReq(t, th, KindActionDismiss, Args{"on": OnThread, "action": "run-1"})
	if err != nil || len(nt.Offers) != 0 {
		t.Fatalf("dismiss: %v %+v", err, nt.Offers)
	}
	ns := resultNotices(Reduced{Events: res.Events})
	if len(ns) != 1 || !strings.Contains(ns[0].What, `Not now for "List it"`) {
		t.Fatalf("notice: %+v", ns)
	}
	// Only a run request is dismissed this way.
	h, _ := HolderOf(th, OnThread)
	h.Declare(ActionSpec{ID: "apply", Label: "Apply", Run: RunScript, Argv: []string{"ls"}})
	if _, _, err := pressReq(t, th, KindActionDismiss, Args{"on": OnThread, "action": "apply"}); err == nil {
		t.Fatal("dismissed an action that is not a request")
	}
}

func TestRunRequestResultTellsExitAndTail(t *testing.T) {
	th, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "make", Why: "Build"})
	o := th.Offers[0]
	th, res, _ := pressReq(t, th, KindActionPress, Args{"on": OnThread, "action": o.ID, "digest": o.Digest})
	out := strings.Repeat("line\n", 600) + "error: the end"
	_, _, res, err := Reduce(th, nil, Input{Source: FromEffect("e1"), Kind: KindEffectResult, Thread: th.ID, Args: EffectResult{Effect: res.Effects[0], OK: false, Exit: 2, Text: out}}, at(5))
	if err != nil {
		t.Fatal(err)
	}
	ns := resultNotices(Reduced{Events: res.Events})
	if len(ns) != 1 {
		t.Fatalf("notices: %+v", ns)
	}
	w := ns[0].What
	if !strings.Contains(w, "exit 2") || !strings.Contains(w, "error: the end") || !strings.Contains(w, "```") || len(w) > 2000 {
		t.Fatalf("what: %d %q", len(w), w)
	}
}

func TestProseCommands(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"The minting needs a device:\n\n```\n! sudo /nix/store/prc4lv5v5kppd7-x/bin/mint\n! udisksctl mount -b /dev/sdc1\n```\n\nThe minting runs in a bwrap sandbox:", []string{"sudo /nix/store/prc4lv5v5kppd7-x/bin/mint\nudisksctl mount -b /dev/sdc1"}},
		{"Please run this:\n\n```bash\nnix build .#x\n```", []string{"nix build .#x"}},
		{"Here is the code:\n\n```bash\nnix build .#x\n```", nil},
		{"Try `! ls` inline", nil},
	}
	for _, c := range cases {
		got := ProseCommands(c.text)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q: got %q, want %q", c.text, got, c.want)
		}
	}
}

func TestProseCommandNudgesOnce(t *testing.T) {
	th := requestThread()
	msg := map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Run this:\n\n```\n! udisksctl mount -b /dev/sdc1\n```"}}}}
	ns := runRequestNotices(Reduced{In: Input{Kind: KindDigest}, After: th, Events: []Event{{"msg", msg}}})
	if len(ns) != 1 || !strings.Contains(ns[0].What, "mcp__fyr__run_request") || ns[0].Key != "prose-command" {
		t.Fatalf("nudge: %+v", ns)
	}
	plain := map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Done."}}}}
	if ns := runRequestNotices(Reduced{In: Input{Kind: KindDigest}, After: th, Events: []Event{{"msg", plain}}}); len(ns) != 0 {
		t.Fatalf("plain text nudged: %+v", ns)
	}
}

// V0.149: sudo waits for a password nobody can type in Fyr; the gate names
// run_request instead. sudo -n never asks, so it passes.
func TestGateSendsSudoToRunRequest(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		deny bool
	}{
		{"sudo nixos-rebuild switch", true},
		{"cd /x && sudo -u root mount /dev/sdc1 /mnt", true},
		{"sudo -n true", false},
		{"echo pseudo sudoers", false},
		{"grep sudo /etc/group", false},
	} {
		r := Gate("Bash", map[string]any{"command": c.cmd})
		if (r != "") != c.deny || (c.deny && !strings.Contains(r, "run_request")) {
			t.Errorf("%q: %q", c.cmd, r)
		}
	}
}

// V0.run-long-notice: action.long on a running request tells the agent it
// started, under the result's key so the result replaces it while queued;
// on one that ended it says nothing.
func TestRunRequestLongNotice(t *testing.T) {
	th, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "make", Why: "Build", Root: true})
	o := th.Offers[0]
	th, press, _ := pressReq(t, th, KindActionPress, Args{"on": OnThread, "action": o.ID, "digest": o.Digest})
	long := Input{Source: Observer(ObsProcess), Kind: KindActionLong, Thread: th.ID, Args: ActionLongArgs{On: OnThread, Action: o.ID}}
	_, _, res, err := Reduce(th, nil, long, at(4))
	if err != nil {
		t.Fatal(err)
	}
	ns := resultNotices(Reduced{Events: res.Events})
	if len(ns) != 1 || !strings.Contains(ns[0].What, `Run as root on your run request "Build"`) || !strings.Contains(ns[0].What, "still runs") {
		t.Fatalf("long notice: %+v", ns)
	}
	th, _, res, _ = Reduce(th, nil, Input{Source: FromEffect("e1"), Kind: KindEffectResult, Thread: th.ID, Args: EffectResult{Effect: press.Effects[0], OK: true, Text: "done"}}, at(5))
	end := resultNotices(Reduced{Events: res.Events})
	if len(end) != 1 || end[0].Key != ns[0].Key {
		t.Fatalf("the result does not replace the long notice: %+v %+v", end, ns)
	}
	if _, _, res, _ = Reduce(th, nil, long, at(6)); len(res.Events) != 0 {
		t.Fatalf("a run that ended told it runs: %+v", res.Events)
	}
}

// V0.run-long-notice-next: the start notice never starts a turn. An idle
// agent is not woken by it; the result wakes it once and it reads one line.
// In a live turn it goes out with the next tool call.
func TestRunRequestLongNoticeWaitsForNext(t *testing.T) {
	th, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "make", Why: "Build"})
	o := th.Offers[0]
	th, press, _ := pressReq(t, th, KindActionPress, Args{"on": OnThread, "action": o.ID, "digest": o.Digest})
	long := Input{Source: Observer(ObsProcess), Kind: KindActionLong, Thread: th.ID, Args: ActionLongArgs{On: OnThread, Action: o.ID}}
	th, _, res, err := Reduce(th, nil, long, at(4))
	if err != nil {
		t.Fatal(err)
	}
	key := "result:thread:" + o.ID
	if n := queued(t, th, key); n.Urgency != Next || wakes(res) {
		t.Fatalf("the start notice wakes the agent: %+v wakes=%v", n, wakes(res))
	}
	if _, ns := take(t, th, FeedTake{At: AtIdle}, at(4)); len(ns) != 0 {
		t.Fatalf("an idle agent took the start notice: %q", whats(ns))
	}
	if _, ns := take(t, th, FeedTake{At: AtTool}, at(4)); len(ns) != 1 || !strings.Contains(ns[0].What, "still runs") {
		t.Fatalf("not delivered at the next tool call: %q", whats(ns))
	}
	th, _, res, _ = Reduce(th, nil, Input{Source: FromEffect("e1"), Kind: KindEffectResult, Thread: th.ID, Args: EffectResult{Effect: press.Effects[0], OK: true, Text: "done"}}, at(5))
	if n := queued(t, th, key); n.Urgency != Wake || !wakes(res) {
		t.Fatalf("the result does not wake the agent: %+v wakes=%v", n, wakes(res))
	}
	if _, ns := take(t, th, FeedTake{At: AtIdle}, at(5)); len(ns) != 1 || strings.Contains(ns[0].What, "still runs") {
		t.Fatalf("the agent reads more than the result: %q", whats(ns))
	}
}

// V0.run-card-rerun: action.run and action.dismiss record the acting device and
// front on their action events, matching command events.
func TestActionEventsRecordActor(t *testing.T) {
	th, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "true", Why: "Check"})
	o := th.Offers[0]
	th, _, res, err := Reduce(th, nil, Input{Source: FromOwner, Kind: KindActionPress, Thread: th.ID, Actor: "device:dev_1", Front: "FRNT-0000-0000-0000", Args: Args{"on": OnThread, "action": o.ID, "digest": o.Digest}}, at(3))
	if err != nil {
		t.Fatal(err)
	}
	ev := res.Events[0].Data.(map[string]any)
	if ev["actor"] != "device:dev_1" || ev["front"] != "FRNT-0000-0000-0000" {
		t.Fatalf("press action event missing actor/front: %+v", ev)
	}
	th, _, _, _ = Reduce(th, nil, Input{Source: FromEffect("e1"), Kind: KindEffectResult, Thread: th.ID, Args: EffectResult{Effect: res.Effects[0], OK: true}}, at(4))
	_, _, res, err = Reduce(th, nil, Input{Source: FromOwner, Kind: KindActionDismiss, Thread: th.ID, Actor: "device:dev_2", Front: "FRNT-1111-1111-1111", Args: Args{"on": OnThread, "action": o.ID}}, at(5))
	if err != nil {
		t.Fatal(err)
	}
	ev = res.Events[0].Data.(map[string]any)
	if ev["actor"] != "device:dev_2" || ev["front"] != "FRNT-1111-1111-1111" {
		t.Fatalf("dismiss action event missing actor/front: %+v", ev)
	}
	// No actor on the input leaves the fields out.
	th2, _, _ := runReq(t, requestThread(), RunRequestArgs{Command: "true", Why: "Check"})
	o2 := th2.Offers[0]
	_, _, res, err = Reduce(th2, nil, Input{Source: FromOwner, Kind: KindActionPress, Thread: th2.ID, Args: Args{"on": OnThread, "action": o2.ID, "digest": o2.Digest}}, at(6))
	if err != nil {
		t.Fatal(err)
	}
	ev = res.Events[0].Data.(map[string]any)
	if _, ok := ev["actor"]; ok {
		t.Fatalf("unexpected actor: %+v", ev)
	}
}

// V0.run-cards-new-card: asking with an id that is taken makes a new card; the card the owner sees is not updated.
func TestRunRequestSameIDIsANewCard(t *testing.T) {
	th, _, err := runReq(t, requestThread(), RunRequestArgs{ID: "mount", Command: "true", Why: "first"})
	if err != nil {
		t.Fatal(err)
	}
	th, res, err := runReq(t, th, RunRequestArgs{ID: "mount", Command: "false", Why: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Offers) != 2 || th.Offers[0].ID != "mount" || th.Offers[0].Why != "first" || th.Offers[1].ID != "mount-2" || th.Offers[1].Why != "second" {
		t.Fatalf("offers: %+v", th.Offers)
	}
	if d := res.Events[0].Data.(map[string]any); d["action"] != "mount-2" {
		t.Fatalf("data: %+v", d)
	}
}
