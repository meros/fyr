package httpguard

import (
	"testing"
	"time"
)

func TestBackoffDoublesToMaxAndResets(t *testing.T) {
	b := Backoff{Min: time.Second, Max: 5 * time.Second}
	want := []time.Duration{1, 2, 4, 5, 5}
	for i, w := range want {
		if got := b.Next(); got != w*time.Second {
			t.Fatalf("step %d: %v, want %v", i, got, w*time.Second)
		}
	}
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Fatalf("after Reset: %v", got)
	}
}

func TestBackoffJitterStaysInBand(t *testing.T) {
	b := Backoff{Min: 100 * time.Millisecond, Max: time.Second, Jitter: true}
	step := b.Min
	for i := 0; i < 50; i++ {
		got := b.Next()
		if got < step/2 || got >= step/2+step {
			t.Fatalf("%v outside the band of %v", got, step)
		}
		step = min(step*2, b.Max)
	}
}
