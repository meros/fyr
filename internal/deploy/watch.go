package deploy

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/release"
)

// #153: Fyr follows the tag `deployable`, which CI moves to each commit it
// tested and published. A move starts a quiet period; when it ends with no
// further move, the host installs the newest release once. It runs on
// every host on its own (decision 0027).

// Watcher polls deployable and starts the release install.
type Watcher struct {
	D *Deployer
	// Every is the poll interval; 0 is a minute.
	Every time.Duration
	// Quiet is how long deployable must stay put; nil or 0 is 10 minutes.
	Quiet func() time.Duration
	// On reports the owner's Auto-update setting; nil is on.
	On func() bool
	// Start runs the install of sha (the same path as an owner's or an
	// agent's ask), in the background.
	Start func(sha string)
	Now   func() time.Time

	kick chan struct{}
	errs int
}

// wanted is the release waiting for its quiet period, kept across restarts.
type wanted struct {
	SHA  string    `json:"sha"`
	Seen time.Time `json:"seen"`
	Back bool      `json:"back,omitempty"`
}

const (
	seenKey   = "update_seen"
	wantedKey = "update_wanted"
)

// Kick checks deployable now (after Fyr merged a PR of its own repo).
func (w *Watcher) Kick() {
	if w.kick == nil {
		return
	}
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Run polls until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	every := w.Every
	if every == 0 {
		every = time.Minute
	}
	w.kick = make(chan struct{}, 1)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		w.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.kick:
		}
	}
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Step reads deployable once and starts an install when one is due.
func (w *Watcher) Step(ctx context.Context) {
	d := w.D
	if d == nil || d.Releases == nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	sha, err := d.Releases.Deployable(rctx)
	cancel()
	if err != nil {
		if w.errs++; w.errs == 1 || w.errs%60 == 0 { // once, then hourly
			log.Printf("update: %v", err)
		}
		return
	}
	w.errs = 0
	seen, _ := d.DB.GetKV(seenKey)
	if sha != seen {
		d.DB.SetKV(seenKey, sha)
		wt := wanted{SHA: sha, Seen: w.now()}
		// A move to an ancestor of what CI published before is a person's
		// act (to stop a bad release everywhere): follow it.
		if seen != "" {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			wt.Back = d.Releases.Contains(cctx, seen, sha)
			cancel()
		}
		b, _ := json.Marshal(wt)
		d.DB.SetKV(wantedKey, string(b))
	}
	raw, _ := d.DB.GetKV(wantedKey)
	var wt wanted
	if raw == "" || json.Unmarshal([]byte(raw), &wt) != nil || wt.SHA == "" {
		return
	}
	quiet := 10 * time.Minute
	if w.Quiet != nil && w.Quiet() > 0 {
		quiet = w.Quiet()
	}
	if w.now().Sub(wt.Seen) < quiet {
		return
	}
	if w.On != nil && !w.On() {
		return // the update line offers Update now (#153)
	}
	if d.Busy() {
		return
	}
	drop := func(why string) {
		d.DB.SetKV(wantedKey, "")
		if why != "" {
			log.Printf("update: %s: %s", release.Short(wt.SHA), why)
		}
	}
	live, _ := d.DB.GetKV("live_commit")
	liveID, _, _ := strings.Cut(d.live(), "-")
	if strings.HasPrefix(wt.SHA, liveID) && liveID != "" || live == wt.SHA {
		drop("")
		return
	}
	if bad, why := d.Bad(); bad == wt.SHA {
		drop("rolled back here before (" + why + "); waiting for a newer release")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if p := d.PausedBy(); p != nil {
		// #159: a local build runs; it resumes once a release holds it.
		if p.Commit == "" || !d.Releases.Contains(cctx, wt.SHA, p.Commit) {
			return
		}
		log.Printf("update: release %s contains the local build %s; auto-update resumes", release.Short(wt.SHA), p.Build)
	}
	if !wt.Back && live != "" && d.Releases.Contains(cctx, live, wt.SHA) {
		drop("the running build already contains it")
		return
	}
	drop("")
	log.Printf("update: installing release %s", release.Short(wt.SHA))
	if w.Start != nil {
		w.Start(wt.SHA)
	}
}
