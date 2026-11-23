package preview

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestMain lets this test binary be a fake Chrome (FYR_FAKE_CHROME=1) that
// speaks the DevTools pipe on fd 3 and 4 and never answers
// Target.closeTarget.
func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_CHROME") == "1" {
		fakeChrome()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeChrome() {
	in, out := bufio.NewReader(os.NewFile(3, "in")), os.NewFile(4, "out")
	send := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, 0))
	}
	for {
		msg, err := in.ReadBytes(0)
		if err != nil {
			return
		}
		var m struct {
			ID        int    `json:"id"`
			Method    string `json:"method"`
			SessionID string `json:"sessionId"`
		}
		json.Unmarshal(msg[:len(msg)-1], &m)
		result := map[string]any{}
		switch m.Method {
		case "Target.closeTarget":
			continue // a Chrome that hangs on it
		case "Target.createTarget":
			result["targetId"] = "t1"
		case "Target.attachToTarget":
			result["sessionId"] = "s1"
		case "Page.navigate":
			send(map[string]any{"method": "Page.loadEventFired", "sessionId": m.SessionID, "params": map[string]any{}})
		case "Page.captureScreenshot":
			result["data"] = base64.StdEncoding.EncodeToString([]byte("png"))
		}
		send(map[string]any{"id": m.ID, "result": result})
	}
}

// V0.review-hangs (8): a Chrome that never answers closing a shot's tab
// holds up the next shot by closeTimeout at most, not forever, and a call
// that got no answer leaves no pending entry behind.
func TestShootSurvivesUnansweredClose(t *testing.T) {
	t.Setenv("FYR_FAKE_CHROME", "1")
	old := closeTimeout
	closeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { closeTimeout = old })
	c := &Chrome{Bin: os.Args[0], Dir: t.TempDir(), Idle: time.Minute}
	defer c.Close()
	for i := range 3 {
		done := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			b, err := c.Shoot(ctx, "about:blank", 800, 600, false)
			if err == nil && string(b) != "png" {
				t.Errorf("shot %q", b)
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("shot %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("shot %d hung behind an unanswered closeTarget", i)
		}
	}
	c.pmu.Lock()
	n := len(c.pending)
	c.pmu.Unlock()
	if n != 0 {
		t.Fatalf("%d pending calls left behind", n)
	}
}
