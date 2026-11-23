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

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/web/viewbridge"
)

// viewBridge is put at the top of every view page (SPEC 0.5 V0.37). It
// speaks the MCP Apps message names with the host page: state arrives as
// ui/notifications/state, a press leaves as ui/message, the height as
// ui/notifications/size-changed. The page reads window.fyr.state, calls
// fyr.onState(fn), fyr.send(text) and fyr.run(id); an element with
// data-send="text" sends on a click, one with data-action="id" runs the
// view's action with that id (V0.60, ui/action), and data-text="a.b" shows
// that state field. A pressed
// data-send element shows feedback at once (busy, then Sent or Not sent),
// whatever the page itself does (V0.37). Its one source is web/viewbridge/bridge.html,
// which the client's mock serves too (V0.single-sources).
var viewBridge = viewbridge.HTML

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

// viewOff refuses a view tool while the thread's live view is off. Every
// thread lists the view tools (V0.107): the CLI reads the tool list once,
// when the process starts, so a process that started before the owner
// turned the view on could otherwise never fill it.
func (e *Engine) viewOff(th string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t := e.threads[th]; t != nil && t.ViewOn() {
		return nil
	}
	return errors.New("the live view is off in this thread; only the owner turns it on, and Fyr tells you when they do")
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

func (e *Engine) viewTools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "view_publish",
		Description: "Publish this thread's live view: a self-contained HTML page (or fragment) pinned above the conversation, plus its full JSON state. Give html or path (an .html file), and state. Republish only when the layout must change; use view_state for new data. Identical pages are stored once.",
		Schema: obj(map[string]any{
			"html":  str("A self-contained HTML page or fragment (at most 512 kB). It reads window.fyr.state."),
			"path":  str("A local HTML file to publish instead of html."),
			"state": map[string]any{"type": "object", "description": "The full state (at most 16 kB as JSON); \"headline\" is the one-line status."},
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			if err := e.viewOff(th); err != nil {
				return "", err
			}
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
			if err := core.CheckViewState(state); err != nil {
				return "", err
			}
			sum := sha256.Sum256([]byte(html))
			id, err := e.storeOnce("view:"+hex.EncodeToString(sum[:]), func() (string, error) {
				m, err := e.Attach.Save("view.html", "text/html", strings.NewReader(viewPage(html)))
				return m.ID, err
			})
			if err != nil {
				return "", err
			}
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindViewPublish, Thread: th, Args: core.ViewPublishArgs{PageID: id, State: state}}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Published the live view (page %s). Update it with view_state from now on.", id), nil
		}})

	s.Add(mcp.Tool{Name: "view_state",
		Description: "Update this thread's live view state with a JSON Merge Patch (RFC 7396): keys merge, null deletes a key, arrays are replaced whole. The page gets the full new state at once.",
		Schema: obj(map[string]any{
			"patch": map[string]any{"type": "object", "description": "The merge patch, e.g. {\"headline\": \"Tests pass; PR open\", \"blockers\": null}."},
		}, "patch"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			if err := e.viewOff(th); err != nil {
				return "", err
			}
			patch, err := stateArg(a, "patch")
			if err != nil {
				return "", err
			}
			if patch == nil {
				return "", errors.New("patch must be a JSON object")
			}
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindViewState, Thread: th, Args: patch}); err != nil {
				return "", err
			}
			return "Updated the live view.", nil
		}})

	s.Add(mcp.Tool{Name: "view_unchanged",
		Description: "Say that this thread's live view is still current after this turn, with a short reason. Call it instead of view_state when nothing the view shows changed.",
		Schema:      obj(map[string]any{"reason": str("Why the view is still current, in a few words.")}, "reason"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			if err := e.viewOff(th); err != nil {
				return "", err
			}
			reason, _ := a["reason"].(string)
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindViewUnchanged, Thread: th, Args: reason}); err != nil {
				return "", err
			}
			return "Noted; the view stays as it is.", nil
		}})
}
