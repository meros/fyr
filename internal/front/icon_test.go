package front

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/meros/fyr/internal/projicon"
)

// SPEC 0.5 V0.116: a project's icon is read from and uploaded to the host
// the project lives on, whichever host is first.
func TestFrontProjectIconOnItsHost(t *testing.T) {
	a, b := newHost(t, "a"), newHost(t, "b")
	b.eng.Icons = &projicon.Store{Dir: t.TempDir()}
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"p1","action":"project.create","args":{"name":"Acme","host":"b"}}`)
	pid, _ := rep["result"].(map[string]any)["id"].(string)
	if code != 200 || pid == "" {
		t.Fatalf("%d %v", code, rep)
	}
	eventually(t, "the project on the front", func() bool {
		resp, err := http.Get(fs.URL + "/api/state")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var s struct{ Projects []map[string]any }
		json.NewDecoder(resp.Body).Decode(&s)
		return len(s.Projects) == 1
	})
	var img bytes.Buffer
	png.Encode(&img, image.NewNRGBA(image.Rect(0, 0, 8, 8)))
	eventually(t, "the upload reaches b", func() bool {
		req, _ := http.NewRequest("POST", fs.URL+"/api/projects/"+pid+"/icon", bytes.NewReader(img.Bytes()))
		req.Header.Set("X-Fyr", "1")
		resp, err := http.DefaultClient.Do(req)
		return err == nil && resp.StatusCode == 200
	})
	resp, err := http.Get(fs.URL + "/api/projects/" + pid + "/icon?size=64")
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("%v %v", err, resp)
	}
}
