package deploy

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// V0.host-updates (#154): each host's update state as the fronts show it in
// Settings → Hosts: what it runs, the release it wants, how far behind it
// is, how its last update ended, and a release it holds back.

// HostUpdate is one host's update state, sent in its state and as the
// stream frame "update".
type HostUpdate struct {
	// Running is the commit this host runs (a release's, or a local build's
	// checkout HEAD); Build is its build id.
	Running string `json:"running,omitempty"`
	Build   string `json:"build,omitempty"`
	// Wanted is where the channel (`deployable` on main) pointed when this host last read it.
	Wanted string `json:"wanted,omitempty"`
	// Behind is how many commits Wanted is ahead of Running; -1 when not
	// known (not read yet, or GitHub could not say).
	Behind int `json:"behind"`
	// Last is the newest update attempt, without its phases.
	Last *Update `json:"last,omitempty"`
	// Bad is the release this host holds back because it rolled back or
	// failed its smoke test here; nil when none is held back.
	Bad *BadMark `json:"bad,omitempty"`
	// Paused: a local build runs, and auto-update waits for it (#159).
	Paused bool `json:"paused,omitempty"`
	// Version is the running build's version (vX.Y.Z), empty for a build of
	// main; Channel is the update channel the host follows: main, stable or
	// a pinned version (V0.versioned-releases).
	Version string `json:"version,omitempty"`
	Channel string `json:"channel,omitempty"`
	// Due is when the release that waits out its quiet period installs by
	// itself (V0.update-now-fresh); zero when none waits.
	Due time.Time `json:"due,omitzero"`
}

// BadMark is a release marked bad on this host.
type BadMark struct {
	Commit string    `json:"commit"`
	Why    string    `json:"why"`
	At     time.Time `json:"at,omitzero"`
}

const (
	badKey    = "update_bad"
	behindKey = "update_behind"
)

// changed tells OnChange that the summary may have moved.
func (d *Deployer) changed() {
	if d.OnChange != nil {
		d.OnChange()
	}
}

// markBad keeps a release that rolled back or failed its smoke test here
// from being tried again until deployable moves past it (#154).
func (d *Deployer) markBad(commit, why string) {
	b, _ := json.Marshal(map[string]string{"commit": commit, "why": why, "at": time.Now().UTC().Format(time.RFC3339)})
	d.DB.SetKV(badKey, string(b))
	d.changed()
}

// ClearBad forgets the bad mark (the owner's Update now · retry, or a
// release that went live); true when there was one.
func (d *Deployer) ClearBad() bool {
	if c, _ := d.Bad(); c == "" {
		return false
	}
	d.DB.SetKV(badKey, "")
	d.changed()
	return true
}

func (d *Deployer) badMark() *BadMark {
	raw, _ := d.DB.GetKV(badKey)
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil || m["commit"] == "" {
		return nil
	}
	at, _ := time.Parse(time.RFC3339, m["at"])
	return &BadMark{Commit: m["commit"], Why: m["why"], At: at}
}

// Bad is the release commit that last rolled back here, and why.
func (d *Deployer) Bad() (commit, why string) {
	if m := d.badMark(); m != nil {
		return m.Commit, m.Why
	}
	return "", ""
}

// running is the commit this host runs: the recorded one, else the commit
// of its build id when that id is a hex prefix (a release's).
func (d *Deployer) running() string {
	if c, _ := d.DB.GetKV("live_commit"); c != "" {
		return c
	}
	id, _, _ := strings.Cut(d.live(), "-")
	if len(id) >= 7 && isHex(id) {
		return id
	}
	return ""
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != ""
}

func sameCommit(a, b string) bool {
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

type behind struct {
	From string `json:"from"`
	To   string `json:"to"`
	N    int    `json:"n"`
}

// Summary is this host's update state (V0.host-updates).
func (d *Deployer) Summary() HostUpdate {
	s := HostUpdate{Running: d.running(), Build: d.live(), Behind: -1}
	s.Wanted, _ = d.DB.GetKV(seenKey)
	switch {
	case s.Wanted == "":
	case sameCommit(s.Running, s.Wanted):
		s.Behind = 0
	default:
		raw, _ := d.DB.GetKV(behindKey)
		var b behind
		if json.Unmarshal([]byte(raw), &b) == nil && b.From == s.Running && b.To == s.Wanted {
			s.Behind = b.N
		}
	}
	if u := d.Updates(); len(u) > 0 {
		last := u[0]
		last.Phases, last.Logs = nil, nil
		s.Last = &last
	}
	// The mark shows while it holds a release back: deployable still
	// points at it. A newer release ends it by itself.
	if m := d.badMark(); m != nil && (s.Wanted == "" || sameCommit(m.Commit, s.Wanted)) {
		s.Bad = m
	}
	s.Paused = d.PausedBy() != nil
	if raw, _ := d.DB.GetKV(wantedKey); raw != "" {
		var wt wanted
		if json.Unmarshal([]byte(raw), &wt) == nil && wt.SHA != "" && !sameCommit(wt.SHA, s.Running) {
			s.Due = wt.Due
		}
	}
	s.Version = d.LiveVersion
	if d.Releases != nil {
		s.Channel = d.Releases.ChannelName()
	}
	return s
}

// noteBehind counts, once per pair, the commits between what runs and what
// deployable points at (one GitHub compare per move of either).
func (d *Deployer) noteBehind(ctx context.Context, wanted string) {
	run := d.running()
	if d.Releases == nil || run == "" || wanted == "" || sameCommit(run, wanted) {
		return
	}
	raw, _ := d.DB.GetKV(behindKey)
	var b behind
	if json.Unmarshal([]byte(raw), &b) == nil && b.From == run && b.To == wanted {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	n, ok := d.Releases.Behind(cctx, run, wanted)
	cancel()
	if !ok {
		return
	}
	nb, _ := json.Marshal(behind{From: run, To: wanted, N: n})
	d.DB.SetKV(behindKey, string(nb))
	d.changed()
}

// Behind is how many commits wanted is ahead of running, by GitHub's
// compare; false when it cannot say.
func (r *Releases) Behind(ctx context.Context, running, wanted string) (int, bool) {
	if running == "" || wanted == "" || strings.HasPrefix(running, "-") || strings.HasPrefix(wanted, "-") {
		return 0, false
	}
	c, err := r.compare(ctx, running, wanted)
	if err != nil {
		return 0, false
	}
	return c.AheadBy, c.AheadBy >= 0
}
