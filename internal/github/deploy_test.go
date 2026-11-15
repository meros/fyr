package github

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// byArgs answers gh calls by a substring of the joined arguments and records them.
type byArgs struct {
	calls []string
	out   map[string]string
}

func (b *byArgs) run(_ context.Context, args ...string) ([]byte, error) {
	j := strings.Join(args, " ")
	b.calls = append(b.calls, j)
	best := ""
	for k := range b.out {
		if strings.Contains(j, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return nil, fmt.Errorf("unexpected gh %s", j)
	}
	return []byte(b.out[best]), nil
}

func (b *byArgs) count(sub string) int {
	n := 0
	for _, c := range b.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// SPEC 0.5 V0.66 (1): the poller's PR view reads the head and merge commits.
func TestFetchReadsCommits(t *testing.T) {
	g := fakeGh(`{"state":"MERGED","headRefOid":"h1","mergeCommit":{"oid":"m1"},"mergedAt":"2026-11-15T09:00:00Z","statusCheckRollup":[]}`)
	p, err := Fetch(context.Background(), g, "https://github.com/a/b/pull/1")
	if err != nil || p.Head != "h1" || p.MergeSHA != "m1" || p.MergedAt == nil || !p.MergedAt.Equal(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v %v", p, err)
	}
}

// SPEC 0.5 V0.66 (2): a repo without deployments costs one call.
func TestDeploymentsNoneOneCall(t *testing.T) {
	b := &byArgs{out: map[string]string{"repos/o/r/deployments?per_page=30": `[]`}}
	d, err := FetchDeployments(context.Background(), b.run, "o/r", "m1", true)
	if err != nil || !d.None || len(b.calls) != 1 {
		t.Fatalf("%+v %v %v", d, err, b.calls)
	}
}

// SPEC 0.5 V0.66 (2): the newest deployment per environment and its newest
// status; production by the flag or the name.
func TestDeploymentsPerEnvironment(t *testing.T) {
	b := &byArgs{out: map[string]string{
		"repos/o/r/deployments?sha=m1": `[
			{"id": 3, "sha": "m1", "environment": "production", "production_environment": false},
			{"id": 2, "sha": "m1", "environment": "staging"},
			{"id": 1, "sha": "m1", "environment": "production"},
			{"id": 4, "sha": "m1", "environment": "eu", "production_environment": true}]`,
		"deployments/3/statuses": `[{"state": "success", "environment_url": "https://app.example", "log_url": "https://log/3"}]`,
		"deployments/2/statuses": `[{"state": "failure", "log_url": "https://log/2"}]`,
		"deployments/4/statuses": `[]`,
	}}
	d, err := FetchDeployments(context.Background(), b.run, "o/r", "m1", false)
	if err != nil || d.None {
		t.Fatal(d, err)
	}
	want := map[string]core.Deployment{
		"production": {Env: "production", Production: true, State: "success", URL: "https://app.example", LogURL: "https://log/3"},
		"staging":    {Env: "staging", State: "failure", LogURL: "https://log/2"},
		"eu":         {Env: "eu", Production: true, State: "pending"},
	}
	if len(d.Deployments) != 3 {
		t.Fatalf("%+v", d.Deployments)
	}
	for _, x := range d.Deployments {
		if want[x.Env] != x {
			t.Errorf("%s: %+v want %+v", x.Env, x, want[x.Env])
		}
	}
	if b.count("deployments/1/statuses") != 0 {
		t.Fatal("read the status of an older deployment of the same environment")
	}
}

// SPEC 0.5 V0.66 (2): the first call reads the repo's newest deployments and
// keeps this commit's.
func TestDeploymentsFirstFiltersBySHA(t *testing.T) {
	b := &byArgs{out: map[string]string{
		"repos/o/r/deployments?per_page=30": `[{"id": 9, "sha": "other", "environment": "production"}, {"id": 8, "sha": "m1", "environment": "production"}]`,
		"deployments/8/statuses":            `[{"state": "queued"}]`,
	}}
	d, err := FetchDeployments(context.Background(), b.run, "o/r", "m1", true)
	if err != nil || d.None || len(d.Deployments) != 1 || d.Deployments[0].State != "queued" || b.count("deployments/9/") != 0 {
		t.Fatalf("%+v %v %v", d, err, b.calls)
	}
}

// SPEC 0.5 V0.66 (7): the annotations of the failed check runs, capped.
func TestFetchAnnotations(t *testing.T) {
	var many []string
	for i := 0; i < 60; i++ {
		many = append(many, fmt.Sprintf(`{"path": "b.go", "start_line": %d, "end_line": %d, "annotation_level": "warning", "message": "w%d"}`, i+1, i+1, i))
	}
	b := &byArgs{out: map[string]string{
		"commits/h1/check-runs": `{"check_runs": [
			{"id": 11, "name": "lint", "conclusion": "failure", "output": {"annotations_count": 1}},
			{"id": 12, "name": "test", "conclusion": "success", "output": {"annotations_count": 4}},
			{"id": 13, "name": "vet", "conclusion": "failure", "output": {"annotations_count": 60}},
			{"id": 14, "name": "e2e", "conclusion": "failure", "output": {"annotations_count": 0}}]}`,
		"check-runs/11/annotations": `[{"path": "a.go", "start_line": 3, "end_line": 4, "annotation_level": "failure", "title": "unused", "message": "x is unused"}]`,
		"check-runs/13/annotations": "[" + strings.Join(many, ",") + "]",
	}}
	got, err := FetchAnnotations(context.Background(), b.run, "o/r", "h1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Fatalf("%d annotations, want the cap of 50", len(got))
	}
	if got[0] != (core.Annotation{Path: "a.go", Line: 3, EndLine: 4, Level: "failure", Title: "unused", Message: "x is unused", Check: "lint"}) {
		t.Fatalf("first: %+v", got[0])
	}
	if got[1].Check != "vet" || got[1].Line != 1 || got[1].EndLine != 0 {
		t.Fatalf("second: %+v", got[1])
	}
	if b.count("check-runs/12/") != 0 || b.count("check-runs/14/") != 0 {
		t.Fatalf("read annotations of a passing run or a run without any: %v", b.calls)
	}
}

type fakeSrc struct {
	prs      []PRThread
	deploys  []DeployThread
	observed []core.PR
	deployed []core.DeployObs
}

func (f *fakeSrc) Snapshot() []PRThread            { return f.prs }
func (f *fakeSrc) Observe(_ string, pr core.PR)    { f.observed = append(f.observed, pr) }
func (f *fakeSrc) Deploys() []DeployThread         { return f.deploys }
func (f *fakeSrc) ObserveDeploy(_, _ string, d core.DeployObs) { f.deployed = append(f.deployed, d) }

// SPEC 0.5 V0.66 (7): the poller adds annotations to a failing PR, once per
// head commit and number of failed checks.
func TestPollerAnnotatesFailingPR(t *testing.T) {
	view := `{"state":"OPEN","headRefOid":"h1","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]}`
	b := &byArgs{out: map[string]string{
		"pr view":               view,
		"commits/h1/check-runs": `{"check_runs": [{"id": 11, "name": "lint", "conclusion": "failure", "output": {"annotations_count": 1}}]}`,
		"check-runs/11/annotations": `[{"path": "a.go", "start_line": 3, "end_line": 3, "annotation_level": "failure", "message": "boom"}]`,
	}}
	src := &fakeSrc{prs: []PRThread{{ID: "t1", URL: "https://github.com/o/r/pull/7"}}}
	p := &Poller{Gh: b.run, Src: src}
	p.pollAll(context.Background())
	p.pollAll(context.Background())
	if len(src.observed) != 2 || len(src.observed[1].Annotations) != 1 || src.observed[1].Annotations[0].Message != "boom" {
		t.Fatalf("observed: %+v", src.observed)
	}
	if n := b.count("check-runs"); n != 2 {
		t.Fatalf("%d check-run calls for 2 polls of one head; want 2 (runs and annotations once)", n)
	}
}

// SPEC 0.5 V0.66 (2): the poller reads deployments of watched merges and
// passes them on.
func TestPollerReadsDeployments(t *testing.T) {
	b := &byArgs{out: map[string]string{
		"repos/o/r/deployments?per_page=30": `[]`,
	}}
	src := &fakeSrc{deploys: []DeployThread{{ID: "t1", Change: "c1", Repo: "o/r", SHA: "m1", First: true}}}
	p := &Poller{Gh: b.run, Src: src}
	p.pollAll(context.Background())
	if len(src.deployed) != 1 || !src.deployed[0].None || len(b.calls) != 1 {
		t.Fatalf("deployed: %+v calls %v", src.deployed, b.calls)
	}
}
