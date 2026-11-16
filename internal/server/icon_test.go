package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/projicon"
)

// SPEC 0.5 V0.116: the owner uploads a project's icon (X-Fyr, at most
// 2 MB, an image) and every client reads it at /api/projects/<id>/icon as
// a PNG square of 64 or 128 px.
func TestProjectIconRoutes(t *testing.T) {
	hs, e := newServer(t)
	e.Icons = &projicon.Store{Dir: t.TempDir()}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	r := e.Command(api.Cmd{CommandID: "c1", Action: "project.create", Args: map[string]any{"folder": filepath.Join(dir, "app")}})
	pid := r.Result.(map[string]any)["id"].(string)
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 40, 30)))

	if resp := do(t, "POST", hs.URL+"/api/projects/"+pid+"/icon", "", map[string]string{"Content-Type": "image/png"}, b.String()); resp.StatusCode != 403 {
		t.Fatalf("an upload without X-Fyr: %d", resp.StatusCode)
	}
	if resp := do(t, "POST", hs.URL+"/api/projects/"+pid+"/icon", "", map[string]string{"X-Fyr": "1"}, "<svg/>"); resp.StatusCode != 400 {
		t.Fatalf("an svg: %d", resp.StatusCode)
	}
	if resp := do(t, "POST", hs.URL+"/api/projects/"+pid+"/icon", "", map[string]string{"X-Fyr": "1"}, string(make([]byte, projicon.MaxBytes+5))); resp.StatusCode != 413 {
		t.Fatalf("over 2 MB: %d", resp.StatusCode)
	}
	resp := do(t, "POST", hs.URL+"/api/projects/"+pid+"/icon", "", map[string]string{"X-Fyr": "1", "Content-Type": "image/png"}, b.String())
	var rep api.Reply
	json.NewDecoder(resp.Body).Decode(&rep)
	if resp.StatusCode != 200 || !rep.OK {
		t.Fatalf("%d %+v", resp.StatusCode, rep)
	}
	for _, size := range []string{"64", "128"} {
		resp := do(t, "GET", hs.URL+"/api/projects/"+pid+"/icon?size="+size, "", nil, "")
		body, _ := io.ReadAll(resp.Body)
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" || err != nil || format != "png" || cfg.Width != len(size)*32+map[string]int{"64": 0, "128": 32}[size] {
			t.Fatalf("%s: %d %s %v %dx%d", size, resp.StatusCode, resp.Header.Get("Content-Type"), err, cfg.Width, cfg.Height)
		}
	}
	if resp := do(t, "GET", hs.URL+"/api/projects/p_none/icon", "", nil, ""); resp.StatusCode != 404 {
		t.Fatalf("no project: %d", resp.StatusCode)
	}
}
