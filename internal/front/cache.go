package front

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// The front's cache on disk (SPEC 0.5 V0.54, decision 0016): the last state
// of each host, so a host that is offline when the front starts still shows
// its threads, marked offline and read-only. One file per host, written
// when its state changed (#22): checked every saveEvery, a host's file at
// most every saveGap, and every host's when the front stops. An online host
// whose state did not change is written again after saveIdle, so savedAt
// (the "offline since" after a restart) stays near the truth.

const (
	saveEvery = 2 * time.Second
	saveGap   = 30 * time.Second
	saveIdle  = 10 * time.Minute
)

type cacheFile struct {
	Name     string                    `json:"name"`
	SavedAt  time.Time                 `json:"savedAt"`
	Online   bool                      `json:"online"`
	Since    time.Time                 `json:"since"` // the last time the front had the host online
	Raw      map[string]map[string]any `json:"raw"`
	Projects []map[string]any          `json:"projects"`
	Env      map[string]any            `json:"env"`
	// Settings: the host's last settings reads (V0.127), by path.
	Settings map[string]settingsRead `json:"settings,omitempty"`
}

// key names a host's cache file: its configured name, which stays the same
// across restarts (a both front's local link has a new port each start).
func (l *link) key() string {
	if l.cfg.Name != "" {
		return l.cfg.Name
	}
	return l.base.Host
}

// cachePath is a host's cache file: named by its install id once the front
// knows it (decision 0031 4.3: a rename keeps the cache), else by its name.
func (f *Front) cachePath(l *link) string {
	f.mu.Lock()
	id := f.idOfLocked(l)
	f.mu.Unlock()
	return f.cachePathFor(l, id)
}

func (f *Front) cachePathFor(l *link, id string) string {
	if id == "" {
		id = l.key()
	}
	return filepath.Join(f.opt.CacheDir, url.PathEscape(id)+".json")
}

// legacyCachePath is the file the front wrote before the id named it.
func (f *Front) legacyCachePath(l *link) string {
	return filepath.Join(f.opt.CacheDir, url.PathEscape(l.key())+".json")
}

// restore loads a host's cached state: offline since the front last saw it,
// until the link comes up.
func (f *Front) restore(l *link) {
	b, err := os.ReadFile(f.cachePath(l))
	if err != nil {
		if b, err = os.ReadFile(f.legacyCachePath(l)); err != nil {
			return
		}
	}
	var c cacheFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&c); err != nil {
		f.opt.Logf("front: cache %s: %v", l.key(), err)
		return
	}
	if c.Raw != nil {
		l.raw = c.Raw
	}
	l.projects = c.Projects
	if c.Settings != nil {
		l.settings = c.Settings
	}
	l.setEnv(c.Env)
	l.since = c.Since
	if c.Online {
		l.since = c.SavedAt
	}
	l.online, l.err = false, "not reached since the front started"
	f.opt.Logf("front: host %s from the cache (%d threads, offline since %s)", l.name(), len(l.raw), l.since.Local().Format("15:04"))
}

// saveDirty writes the cache of every host that changed, at most every
// saveGap per host.
func (f *Front) saveDirty() { f.save(time.Now(), false) }

// save writes the cache of each host whose state changed since its last
// write. Unless force, a host written less than saveGap ago waits for a
// later call (it stays dirty).
func (f *Front) save(now time.Time, force bool) {
	if f.opt.CacheDir == "" {
		return
	}
	type job struct {
		l            *link
		path, legacy string
		b            []byte
	}
	var jobs []job
	f.mu.Lock()
	for _, l := range f.all() {
		if !l.dirty || (!force && now.Sub(l.savedAt) < saveGap) {
			continue
		}
		c := cacheFile{Name: l.name(), Online: l.online, Since: l.since,
			Raw: l.raw, Projects: l.projects, Env: l.env, Settings: l.settings}
		b, err := json.Marshal(c)
		if err != nil {
			f.opt.Logf("front: cache %s: %v", l.key(), err)
			l.dirty = false
			continue
		}
		l.dirty = false
		sum := sha256.Sum256(b)
		if sum == l.savedSum && !(l.online && now.Sub(l.savedAt) >= saveIdle) {
			continue // nothing the file holds changed
		}
		c.SavedAt = now
		if b, err = json.Marshal(c); err != nil {
			continue
		}
		l.savedSum, l.savedAt = sum, now
		id := f.idOfLocked(l)
		legacy := ""
		if id != "" {
			legacy = f.legacyCachePath(l) // moved to the id's file
		}
		jobs = append(jobs, job{l, f.cachePathFor(l, id), legacy, b})
	}
	f.mu.Unlock()
	if len(jobs) == 0 {
		return
	}
	failed := func(j job, err error) {
		f.opt.Logf("front: cache: %v", err)
		f.mu.Lock()
		j.l.dirty, j.l.savedSum = true, [sha256.Size]byte{} // written again on a later call
		f.mu.Unlock()
	}
	if err := os.MkdirAll(f.opt.CacheDir, 0o700); err != nil {
		for _, j := range jobs {
			failed(j, err)
		}
		return
	}
	for _, j := range jobs {
		tmp := j.path + ".tmp"
		if err := os.WriteFile(tmp, j.b, 0o600); err != nil {
			failed(j, err)
			continue
		}
		if err := os.Rename(tmp, j.path); err != nil {
			failed(j, err)
		} else if j.legacy != "" && j.legacy != j.path {
			os.Remove(j.legacy)
		}
	}
}
