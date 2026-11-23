package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// Shooter renders a page to PNG (SPEC 0.5 V0.19).
type Shooter interface {
	Shoot(ctx context.Context, url string, width, height int, full bool) ([]byte, error)
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
	e.record(thread, core.Event{Type: "preview", Data: data})
}

func (e *Engine) showTool() mcp.Tool {
	return mcp.Tool{Name: "show",
		Description: "Show something to the user inside this Fyr thread, on desktop and phone. Give exactly one of: path (an image file; an HTML page, shown with the files next to it; or a folder, shown from its index.html or entry), html (a page or an HTML fragment; fragments get the Fyr theme with CSS variables --bg --fg --muted --line --accent and light and dark modes), or url (Fyr takes a screenshot of the page, for example a dev server on localhost, and shows it). Pages run real HTML in a sandbox: scripts, ES modules, WebGL, WASM, workers, and their own files by relative URL (models such as .glb, textures, .wasm). Libraries load only from cdnjs.cloudflare.com, cdn.jsdelivr.net, unpkg.com, esm.sh, ajax.googleapis.com and www.gstatic.com; no other network, no Fyr API. A folder is at most 500 files and 50 MB (dot names and node_modules are skipped). A 3D page, for example:\n<script type=importmap>{\"imports\":{\"three\":\"https://cdn.jsdelivr.net/npm/three@0.170.0/build/three.module.js\",\"three/addons/\":\"https://cdn.jsdelivr.net/npm/three@0.170.0/examples/jsm/\"}}</script>\n<style>body{margin:0}canvas{display:block;width:100vw;height:100vh;touch-action:none}</style>\n<script type=module>\nimport * as THREE from 'three'; import {OrbitControls} from 'three/addons/controls/OrbitControls.js';\nconst r=new THREE.WebGLRenderer({antialias:true}); r.setSize(innerWidth,innerHeight); document.body.append(r.domElement);\nconst s=new THREE.Scene(), c=new THREE.PerspectiveCamera(50,innerWidth/innerHeight,0.1,100); c.position.set(2,2,3);\ns.add(new THREE.Mesh(new THREE.TorusKnotGeometry(.6,.2,128,16), new THREE.MeshNormalMaterial()));\nconst o=new OrbitControls(c,r.domElement); r.setAnimationLoop(()=>{o.update(); r.render(s,c)});\n</script>\nA model in a folder: <script type=module src=\"https://ajax.googleapis.com/ajax/libs/model-viewer/4.0.0/model-viewer.min.js\"></script><model-viewer src=\"duck.glb\" camera-controls auto-rotate style=\"width:100%;height:100vh\"></model-viewer>. Optional: title, caption, size (phone, desktop or WxH; without it an HTML card is 16:10) and full (whole page). Returns at once with the stored file's path; read that file only if you need to look at it yourself. Use it after UI work and whenever a picture says more than text.",
		Schema: obj(map[string]any{
			"path":  str("A local image, an HTML page (with the files next to it) or a folder (index.html or entry)."),
			"entry": str("For a folder: the page to open, a relative path (default index.html)."), "html": str("A self-contained HTML page or fragment."),
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
			entry, _ := a["entry"].(string)
			size, _ := a["size"].(string)
			sized := strings.TrimSpace(size) != ""
			data := map[string]any{"title": title, "caption": caption}
			switch {
			case path != "":
				st, err := os.Stat(path)
				if err == nil && (st.IsDir() || isHTMLFile(path)) {
					return e.showFolder(th, data, path, st.IsDir(), entry, sized, w, h)
				}
				if entry != "" {
					return "", errors.New("entry is for a folder")
				}
				if err != nil || !st.Mode().IsRegular() {
					return "", fmt.Errorf("%s is not a readable file", path)
				}
				if st.Size() > 5<<20 {
					return "", errors.New("images over 5 MB are not shown")
				}
				mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
				if !strings.HasPrefix(mt, "image/") {
					return "", fmt.Errorf("%s is not an image, an HTML page or a folder", filepath.Base(path))
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
				id, err := e.storeHTML(title, html)
				if err != nil {
					return "", err
				}
				data["kind"], data["attachmentId"] = "html", id
				if sized {
					data["width"], data["height"] = w, h
				}
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
				if !e.shotLimit.allow(th, e.Now()) {
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

func isHTMLFile(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".html" || ext == ".htm"
}

// storeHTML stores a page or a fragment (themed) once by content (V0.19).
func (e *Engine) storeHTML(title, html string) (string, error) {
	if len(html) > 512<<10 {
		return "", errors.New("HTML over 512 kB is not shown")
	}
	// Identical HTML from the agent is stored once, whatever the title.
	sum := sha256.Sum256([]byte(html))
	if !strings.Contains(strings.ToLower(html), "<html") {
		html = themePage(title, html)
	}
	return e.storeOnce("preview:"+hex.EncodeToString(sum[:]), func() (string, error) {
		m, err := e.Attach.Save(nameOr(title, "preview")+".html", "text/html", strings.NewReader(html))
		return m.ID, err
	})
}

// storeOnce stores a file once per content key: the attachment id saved
// under key, or a new one from save when there is none or its file is gone
// (V0.19, V0.37).
func (e *Engine) storeOnce(key string, save func() (string, error)) (string, error) {
	if id, _ := e.st.GetKV(key); id != "" {
		if _, err := e.Attach.Get(id); err == nil {
			return id, nil
		}
	}
	id, err := save()
	if err != nil {
		return "", err
	}
	e.st.SetKV(key, id)
	return id, nil
}

// showFolder shows a folder, or an HTML page with the files next to it, as
// one preview whose relative URLs resolve (SPEC 0.5 V0.47).
func (e *Engine) showFolder(th string, data map[string]any, path string, dir bool, entry string, sized bool, w, h int) (string, error) {
	root := path
	if !dir {
		if entry != "" {
			return "", errors.New("entry is for a folder; give the page as path")
		}
		root, entry = filepath.Dir(path), filepath.Base(path)
	} else if entry == "" {
		entry = "index.html"
	}
	entry = filepath.ToSlash(entry)
	title, _ := data["title"].(string)
	files, outside, err := attach.ScanTree(root)
	if err != nil {
		if !dir && errors.Is(err, attach.ErrTreeTooBig) {
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return "", rerr
			}
			id, serr := e.storeHTML(title, string(b))
			if serr != nil {
				return "", serr
			}
			data["kind"], data["attachmentId"] = "html", id
			if sized {
				data["width"], data["height"] = w, h
			}
			e.addPreview(th, data)
			return fmt.Sprintf("Shown in the thread: %s (preview %s), alone: its folder is over %d files or %d MB, so files next to it were not included; put the page and its files in a folder of their own.", nameOr(title, filepath.Base(path)), id, attach.TreeMaxFiles, attach.TreeMaxBytes>>20), nil
		}
		if errors.Is(err, attach.ErrTreeTooBig) {
			return "", fmt.Errorf("%s: %v (dot names and node_modules are skipped)", root, err)
		}
		return "", fmt.Errorf("%s is not a readable folder: %v", root, err)
	}
	found := false
	for _, f := range files {
		found = found || f.Rel == entry
	}
	if !found {
		return "", fmt.Errorf("the folder has no page %s (give entry, a relative path to the page)", entry)
	}
	// Identical folders are stored once: a hash over the paths and contents.
	hs := sha256.New()
	fmt.Fprintf(hs, "entry %s\x00", entry)
	for _, f := range files {
		fh, err := os.Open(f.Abs)
		if err != nil {
			return "", err
		}
		c := sha256.New()
		_, err = io.Copy(c, fh)
		fh.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hs, "%s\x00%x\x00", f.Rel, c.Sum(nil))
	}
	id, err := e.storeOnce("preview:tree:"+hex.EncodeToString(hs.Sum(nil)), func() (string, error) {
		m, err := e.Attach.SaveTree(nameOr(title, filepath.Base(root)), entry, files)
		return m.ID, err
	})
	if err != nil {
		return "", err
	}
	data["kind"], data["attachmentId"] = "html", id
	if sized {
		data["width"], data["height"] = w, h
	}
	e.addPreview(th, data)
	out := fmt.Sprintf("Shown in the thread: %s (folder preview %s, %d files, page %s).", nameOr(title, filepath.Base(root)), id, len(files), entry)
	if len(outside) > 0 {
		out += " Skipped links that point outside the folder: " + strings.Join(outside, ", ") + "."
	}
	return out, nil
}
