package core

import (
	"fmt"
	"testing"
	"time"
)

// V0.rule-fanout: one deploy.self moves many landed changes at
// once, and each change's built-in Rule fires once for its own move. That is
// not a loop: no rule.loop, and every Rule has seen its change's new state.
func TestSelfDeployOfManyChangesIsNoRuleLoop(t *testing.T) {
	th, v := &Thread{ID: "t1"}, &View{}
	merged := ct0
	var ids []string
	for i := 1; i <= 3*maxFirings; i++ {
		c := ChangePRObserved(th, PR{URL: fmt.Sprintf("https://github.com/acme/app/pull/%d", i), Repo: "acme/app", Number: i,
			State: "MERGED", Checks: ChecksPass, MergeSHA: fmt.Sprintf("m%d", i), MergedAt: &merged}, ct0)
		ids = append(ids, c.ID)
	}
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0) // every built-in Rule has its anchor
	res := step(t, th, v, Input{Source: Observer(ObsDeploy), Kind: KindSelfDeployed, Thread: "t1",
		Args: SelfDeployedArgs{Build: "b1", Changes: ids}}, ct0.Add(time.Hour))
	for _, ev := range res.Events {
		if ev.Type == "rule.loop" {
			t.Fatalf("rule.loop on a deploy of %d changes: %v", len(ids), ev.Data)
		}
	}
	for _, c := range th.Changes {
		if c.Phase != PhaseLive {
			t.Fatalf("change %s is %s", c.ID, c.Phase)
		}
	}
}
