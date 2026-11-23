package engine

import (
	"testing"
	"time"
)

// V0.review-correctness (6): the deploy state and the revs read e.Now, so a sim's fake
// clock drives them: a finished deploy leaves the stream two minutes after
// it ended on that clock, and revs follow it.
func TestDeployStateAndRevsFollowTheEngineClock(t *testing.T) {
	e, _, now := bare(t)
	*now = time.Date(2020, 1, 1, 10, 0, 0, 0, time.UTC) // far from the wall clock
	e.SetDeploy("v", "done")
	if e.DeployState(*now) == nil {
		t.Fatal("a deploy that just ended is not shown")
	}
	*now = now.Add(3 * time.Minute)
	if d := e.DeployState(*now); d != nil {
		t.Fatalf("a deploy that ended 3 minutes ago is still shown: %v", d)
	}
	e.mu.Lock()
	e.lastRev = 0
	r := e.nextRev()
	e.mu.Unlock()
	if r != now.UnixNano() {
		t.Fatalf("rev %d, want the engine clock's %d", r, now.UnixNano())
	}
}
