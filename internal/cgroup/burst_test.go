package cgroup

import "testing"

// Decision 0029 S1 (#185): the probe's burst measures wall ÷ CPU time, at least 1.
func TestS1BurstMeasuresASlowdown(t *testing.T) {
	if !CanBurst() {
		t.Skip("no CPU clock per thread")
	}
	v, ok := Burst()
	if !ok || v < 1 || v > 1000 {
		t.Fatalf("%v %v", v, ok)
	}
}
