// Package notify decides which attention changes become a push and when
// (SPEC 0.5 V0.20), and tracks which threads the user is watching.
package notify

import (
	"sync"
	"time"
)

type Plan int

const (
	Never Plan = iota
	Now
	Later // after Delay, if the thread is still in that state and unwatched
)

// Delay is how long done and blocked wait before they push.
const Delay = 20 * time.Second

// Watch is how long a presence beat keeps a thread watched.
const Watch = 45 * time.Second

// PlanFor maps a transition to a plan. prev is "" for a thread the host has
// not reported before (new, or after a restart): no push for old states.
func PlanFor(prev, next string) Plan {
	if prev == "" || prev == next {
		return Never
	}
	switch next {
	case "asking", "failed":
		return Now
	case "done", "blocked":
		return Later
	}
	return Never
}

// Presence remembers, per device, which thread it shows and when it last
// said so.
type Presence struct {
	mu    sync.Mutex
	shows map[string]beat
	// seen is the newest beat of any device, or the start (Seen): the owner
	// is away AwayAfter later (V0.push-summary-batch).
	seen time.Time
}

type beat struct {
	thread string
	at     time.Time
}

// Beat records that device shows thread (or nothing, with "") at now, and
// returns the thread its last beat named (V0.85).
func (p *Presence) Beat(device, thread string, now time.Time) (prev string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shows == nil {
		p.shows = map[string]beat{}
	}
	prev = p.shows[device].thread
	p.shows[device] = beat{thread, now}
	if now.After(p.seen) {
		p.seen = now
	}
	return prev
}

// Seen counts now as presence without a device: the front's start, so a
// restart is not taken for an owner long gone.
func (p *Presence) Seen(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.After(p.seen) {
		p.seen = now
	}
}

// LastSeen is the newest beat of any device (or Seen).
func (p *Presence) LastSeen() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen
}

// Away reports whether no device has beat for AwayAfter. Every beat counts,
// the one that says the page went hidden too: the owner was there then.
func (p *Presence) Away(now time.Time) bool {
	return now.Sub(p.LastSeen()) >= AwayAfter
}

// Watched reports whether any device showed thread within Watch of now.
func (p *Presence) Watched(thread string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range p.shows {
		if b.thread == thread && now.Sub(b.at) <= Watch {
			return true
		}
	}
	return false
}

// Allowed reports whether a planned push may go out at now: not for a
// thread a device is watching, and not while the thread's project is in its
// quiet hours (quietUntil in the future, SPEC 0.5 V0.46). A thread's push
// refused for quiet hours is held (RouteFor, V0.push-summary-batch); this is
// for a push that is not held (a quota reset).
func Allowed(watched bool, quietUntil *time.Time, now time.Time) bool {
	return RouteFor("", watched, quietUntil, false, now) == Send
}
