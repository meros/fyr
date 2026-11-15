package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Serve writes a hub subscription as the SSE stream of the API (V0.2,
// V0.38): hello, then the frames, threads as merge patches against what this
// connection already has. The host and the front (V0.51) both use it.
func Serve(w http.ResponseWriter, r *http.Request, ch <-chan Frame, buildID string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	hello, _ := json.Marshal(map[string]any{"now": time.Now().UTC(), "buildId": buildID})
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	// V0.38: after the first whole thread, this connection gets merge patches
	// against what it already has; a thread goes out at most every 100 ms,
	// the newest version winning.
	sent := map[string]map[string]any{}
	lastAt := map[string]time.Time{}
	pending := map[string][]byte{}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	sendThread := func(data []byte) {
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber() // revs are nanosecond counts above 2^53
		if dec.Decode(&m) != nil {
			fmt.Fprintf(w, "event: thread\ndata: %s\n\n", data)
			return
		}
		id, _ := m["id"].(string)
		lastAt[id] = time.Now()
		if prev, ok := sent[id]; ok {
			out, err := json.Marshal(map[string]any{"id": id, "rev": m["rev"], "base": prev["rev"], "patch": MergePatchDiff(prev, m)})
			if err == nil && len(out) < len(data) {
				sent[id] = m
				fmt.Fprintf(w, "event: thread.patch\ndata: %s\n\n", out)
				return
			}
		}
		sent[id] = m
		fmt.Fprintf(w, "event: thread\ndata: %s\n\n", data)
	}
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return // dropped as slow; the client reconnects
			}
			if f.Event == "thread" {
				var head struct {
					ID string `json:"id"`
				}
				json.Unmarshal(f.Data, &head)
				if time.Since(lastAt[head.ID]) < 100*time.Millisecond {
					pending[head.ID] = f.Data
					continue
				}
				delete(pending, head.ID)
				sendThread(f.Data)
				fl.Flush()
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", f.Event, f.Data)
			fl.Flush()
		case <-tick.C:
			if len(pending) == 0 {
				continue
			}
			for id, d := range pending {
				delete(pending, id)
				sendThread(d)
			}
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
