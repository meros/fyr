package front

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/meros/fyr/internal/api"
)

// KV is where a front keeps the owner's settings that belong to no host
// (V0.121): the database it keeps device data in.
type KV interface {
	GetKV(k string) (string, error)
	SetKV(k, v string) error
}

const (
	// KindProjectOrder sets the owner's project order (V0.121). It is the
	// front's own command: no host has the kind, so no agent tool or MCP
	// call can send it; the browser guard asks for X-Fyr.
	KindProjectOrder = "front.projectOrder"
	projectOrderKey  = "front.projectOrder"
	maxOrderIDs      = 5000
	maxReceipts      = 256
)

// loadOrder reads the stored order.
func (f *Front) loadOrder() {
	if f.opt.Store == nil {
		return
	}
	raw, err := f.opt.Store.GetKV(projectOrderKey)
	if err != nil || raw == "" {
		return
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		f.opt.Logf("front: project order: %v", err)
		return
	}
	f.order = ids
}

// frontCommand runs one of the front's own commands (V0.121, V0.124). A
// commandId that already ran answers its first reply and changes nothing.
func (f *Front) frontCommand(w http.ResponseWriter, commandID string, run func() (int, api.Reply)) {
	if commandID != "" {
		f.mu.Lock()
		rep, ok := f.receipts[commandID]
		f.mu.Unlock()
		if ok {
			api.WriteJSON(w, 200, rep)
			return
		}
	}
	code, rep := run()
	if commandID != "" && code < 500 {
		f.mu.Lock()
		if f.receipts == nil {
			f.receipts = map[string]api.Reply{}
		}
		if len(f.receipts) >= maxReceipts {
			clear(f.receipts)
		}
		f.receipts[commandID] = rep
		f.mu.Unlock()
	}
	api.WriteJSON(w, code, rep)
}

// orderCommand runs front.projectOrder {ids}: one list of project ids across
// all hosts, the owner's; blanks and repeats are dropped.
func (f *Front) orderCommand(w http.ResponseWriter, commandID string, args map[string]any) {
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		raw, ok := args["ids"].([]any)
		if !ok {
			return 400, api.Reply{Error: "ids must be a list of project ids", Status: 400}
		}
		if len(raw) > maxOrderIDs {
			return 400, api.Reply{Error: fmt.Sprintf("at most %d projects", maxOrderIDs), Status: 400}
		}
		ids := make([]string, 0, len(raw))
		seen := map[string]bool{}
		for _, x := range raw {
			s, _ := x.(string)
			s = strings.TrimSpace(s)
			if s == "" || len(s) > 200 || seen[s] {
				continue
			}
			seen[s] = true
			ids = append(ids, s)
		}
		if f.opt.Store == nil {
			return 500, api.Reply{Error: "This front keeps no settings", Status: 500}
		}
		b, _ := json.Marshal(ids)
		if err := f.opt.Store.SetKV(projectOrderKey, string(b)); err != nil {
			return 500, api.Reply{Error: "Could not save the project order: " + err.Error(), Status: 500}
		}
		f.mu.Lock()
		f.order = ids
		f.mu.Unlock()
		ev, _ := json.Marshal(map[string]any{"ids": ids})
		f.publishFront("projectOrder", ev)
		return 200, api.Reply{OK: true, Status: 200, Result: map[string]any{"ids": ids}}
	})
}
