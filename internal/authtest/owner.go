package authtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

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

// Member is an install of the roster, as `fyr host list` shows it.
type Member struct {
	ID    string
	Name  string
	Self  bool
	Port  int
	Drive bool
}

// Roster is `fyr host list`.
type Roster struct {
	Members       []Member
	WhoisLinkUses int64
	Rounds        int64
}

// Roster reads the install's roster through the owner socket.
func (f *Fyrd) Roster() (*Roster, error) {
	out, err := f.OwnerCall("GET", "/host/list", nil)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out)
	var r Roster
	json.Unmarshal(b, &r)
	return &r, nil
}

// MemberNames is the names of the installs this one knows besides itself.
func (r *Roster) Others() []string {
	var out []string
	for _, m := range r.Members {
		if !m.Self {
			out = append(out, m.Name)
		}
	}
	return out
}

// WaitRoster polls until cond holds for the install's roster, or the deadline.
func (f *Fyrd) WaitRoster(d time.Duration, cond func(*Roster) bool) (*Roster, error) {
	end := time.Now().Add(d)
	var last *Roster
	var lastErr error
	for {
		last, lastErr = f.Roster()
		if lastErr == nil && cond(last) {
			return last, nil
		}
		if time.Now().After(end) {
			return last, fmt.Errorf("roster condition not met (%v): %+v\n%s", lastErr, last, f.Log())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
