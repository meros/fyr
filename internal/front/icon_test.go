package front

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/meros/fyr/internal/projicon"
)

// SPEC 0.5 V0.116: a project's icon is read from and uploaded to the host
// the project lives on, which the path names (decision 0031).
func TestFrontProjectIconOnItsHost(t *testing.T) {
	a, b := newHost(t, "a"), newHost(t, "b")
	b.eng.Icons = &projicon.Store{Dir: t.TempDir()}
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	au, bu := hostURL(t, fs.URL, "a"), hostURL(t, fs.URL, "b")
	code, rep := post(t, bu+"/command", `{"commandId":"p1","action":"project.create","args":{"name":"Acme"}}`)
	pid, _ := rep["result"].(map[string]any)["id"].(string)
	if code != 200 || pid == "" {
		t.Fatalf("%d %v", code, rep)
	}
	var img bytes.Buffer
	png.Encode(&img, image.NewNRGBA(image.Rect(0, 0, 8, 8)))
	req, _ := http.NewRequest("POST", bu+"/projects/"+pid+"/icon", bytes.NewReader(img.Bytes()))
	req.Header.Set("X-Fyr", "1")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("upload to b: %v %v", err, resp)
	}
	resp, err := http.Get(bu + "/projects/" + pid + "/icon?size=64")
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("%v %v", err, resp)
	}
	if resp, err := http.Get(au + "/projects/" + pid + "/icon?size=64"); err == nil && resp.StatusCode == 200 {
		t.Fatal("host a served b's project icon")
	}
}
