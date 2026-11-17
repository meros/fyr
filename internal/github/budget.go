package github

// V0.178: one budget for a host's GitHub calls. The account's limits are
// shared by every agent, every host and the owner, so fyrd's polls give
// way: they stop while the account is low or rate limited, and they keep
// to a steady pace; the updater, merges, re-runs and the owner's PR tab
// (Act) always run.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Priority orders the callers: Act before Poll.
type Priority int

const (
	Poll Priority = iota // the poller: PRs, deploy watches, annotations
	Act                  // the updater, merges, re-runs, the PR tab
)

// ErrBudget is a poll the budget held back; nothing was sent.
var ErrBudget = errors.New("github budget: poll deferred")

const (
	// DefaultReserve is the remaining calls (of either limit) at which
	// polls stop until the reset: the rest is for acts and agents.
	DefaultReserve = 500
	// DefaultPollsPerMinute is the pace of poll REST calls.
	DefaultPollsPerMinute = 30
	// limitedFor is how long polls wait after GitHub refused a call for its
	// limit when the reset is not known (secondary limits name none).
	limitedFor = 5 * time.Minute
)

// Pause is what the budget tells the owner while polls wait.
type Pause struct {
	Resource  string    `json:"resource"` // core (REST) or graphql
	Until     time.Time `json:"until"`
	Remaining int       `json:"remaining"`
	Why       string    `json:"why"`
	Deferred  int       `json:"deferred"` // polls held back in this pause
}

type limitState struct {
	remaining int // -1 unknown
	reset     time.Time
	limited   time.Time // refused until
}

// Budget holds back polls. The zero value works.
type Budget struct {
	Reserve        int
	PollsPerMinute int
	Now            func() time.Time

	mu     sync.Mutex
	res    map[string]*limitState
	tokens float64
	filled time.Time
	pause  *Pause
}

func (b *Budget) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Budget) reserve() int {
	if b.Reserve > 0 {
		return b.Reserve
	}
	return DefaultReserve
}

func (b *Budget) perMinute() int {
	if b.PollsPerMinute > 0 {
		return b.PollsPerMinute
	}
	return DefaultPollsPerMinute
}

func (b *Budget) state(resource string) *limitState {
	if b.res == nil {
		b.res = map[string]*limitState{}
	}
	s := b.res[resource]
	if s == nil {
		s = &limitState{remaining: -1}
		b.res[resource] = s
	}
	return s
}

// resource is the limit a call counts against: graphql for gh pr and gh
// api graphql, core for the rest.
func resource(args []string) string {
	k, _ := Kind(args)
	if strings.HasPrefix(k, "graphql") {
		return "graphql"
	}
	return "core"
}

// SetRate takes a read of rate_limit.
func (b *Budget) SetRate(r Rate) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.state("core")
	c.remaining, c.reset = r.Remaining, r.Reset
	if r.Remaining > 0 && c.limited.After(b.now()) && r.Remaining > b.reserve() {
		c.limited = time.Time{} // the limit reset sooner than guessed
	}
	g := b.state("graphql")
	g.remaining = r.GraphQL
	if !r.Reset.IsZero() && g.reset.IsZero() {
		g.reset = r.Reset
	}
}

// Runner returns gh with the budget at priority p.
func (b *Budget) Runner(gh Runner, p Priority) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		res := resource(args)
		if p == Poll {
			if why := b.hold(res); why != nil {
				return nil, why
			}
		}
		out, err := gh(ctx, args...)
		b.observe(res, out, err)
		return out, err
	}
}

// hold reports why a poll must wait now, or takes its token.
func (b *Budget) hold(res string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	s := b.state(res)
	switch {
	case s.limited.After(now):
		return b.deferLocked(res, s.limited, s.remaining, "GitHub refused calls for the rate limit")
	case s.remaining >= 0 && s.remaining <= b.reserve() && s.reset.After(now):
		return b.deferLocked(res, s.reset, s.remaining, fmt.Sprintf("the account has %d calls left; Fyr keeps %d for merges, the updater and agents", s.remaining, b.reserve()))
	}
	if b.pause != nil && !b.pause.Until.After(now) {
		log.Printf("github: polls run again (%d were held back)", b.pause.Deferred)
		b.pause = nil
	}
	if res != "core" {
		return nil
	}
	per := float64(b.perMinute())
	if b.filled.IsZero() {
		b.tokens, b.filled = per, now
	}
	b.tokens += now.Sub(b.filled).Minutes() * per
	if b.tokens > per {
		b.tokens = per
	}
	b.filled = now
	if b.tokens < 1 {
		return ErrBudget // the pace: no notice, the next poll catches up
	}
	b.tokens--
	return nil
}

func (b *Budget) deferLocked(res string, until time.Time, remaining int, why string) error {
	if b.pause == nil || b.pause.Resource != res || !b.pause.Until.Equal(until) {
		log.Printf("github: polls wait until %s: %s", until.Local().Format("15:04"), why)
		b.pause = &Pause{Resource: res, Until: until, Remaining: remaining, Why: why}
	}
	b.pause.Deferred++
	return ErrBudget
}

// observe reads what a call's answer says of the limit: the rate headers
// of gh api -i, and GitHub's refusal for the limit.
func (b *Budget) observe(res string, out []byte, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if _, head, _ := splitHTTP(out); head != "" {
		if r := header(head, "x-ratelimit-resource"); r != "" {
			res = r
		}
		s := b.state(res)
		if n, e := strconv.Atoi(header(head, "x-ratelimit-remaining")); e == nil {
			s.remaining = n
		}
		if n, e := strconv.ParseInt(header(head, "x-ratelimit-reset"), 10, 64); e == nil {
			s.reset = time.Unix(n, 0)
		}
	}
	if err == nil {
		return
	}
	msg := err.Error() + string(out)
	if !strings.Contains(msg, "rate limit") {
		return
	}
	s := b.state(res)
	until := now.Add(limitedFor)
	if !strings.Contains(msg, "secondary rate limit") && s.reset.After(now) {
		until = s.reset
		s.remaining = 0
	}
	if until.After(s.limited) {
		s.limited = until
	}
}

// Paused is the current pause, nil while polls run.
func (b *Budget) Paused() *Pause {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pause == nil || !b.pause.Until.After(b.now()) {
		return nil
	}
	p := *b.pause
	return &p
}
