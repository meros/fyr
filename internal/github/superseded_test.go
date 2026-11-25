package github

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.ci-superseded-checks: a cancelled run that a newer run of the same
// check replaced, and a red local/test after the runner's test passed, do
// not count; a red runner check always does.
func TestFetchSupersededAndFallbackChecks(t *testing.T) {
	run := func(wf, name, concl, started string) string {
		return `{"__typename":"CheckRun","workflowName":"` + wf + `","name":"` + name + `","status":"COMPLETED","conclusion":"` + concl + `","startedAt":"` + started + `"}`
	}
	status := func(ctx, state string) string {
		return `{"__typename":"StatusContext","context":"` + ctx + `","state":"` + state + `"}`
	}
	queued := `{"__typename":"CheckRun","workflowName":"pr","name":"test","status":"QUEUED","conclusion":""}`
	cases := []struct {
		name   string
		rollup []string
		want   core.Checks
		failed []string
	}{
		{"marking ready re-ran a workflow and cancelled its first run",
			[]string{run("PR → In Progress", "move", "CANCELLED", "2026-11-25T15:42:12Z"), run("PR → In Progress", "move", "SUCCESS", "2026-11-25T15:42:35Z"), run("CI", "go", "SUCCESS", "2026-11-25T15:42:11Z")},
			core.ChecksPass, nil},
		{"the newer run is the cancelled one",
			[]string{run("wf", "move", "SUCCESS", "2026-11-25T15:42:12Z"), run("wf", "move", "CANCELLED", "2026-11-25T15:42:35Z")},
			core.ChecksFail, []string{"move"}},
		{"a cancelled run is replaced only by a run of its own workflow",
			[]string{run("a", "build", "CANCELLED", "2026-11-25T15:42:12Z"), run("b", "build", "SUCCESS", "2026-11-25T15:42:35Z")},
			core.ChecksFail, []string{"build"}},
		{"a cancelled run alone still fails",
			[]string{run("wf", "move", "CANCELLED", "2026-11-25T15:42:12Z")},
			core.ChecksFail, []string{"move"}},
		{"the newer run still running holds the PR",
			[]string{run("wf", "test", "CANCELLED", "2026-11-25T15:42:12Z"), `{"__typename":"CheckRun","workflowName":"wf","name":"test","status":"IN_PROGRESS","startedAt":"2026-11-25T15:43:00Z"}`},
			core.ChecksPending, nil},
		{"the runner's test passed: a red local/test does not count",
			[]string{run("pr", "test", "SUCCESS", "2026-11-25T15:42:12Z"), status("local/test", "FAILURE")},
			core.ChecksPass, nil},
		{"the runner is down: a green local/test stands in for the queued test",
			[]string{queued, status("local/test", "SUCCESS")},
			core.ChecksPass, nil},
		{"a red runner test is not outvoted by a green local/test",
			[]string{run("pr", "test", "FAILURE", "2026-11-25T15:42:12Z"), status("local/test", "SUCCESS")},
			core.ChecksFail, []string{"test"}},
		{"a red local/test counts while the runner's test has not passed",
			[]string{queued, status("local/test", "FAILURE")},
			core.ChecksFail, []string{"local/test"}},
		{"a local status of a check that does not exist counts as itself",
			[]string{run("pr", "lint", "SUCCESS", "2026-11-25T15:42:12Z"), status("local/test", "FAILURE")},
			core.ChecksFail, []string{"local/test"}},
	}
	for _, c := range cases {
		view := `{"state":"OPEN","statusCheckRollup":[` + strings.Join(c.rollup, ",") + `]}`
		p, err := Fetch(context.Background(), fakeGh(view), "https://github.com/a/b/pull/1")
		if err != nil {
			t.Fatal(err)
		}
		if p.Checks != c.want || strings.Join(p.FailedChecks, ",") != strings.Join(c.failed, ",") {
			t.Errorf("%s: checks %s, failed %v; want %s, %v", c.name, p.Checks, p.FailedChecks, c.want, c.failed)
		}
	}
}

// V0.ci-superseded-checks: the batched read carries a run's workflow and
// start, so the poll reads the superseded run as gh pr view does.
func TestBatchViewKeepsWorkflowOfARun(t *testing.T) {
	const pr = `{"state":"OPEN","headRefOid":"h1","commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
	  {"__typename":"CheckRun","name":"move","status":"COMPLETED","conclusion":"CANCELLED","startedAt":"2026-11-25T15:42:12Z","checkSuite":{"workflowRun":{"workflow":{"name":"PR → In Progress"}}}},
	  {"__typename":"CheckRun","name":"move","status":"COMPLETED","conclusion":"SUCCESS","startedAt":"2026-11-25T15:42:35Z","checkSuite":{"workflowRun":{"workflow":{"name":"PR → In Progress"}}}},
	  {"__typename":"CheckRun","name":"app","status":"COMPLETED","conclusion":"SUCCESS","checkSuite":{"workflowRun":null}},
	  {"__typename":"StatusContext","context":"local/test","state":"SUCCESS"}]}}}}]}}`
	var b batchPR
	if err := json.Unmarshal([]byte(pr), &b); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(b.view())
	if !strings.Contains(string(out), `"workflowName":"PR → In Progress"`) || strings.Contains(string(out), "checkSuite") {
		t.Fatalf("view %s", out)
	}
	p, _, err := fromView(context.Background(), fakeGh(""), "https://github.com/a/b/pull/1", "a/b", 1, out, time.Now())
	if err != nil || p.Checks != core.ChecksPass || p.ChecksTotal != 3 {
		t.Fatalf("%+v %v", p, err)
	}
}

// V0.ci-superseded-checks: the host's last look before it merges reads the
// checks as the merge ask does.
func TestMergeBlockSupersededAndFallback(t *testing.T) {
	d := Detail{State: "OPEN", MergeState: "UNSTABLE", HeadSha: "abcdef0", Checks: []Check{
		{Name: "test", State: "pass"}, {Name: "local/test", State: "fail", Conclusion: "FAILURE"},
		{Name: "move", Workflow: "wf", State: "fail", Conclusion: "CANCELLED"}, {Name: "move", Workflow: "wf", State: "pass", Conclusion: "SUCCESS"}}}
	if got := MergeBlock(d); got != "" {
		t.Errorf("MergeBlock %q", got)
	}
	d.MergeState = "BLOCKED"
	d.Checks[0].State = "fail"
	if got := MergeBlock(d); got != "Blocked: 2 failing checks" {
		t.Errorf("a red runner test: %q", got)
	}
}
