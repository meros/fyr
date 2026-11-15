package front

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// The front's cache on disk (SPEC 0.5 V0.54, decision 0016): the last state
// of each host, so a host that is offline when the front starts still shows
// its threads, marked offline and read-only. One file per host, written at
// most every saveEvery and when the front stops.

const saveEvery = 2 * time.Second

type cacheFile struct {
	Name     string                    `json:"name"`
	SavedAt  time.Time                 `json:"savedAt"`
	Online   bool                      `json:"online"`
	Since    time.Time                 `json:"since"` // the last time the front had the host online
	Raw      map[string]map[string]any `json:"raw"`
	Pub      map[string]map[string]any `json:"pub"`
	Projects []map[string]any          `json:"projects"`
	Env      map[string]any            `json:"env"`
}

// key names a host's cache file: its configured name, which stays the same
// across restarts (a both front's local link has a new port each start).
func (l *link) key() string {
	if l.cfg.Name != "" {
		return l.cfg.Name
	}
	return l.base.Host
}

func (f *Front) cachePath(l *link) string {
	return filepath.Join(f.opt.CacheDir, url.PathEscape(l.key())+".json")
}

// restore loads a host's cached state: offline since the front last saw it,
// until the link comes up.
func (f *Front) restore(l *link) {
	b, err := os.ReadFile(f.cachePath(l))
	if err != nil {
		return
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
	if c.Pub != nil {
		l.pub = c.Pub
	}
	l.projects = c.Projects
	l.setEnv(c.Env)
	l.since = c.Since
	if c.Online {
		l.since = c.SavedAt
	}
	l.online, l.err = false, "not reached since the front started"
	for id := range l.raw {
		f.publishLocked(l, id)
	}
	f.opt.Logf("front: host %s from the cache (%d threads, offline since %s)", l.name(), len(l.raw), l.since.Local().Format("15:04"))
}

// saveDirty writes the cache of every host that changed.
func (f *Front) saveDirty() {
	if f.opt.CacheDir == "" {
		return
	}
	type job struct {
		path string
		b    []byte
	}
	var jobs []job
	f.mu.Lock()
	for _, l := range f.links {
		if !l.dirty {
			continue
		}
		l.dirty = false
		b, err := json.Marshal(cacheFile{Name: l.name(), SavedAt: time.Now(), Online: l.online, Since: l.since,
			Raw: l.raw, Pub: l.pub, Projects: l.projects, Env: l.env})
		if err != nil {
			f.opt.Logf("front: cache %s: %v", l.key(), err)
			continue
		}
		jobs = append(jobs, job{f.cachePath(l), b})
	}
	f.mu.Unlock()
	if len(jobs) == 0 {
		return
	}
	if err := os.MkdirAll(f.opt.CacheDir, 0o700); err != nil {
		f.opt.Logf("front: cache: %v", err)
		return
	}
	for _, j := range jobs {
		tmp := j.path + ".tmp"
		if err := os.WriteFile(tmp, j.b, 0o600); err != nil {
			f.opt.Logf("front: cache: %v", err)
			continue
		}
		if err := os.Rename(tmp, j.path); err != nil {
			f.opt.Logf("front: cache: %v", err)
		}
	}
}
