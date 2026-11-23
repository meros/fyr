package httpguard

import (
	"math/rand"
	"time"
)

// Backoff is the wait between tries to reach something that is down: Min at
// first, doubled by each Next up to Max, back to Min after Reset. With Jitter
// each wait is spread over half to one and a half times the step, so that
// peers that lost a link together do not all retry together.
// The zero Max means no cap. A Backoff is for one loop and not safe for
// concurrent use.
type Backoff struct {
	Min, Max time.Duration
	Jitter   bool

	cur time.Duration
}

// Next is the wait for this try and steps the next one up.
func (b *Backoff) Next() time.Duration {
	if b.cur <= 0 {
		b.cur = b.Min
	}
	d := b.cur
	b.cur *= 2
	if b.Max > 0 && b.cur > b.Max {
		b.cur = b.Max
	}
	if b.Jitter && d > 0 {
		d = d/2 + time.Duration(rand.Int63n(int64(d)))
	}
	return d
}

// Reset starts again from Min, after a try that worked.
func (b *Backoff) Reset() { b.cur = 0 }
