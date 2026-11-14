package agent

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

type rlHost struct{}

func (rlHost) Observe(string, func(*core.Thread, time.Time), ...Ev) {}
func (rlHost) Thread(id string) (core.Thread, bool) {
	return core.Thread{ID: id, Profile: "claude"}, true
}

// V0.44: the CLI's rate_limit_event reaches OnRateLimit with the thread's
// profile, resolved as the session resolves it.
func TestRateLimitEventReachesQuota(t *testing.T) {
	var gotProfile string
	var gotInfo map[string]any
	m := New(rlHost{}, Config{
		Profile:     func(string) Profile { return Profile{ID: "claudeAgent"} },
		OnRateLimit: func(p string, info map[string]any) { gotProfile, gotInfo = p, info },
	})
	s := &session{thread: "t1", digest: newDigest()}
	m.handle(s, map[string]any{"type": "rate_limit_event", "rate_limit_info": map[string]any{"status": "allowed", "utilization": 0.4}})
	if gotProfile != "claudeAgent" || gotInfo["utilization"] != 0.4 {
		t.Fatalf("got %q %v", gotProfile, gotInfo)
	}
}
