package authtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/meros/fyr/internal/auth"
)

// OwnerCall calls the owner socket, as the fyr command does; it is never
// gated by any mode (decision 0030 5.1).
func (f *Fyrd) OwnerCall(method, path string, body any) (map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := newRequest(method, "http://owner"+path, rd)
	resp, err := Unix(f.Sock(auth.OwnerSocket)).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 {
		return out, fmt.Errorf("%s %s: %s %v", method, path, resp.Status, out["error"])
	}
	return out, nil
}

// Fyr runs the real `fyr` command against this install's data folder
// (`fyr auth enforce`, `fyr device add`, …) and returns what it printed.
func (f *Fyrd) Fyr(args ...string) (string, error) {
	bin, err := Binary("fyr")
	if err != nil {
		return "", err
	}
	cmd := Command(bin, append(args, "-data", f.Data)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("fyr %v: %v\n%s", args, err, out)
	}
	return string(out), nil
}

// SetMode moves a way in's mode through the owner socket.
func (f *Fyrd) SetMode(way, mode string) error {
	_, err := f.OwnerCall("POST", "/auth/mode", map[string]string{"way": way, "mode": mode})
	return err
}

// Mode reads a way in's stored mode.
func (f *Fyrd) Mode(way string) (string, error) {
	out, err := f.OwnerCall("GET", "/auth/status", nil)
	if err != nil {
		return "", err
	}
	modes, _ := out["modes"].([]any)
	for _, m := range modes {
		mm, _ := m.(map[string]any)
		if mm["way"] == way {
			s, _ := mm["mode"].(string)
			return s, nil
		}
	}
	return "", fmt.Errorf("no mode for %s in %v", way, out)
}
