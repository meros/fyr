package slots

import (
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

// V0.209: TryAcquire admits when there is room and never queues; whoever waits goes first. Take gives a share back
// without handing it on, until Fill.
func TestV209TryAcquireAndTake(t *testing.T) {
	p := New(1)
	if !p.TryAcquire("a", resource.Test) || !p.TryAcquire("a", resource.Test) {
		t.Fatal("not admitted")
	}
	if p.TryAcquire("b", resource.Test) || len(p.Snapshot().Waiting) != 0 {
		t.Fatal("over the budget, or queued")
	}
	p.Acquire("c", resource.Test)
	var granted []string
	p.OnGrant = func(o, _ string) { granted = append(granted, o) }
	if !p.Take("a") || p.Take("a") {
		t.Fatal("take")
	}
	if len(granted) != 0 {
		t.Fatal("Take handed it on")
	}
	if p.TryAcquire("b", resource.Test) {
		t.Fatal("jumped the queue")
	}
	p.Fill()
	if len(granted) != 1 || granted[0] != "c" {
		t.Fatalf("%v", granted)
	}
}
