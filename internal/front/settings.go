package front

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/meros/fyr/internal/api"
)

// SPEC V0.126 (3): a host's settings as the owner would see them connected
// to it directly. The front reads them from the host the path names
// (/api/h/<id>/…, decision 0031) through its host client, keeps the last
// answer per host and path, and serves that answer while the host is offline,
// with X-Fyr-Read-At: when it was read. Changes are that host's commands and
// are refused while the host is offline, like every command.

// settingsPaths are the reads of Settings → Providers and Automation.
var settingsPaths = []string{"/api/providers", "/api/providers/found", "/api/systemAgents", "/api/autoDeploy", "/api/autoMerge", "/api/updates"}

// settingsRead is the last answer of one read.
type settingsRead struct {
	Body json.RawMessage `json:"body"`
	At   time.Time       `json:"at"`
}

// maxSettingsRead bounds one answer the front keeps (a provider list with
// a large catalog is a few hundred kB).
const maxSettingsRead = 8 << 20

// settingsFor answers one settings read of one host: from the host, kept, or
// the kept answer while the host is offline (decision 0031 2.2 keeps V0.127's
// rule for the prefix too). A host that cannot be reached and has no kept
// answer is 503 (host-offline on the prefix's route, as the old routes said).
func (f *Front) settingsFor(w http.ResponseWriter, r *http.Request, l *link, path string, offline func()) {
	f.mu.Lock()
	on := l.online
	last, kept := l.settings[path]
	f.mu.Unlock()
	// What was last read, marked with when; for a host that is offline, or
	// that stopped answering before the front noticed.
	stale := func() bool {
		if !kept {
			return false
		}
		w.Header().Set("X-Fyr-Read-At", last.At.UTC().Format(time.RFC3339))
		api.WriteJSON(w, 200, last.Body)
		return true
	}
	if !on {
		if !stale() {
			offline()
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hostReadTimeout)
	defer cancel()
	q := r.URL.Query()
	q.Del("host")
	u := l.base.String() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	// V0.host-ids (decision 0031 2.7): the read is the browser's, so the host
	// hears who asks, as for every other forwarded call; not the install's own.
	setActor(req, principalOf(r), f.opt.ID, originOf(r))
	resp, err := l.client.Do(req)
	if err != nil {
		if stale() {
			return
		}
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": l.name() + ": " + err.Error()})
		return
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSettingsRead+1))
	if err != nil {
		if stale() {
			return
		}
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": l.name() + ": " + err.Error()})
		return
	}
	if resp.StatusCode == 200 && len(b) <= maxSettingsRead && json.Valid(b) {
		f.mu.Lock()
		l.settings[path] = settingsRead{Body: b, At: time.Now().UTC()}
		l.dirty = true
		f.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(b)
}
