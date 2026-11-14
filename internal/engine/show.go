package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// Shooter renders a page to PNG (SPEC 0.5 V0.19).
type Shooter interface {
	Shoot(ctx context.Context, url string, width, height int, full bool) ([]byte, error)
}

// showLimiter allows 10 screenshots a minute per thread.
type showLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *showLimiter) allow(thread string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	keep := l.hits[thread][:0]
	for _, t := range l.hits[thread] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 10 {
		l.hits[thread] = keep
		return false
	}
	l.hits[thread] = append(keep, now)
	return true
}

func parseSize(s string) (int, int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "phone":
		return 390, 844, nil
	case "desktop":
		return 1280, 800, nil
	}
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == 'x' || r == '×' })
	if len(parts) == 2 {
		w, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if e1 == nil && e2 == nil && w >= 200 && h >= 200 && w <= 2560 && h <= 4000 {
			return w, h, nil
		}
	}
	return 0, 0, fmt.Errorf("size must be phone, desktop or WxH (200 to 2560 by 200 to 4000)")
}

// themePage wraps an HTML fragment in a page with the Fyr theme variables.
func themePage(title, body string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` +
		strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;").Replace(title) + `</title><style>
:root{--bg:#ffffff;--fg:#111820;--muted:#56616d;--line:#dde1e6;--accent:#e8960f;--surface:#f6f7f8;color-scheme:light dark}
@media (prefers-color-scheme: dark){:root{--bg:#161d24;--fg:#e8ecf0;--muted:#9aa6b2;--line:#26303a;--accent:#f5a524;--surface:#0f1419}}
html,body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
body{padding:16px}*{box-sizing:border-box}img,svg,canvas,video{max-width:100%;height:auto}
table{border-collapse:collapse}td,th{border:1px solid var(--line);padding:4px 8px}
</style></head><body>` + body + `</body></html>`
}

func (e *Engine) addPreview(thread string, data map[string]any) {
	e.Observe(thread, func(*core.Thread, time.Time) {}, agent.Ev{Type: "preview", Data: data})
}

func (e *Engine) showTool() mcp.Tool {
	return mcp.Tool{Name: "show",
		Description: "Show something to the user inside this Fyr thread, on desktop and phone. Give exactly one of: path (a local image file: a screenshot, a chart), html (a page or an HTML fragment; fragments get the Fyr theme with CSS variables --bg --fg --muted --line --accent and light and dark modes), or url (Fyr takes a screenshot of the page, for example a dev server on localhost, and shows it). Optional: title, caption, size (phone, desktop or WxH) and full (whole page). Returns at once with the stored file's path; read that file only if you need to look at it yourself. Use it after UI work and whenever a picture says more than text.",
		Schema: obj(map[string]any{
			"path": str("A local image file to show."), "html": str("A self-contained HTML page or fragment."),
			"url": str("An http(s) page to screenshot."), "title": str("A short title."), "caption": str("One line under it."),
			"size": str("phone (390×844), desktop (1280×800) or WxH; for url and html."),
			"full": map[string]any{"type": "boolean", "description": "Screenshot the whole page, not only the first screen."},
		}),
		Call: func(ctx context.Context, th string, a map[string]any) (string, error) {
			if e.Attach == nil {
				return "", errors.New("Fyr has no attachment store")
			}
			path, _ := a["path"].(string)
			html, _ := a["html"].(string)
			link, _ := a["url"].(string)
			n := 0
			for _, s := range []string{path, html, link} {
				if s != "" {
					n++
				}
			}
			if n != 1 {
				return "", errors.New("give exactly one of path, html or url")
			}
			title, _ := a["title"].(string)
			caption, _ := a["caption"].(string)
			w, h, err := parseSize(fmt.Sprint(orEmpty(a["size"])))
			if err != nil {
				return "", err
			}
			full, _ := a["full"].(bool)
			data := map[string]any{"title": title, "caption": caption}
			switch {
			case path != "":
				st, err := os.Stat(path)
				if err != nil || !st.Mode().IsRegular() {
					return "", fmt.Errorf("%s is not a readable file", path)
				}
				if st.Size() > 5<<20 {
					return "", errors.New("images over 5 MB are not shown")
				}
				mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
				if !strings.HasPrefix(mt, "image/") {
					return "", fmt.Errorf("%s is not an image (use html for pages)", filepath.Base(path))
				}
				f, err := os.Open(path)
				if err != nil {
					return "", err
				}
				defer f.Close()
				m, err := e.Attach.Save(filepath.Base(path), mt, f)
				if err != nil {
					return "", err
				}
				data["kind"], data["attachmentId"] = "image", m.ID
				e.addPreview(th, data)
				return fmt.Sprintf("Shown in the thread: %s (attachment %s, %s).", nameOr(title, m.Name), m.ID, m.Path), nil
			case html != "":
				if len(html) > 512<<10 {
					return "", errors.New("HTML over 512 kB is not shown")
				}
				// Identical HTML from the agent is stored once, whatever the title.
				sum := sha256.Sum256([]byte(html))
				if !strings.Contains(strings.ToLower(html), "<html") {
					html = themePage(title, html)
				}
				key := "preview:" + hex.EncodeToString(sum[:])
				id, _ := e.st.GetKV(key)
				if _, err := e.Attach.Get(id); id == "" || err != nil {
					m, err := e.Attach.Save(nameOr(title, "preview")+".html", "text/html", strings.NewReader(html))
					if err != nil {
						return "", err
					}
					id = m.ID
					e.st.SetKV(key, id)
				}
				data["kind"], data["attachmentId"], data["width"], data["height"] = "html", id, w, h
				e.addPreview(th, data)
				return fmt.Sprintf("Shown in the thread: %s (preview %s).", nameOr(title, "HTML"), id), nil
			default:
				u, err := url.Parse(link)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return "", errors.New("url must be http or https")
				}
				if e.Shooter == nil {
					return "", errors.New("Fyr has no browser for screenshots on this host")
				}
				if !e.shotLimit.allow(th, time.Now()) {
					return "", errors.New("screenshot limit: 10 a minute per thread")
				}
				sctx, cancel := context.WithTimeout(ctx, 45*time.Second)
				defer cancel()
				png, err := e.Shooter.Shoot(sctx, link, w, h, full)
				if err != nil {
					return "", fmt.Errorf("screenshot of %s failed: %v", link, err)
				}
				m, err := e.Attach.Save(nameOr(title, u.Host)+".png", "image/png", strings.NewReader(string(png)))
				if err != nil {
					return "", err
				}
				data["kind"], data["attachmentId"], data["width"], data["height"], data["url"] = "image", m.ID, w, h, link
				e.addPreview(th, data)
				return fmt.Sprintf("Shown in the thread: screenshot of %s at %d×%d (attachment %s, %s).", link, w, h, m.ID, m.Path), nil
			}
		}}
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func nameOr(a, b string) string {
	a = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 32 {
			return '-'
		}
		return r
	}, strings.TrimSpace(a))
	if a == "" {
		return b
	}
	return a
}
