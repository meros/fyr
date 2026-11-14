package engine

import (
	"encoding/json"
	"fmt"

	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.38: what the host sends is lean. A tool output over the cap is
// cut to its head, with a line that says how much is left; the event gets
// "cut": true, and GET /api/threads/{id}/events/{seq} returns it whole. The
// store keeps everything. Subagent output (shown folded) gets a smaller cap.
const (
	leanMain = 16 * 1024
	leanSub  = 4 * 1024
)

func cutText(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	head := s[:max]
	// do not split a UTF-8 rune
	for len(head) > 0 && head[len(head)-1]&0xC0 == 0x80 {
		head = head[:len(head)-1]
	}
	if len(head) > 0 && head[len(head)-1] >= 0xC0 {
		head = head[:len(head)-1]
	}
	return head + fmt.Sprintf("\n… [%d KB more; open the full output]", (len(s)-len(head)+1023)/1024), true
}

// Lean returns ev as it is sent: tool outputs cut, the original untouched.
func Lean(ev store.Event) store.Event {
	if ev.Type != "msg" || len(ev.Data) <= leanSub {
		return ev
	}
	var d map[string]any
	if json.Unmarshal(ev.Data, &d) != nil {
		return ev
	}
	max := leanMain
	if p, _ := d["parent_tool_use_id"].(string); p != "" {
		max = leanSub
	}
	msg, _ := d["message"].(map[string]any)
	blocks, _ := msg["content"].([]any)
	cut := dropImages(d)
	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		if bm == nil || bm["type"] != "tool_result" {
			continue
		}
		switch c := bm["content"].(type) {
		case string:
			if s, ok := cutText(c, max); ok {
				bm["content"], cut = s, true
			}
		case []any:
			for _, x := range c {
				xm, _ := x.(map[string]any)
				if t, _ := xm["text"].(string); xm != nil && xm["type"] == "text" {
					if s, ok := cutText(t, max); ok {
						xm["text"], cut = s, true
					}
				}
			}
		}
	}
	if !cut {
		return ev
	}
	d["cut"] = true
	b, err := json.Marshal(d)
	if err != nil {
		return ev
	}
	out := ev
	out.Data = b
	return out
}

// LeanAll applies Lean to a list.
func LeanAll(evs []store.Event) []store.Event {
	out := make([]store.Event, len(evs))
	for i, ev := range evs {
		out[i] = Lean(ev)
	}
	return out
}

// dropImages leaves out base64 image data anywhere in a message (V0.38): the
// conversation does not draw it, and the full event keeps it. It reports
// whether it left anything out.
func dropImages(x any) bool {
	cut := false
	switch v := x.(type) {
	case map[string]any:
		if v["type"] == "image" {
			if src, _ := v["source"].(map[string]any); src != nil {
				if data, _ := src["data"].(string); data != "" {
					src["data"], src["omitted"], src["bytes"] = "", true, len(data)*3/4
					return true
				}
			}
		}
		for _, c := range v {
			if dropImages(c) {
				cut = true
			}
		}
	case []any:
		for _, c := range v {
			if dropImages(c) {
				cut = true
			}
		}
	}
	return cut
}
