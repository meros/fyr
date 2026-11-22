package engine

import (
	"fmt"
	"slices"

	"github.com/meros/fyr/internal/slots"
)

// hostName is how the heavy-job answers name this host (V0.163).
func (e *Engine) hostName() string {
	if e.HostName != "" {
		return e.HostName
	}
	return "this host"
}

// jobShare is a job's share of the host in words (V0.163): CPU and memory
// apart, or one share on a host that counts jobs.
func jobShare(s slots.Snapshot, owner string) string {
	for _, h := range append(s.Held, s.Waiting...) {
		if h.Owner == owner {
			if h.Need != nil {
				return fmt.Sprintf("≈ %.0f %% CPU, %.0f %% memory", h.CPUPct, h.MemPct)
			}
			return fmt.Sprintf("≈ %.0f %%", h.Pct)
		}
	}
	return "its share"
}

// freeShare is what the heavy-job budget has left, in words.
func freeShare(s slots.Snapshot) string {
	if s.CPUFreePct != nil && s.MemFreePct != nil {
		return fmt.Sprintf("%.0f %% CPU and %.0f %% memory", *s.CPUFreePct, *s.MemFreePct)
	}
	if s.UsedPct != nil {
		return fmt.Sprintf("%.0f %%", max(0, 100-*s.UsedPct))
	}
	return "room"
}

func article(kind string) string {
	if kind != "" && slices.Contains([]byte("aeiou"), kind[0]) {
		return "an " + kind
	}
	return "a " + kind
}
