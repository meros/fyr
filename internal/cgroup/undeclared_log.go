package cgroup

import (
	"encoding/json"
	"sync"

	"github.com/meros/fyr/internal/kvstore"
)

// Episodes keeps the record of each ended episode, the newest 100, in the
// store's KV table (V0.164 4.5).
type Episodes struct {
	KV kvstore.Table

	mu sync.Mutex
}

const (
	episodesKey = "undeclared"
	maxEpisodes = 100
)

// Add records an ended episode.
func (l *Episodes) Add(e Episode) {
	if l == nil || l.KV == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	all := l.list()
	all = append([]Episode{e}, all...)
	if len(all) > maxEpisodes {
		all = all[:maxEpisodes]
	}
	raw, _ := json.Marshal(all)
	l.KV.SetKV(episodesKey, string(raw))
}

// List is the newest first.
func (l *Episodes) List() []Episode {
	if l == nil || l.KV == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.list()
}

func (l *Episodes) list() []Episode {
	var all []Episode
	if raw, _ := l.KV.GetKV(episodesKey); raw != "" {
		json.Unmarshal([]byte(raw), &all)
	}
	return all
}
