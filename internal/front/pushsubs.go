package front

import (
	"encoding/json"
)

const subsKey = "auth.pushsubs"

// noteSub remembers which device's session subscribed an endpoint.
func (f *Front) noteSub(device, endpoint string) {
	if f.opt.Store == nil || device == "" || endpoint == "" {
		return
	}
	f.subMu.Lock()
	defer f.subMu.Unlock()
	m := f.loadSubs()
	for _, e := range m[device] {
		if e == endpoint {
			return
		}
	}
	m[device] = append(m[device], endpoint)
	b, _ := json.Marshal(m)
	f.opt.Store.SetKV(subsKey, string(b))
}

func (f *Front) loadSubs() map[string][]string {
	m := map[string][]string{}
	if raw, err := f.opt.Store.GetKV(subsKey); err == nil && raw != "" {
		json.Unmarshal([]byte(raw), &m)
	}
	return m
}

// dropSubs drops the push subscriptions a revoked device's session made.
func (f *Front) dropSubs(device string) {
	if f.opt.Store == nil || f.opt.Push == nil {
		return
	}
	f.subMu.Lock()
	defer f.subMu.Unlock()
	m := f.loadSubs()
	for _, e := range m[device] {
		f.opt.Push.Unsubscribe(e)
	}
	delete(m, device)
	b, _ := json.Marshal(m)
	f.opt.Store.SetKV(subsKey, string(b))
}
