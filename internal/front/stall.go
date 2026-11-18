package front

// SPEC 0.5 V0.181 (#267): a stalled host is an offline host. The host in this
// process (role both) used to be online for the front as long as its stream
// was open, also while its engine's lock was held: every call to it hung for
// the deadline, and fyrd exited after 2 minutes, taking the other hosts, the
// browsers' streams and the peer links with it. Now a stall makes the front
// treat its own host as it treats a remote one that went away: offline,
// served from the cache, commands refused at once. It is back when it answers.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
)

// stallProbeEvery is how often the front asks a stalled host for its state
// until it answers; stallProbeTimeout bounds one ask (the host's own deadline
// answers 503 after 10 s, V0.147).
var (
	stallProbeEvery   = 5 * time.Second
	stallProbeTimeout = 15 * time.Second
)

// stalledAnswer: resp is the host's 503 past a deadline (api.StalledCode).
// The body is read and put back.
func stalledAnswer(resp *http.Response) bool {
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Body == nil {
		return false
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	return err == nil && bytes.Contains(head, []byte(`"code":"`+api.StalledCode+`"`))
}

// suspect: the host answered a call with the deadline's 503, or sent a stall
// frame. The front asks it for its state; while it does not answer it is
// offline (one ask at a time per host).
func (f *Front) suspect(l *link, why string) {
	f.mu.Lock()
	ctx := l.ctx
	f.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || !l.probing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer l.probing.Store(false)
		f.probeStalled(ctx, l, why)
	}()
}

// probeStalled asks l for its state until it answers: the first failed ask
// marks it offline (stalled), the answer brings it back with that state.
func (f *Front) probeStalled(ctx context.Context, l *link, why string) {
	marked := false
	for {
		var st map[string]any
		gctx, cancel := context.WithTimeout(ctx, stallProbeTimeout)
		err := f.get(gctx, l, "/api/state", &st)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if marked {
				f.opt.Logf("front: host %s answers again after its stall", l.name())
				f.load(l, st)
			}
			return
		}
		if !marked {
			marked = true
			f.setOffline(l, fmt.Errorf("stalled: %s (%v)", why, err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(stallProbeEvery):
		}
	}
}

// stalledErr: a link error set by probeStalled.
func stalledErr(e string) bool { return strings.HasPrefix(e, "stalled: ") }
