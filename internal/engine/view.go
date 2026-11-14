package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// viewBridge is put at the top of every view page (SPEC 0.5 V0.37). It
// speaks the MCP Apps message names with the host page: state arrives as
// ui/notifications/state, a press leaves as ui/message, the height as
// ui/notifications/size-changed. The page reads window.fyr.state, calls
// fyr.onState(fn) and fyr.send(text); an element with data-send="text"
// sends on a click, and data-text="a.b" shows that state field.
const viewBridge = `<script>(function(){
var state={},subs=[],n=0;
function post(m){m.jsonrpc="2.0";parent.postMessage(m,"*")}
function pick(o,p){return p.split(".").reduce(function(a,k){return a==null?a:a[k]},o)}
function fill(){var els=document.querySelectorAll("[data-text]");for(var i=0;i<els.length;i++){var v=pick(state,els[i].getAttribute("data-text"));els[i].textContent=v==null?"":typeof v=="object"?JSON.stringify(v):String(v)}}
window.fyr={get state(){return state},onState:function(f){subs.push(f);try{f(state)}catch(e){console.error(e)}},
send:function(t){t=String(t||"").trim();if(t)post({id:"m"+(++n),method:"ui/message",params:{role:"user",content:[{type:"text",text:t}]}})}};
addEventListener("message",function(e){if(e.source!==parent)return;var d=e.data;if(!d||d.method!=="ui/notifications/state")return;
state=(d.params&&d.params.state)||{};fill();subs.forEach(function(f){try{f(state)}catch(x){console.error(x)}})});
document.addEventListener("click",function(e){var b=e.target&&e.target.closest&&e.target.closest("[data-send]");if(b){e.preventDefault();fyr.send(b.getAttribute("data-send")||b.textContent)}});
var last=-1;function size(){var h=Math.ceil(document.documentElement.getBoundingClientRect().height);if(h!==last){last=h;post({method:"ui/notifications/size-changed",params:{height:h}})}}
if(window.ResizeObserver)new ResizeObserver(size).observe(document.documentElement);addEventListener("load",size);
document.addEventListener("DOMContentLoaded",function(){fill();size()});
post({method:"ui/notifications/initialized",params:{}});
})();</script>`

var headTag = regexp.MustCompile(`(?i)<head[^>]*>`)
var htmlTag = regexp.MustCompile(`(?i)<html[^>]*>`)

// viewPage wraps a fragment in the Fyr theme and puts the bridge first.
func viewPage(html string) string {
	if !strings.Contains(strings.ToLower(html), "<html") {
		html = themePage("Live view", html)
	}
	if loc := headTag.FindStringIndex(html); loc != nil {
		return html[:loc[1]] + viewBridge + html[loc[1]:]
	}
	if loc := htmlTag.FindStringIndex(html); loc != nil {
		return html[:loc[1]] + "<head>" + viewBridge + "</head>" + html[loc[1]:]
	}
	return viewBridge + html
}

// viewFor is true for threads with the live view flag.
func (e *Engine) viewFor(th string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[th]
	return t != nil && t.LiveView
}

func stateArg(a map[string]any, key string) (map[string]any, error) {
	raw, present := a[key]
	if !present || raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
	return m, nil
}

func checkState(s map[string]any) error {
	if n := core.StateSize(s); n > core.ViewStateMax {
		return fmt.Errorf("the view state is %d bytes as JSON; at most %d. Keep details in the thread and only the status in the state", n, core.ViewStateMax)
	}
	return nil
}

func (e *Engine) viewTools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "view_publish", For: e.viewFor,
		Description: "Publish this thread's live view: a self-contained HTML page (or fragment) pinned above the conversation, plus its full JSON state. Give html or path (an .html file), and state. Republish only when the layout must change; use view_state for new data. Identical pages are stored once.",
		Schema: obj(map[string]any{
			"html":  str("A self-contained HTML page or fragment (at most 512 kB). It reads window.fyr.state."),
			"path":  str("A local HTML file to publish instead of html."),
			"state": map[string]any{"type": "object", "description": "The full state (at most 16 kB as JSON); \"headline\" is the one-line status."},
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			if e.Attach == nil {
				return "", errors.New("Fyr has no attachment store")
			}
			html, _ := a["html"].(string)
			path, _ := a["path"].(string)
			if (html == "") == (path == "") {
				return "", errors.New("give exactly one of html or path")
			}
			if path != "" {
				st, err := os.Stat(path)
				if err != nil || !st.Mode().IsRegular() {
					return "", fmt.Errorf("%s is not a readable file", path)
				}
				if st.Size() > 512<<10 {
					return "", errors.New("HTML over 512 kB is not published")
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return "", err
				}
				html = string(b)
			}
			if len(html) > 512<<10 {
				return "", errors.New("HTML over 512 kB is not published")
			}
			state, err := stateArg(a, "state")
			if err != nil {
				return "", err
			}
			if state == nil {
				state = map[string]any{}
			}
			if err := checkState(state); err != nil {
				return "", err
			}
			sum := sha256.Sum256([]byte(html))
			key := "view:" + hex.EncodeToString(sum[:])
			id, _ := e.st.GetKV(key)
			if _, err := e.Attach.Get(id); id == "" || err != nil {
				m, err := e.Attach.Save("view.html", "text/html", strings.NewReader(viewPage(html)))
				if err != nil {
					return "", err
				}
				id = m.ID
				e.st.SetKV(key, id)
			}
			e.Observe(th, func(t *core.Thread, now time.Time) { core.ViewPublished(t, id, state, now) },
				agent.Ev{Type: "view.published", Data: map[string]any{"pageId": id, "state": state}})
			return fmt.Sprintf("Published the live view (page %s). Update it with view_state from now on.", id), nil
		}})

	s.Add(mcp.Tool{Name: "view_state", For: e.viewFor,
		Description: "Update this thread's live view state with a JSON Merge Patch (RFC 7396): keys merge, null deletes a key, arrays are replaced whole. The page gets the full new state at once.",
		Schema: obj(map[string]any{
			"patch": map[string]any{"type": "object", "description": "The merge patch, e.g. {\"headline\": \"Tests pass; PR open\", \"blockers\": null}."},
		}, "patch"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			patch, err := stateArg(a, "patch")
			if err != nil {
				return "", err
			}
			if patch == nil {
				return "", errors.New("patch must be a JSON object")
			}
			t, ok := e.Thread(th)
			if !ok {
				return "", errors.New("no thread")
			}
			if t.Live == nil || t.Live.PageID == "" {
				return "", errors.New("this thread has no view page yet; publish one with view_publish first")
			}
			next, _ := core.MergePatch(t.Live.State, patch).(map[string]any)
			if next == nil {
				next = map[string]any{}
			}
			if err := checkState(next); err != nil {
				return "", err
			}
			e.Observe(th, func(t *core.Thread, now time.Time) { core.ViewStateSet(t, next, now) },
				agent.Ev{Type: "view.state", Data: map[string]any{"state": next}})
			return "Updated the live view.", nil
		}})

	s.Add(mcp.Tool{Name: "view_unchanged", For: e.viewFor,
		Description: "Say that this thread's live view is still current after this turn, with a short reason. Call it instead of view_state when nothing the view shows changed.",
		Schema:      obj(map[string]any{"reason": str("Why the view is still current, in a few words.")}, "reason"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			reason, _ := a["reason"].(string)
			if strings.TrimSpace(reason) == "" {
				return "", errors.New("give a reason")
			}
			e.Observe(th, func(t *core.Thread, now time.Time) { core.ViewChecked(t, now) },
				agent.Ev{Type: "view.unchanged", Data: map[string]any{"reason": reason}})
			return "Noted; the view stays as it is.", nil
		}})
}
