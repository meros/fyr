package github

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
)

// V0.fresh-ubuntu-run: a host with no gh login fails every read of the rate
// the same way; the log says it once, again when it changes, and says when a
// read works again.
func TestMeterLogsARateFailureOnce(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	fail := errors.New("exit status 4: To get started with GitHub CLI, please run:  gh auth login\n")
	var err error
	gh := func(context.Context, ...string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return []byte(`{"resources":{"core":{"limit":60,"remaining":59,"reset":1}}}`), nil
	}
	var m Meter
	var failed string
	err = fail
	for range 3 {
		m.readRate(context.Background(), gh, &failed)
	}
	if n := strings.Count(buf.String(), "gh auth login"); n != 1 {
		t.Fatalf("the same failure logged %d times:\n%s", n, buf.String())
	}
	err = errors.New("exit status 1: other")
	m.readRate(context.Background(), gh, &failed)
	err = nil
	m.readRate(context.Background(), gh, &failed)
	err = fail
	m.readRate(context.Background(), gh, &failed)
	out := buf.String()
	for _, want := range []string{"other (said again only when it changes)", "github rate: read again"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if n := strings.Count(out, "gh auth login"); n != 2 {
		t.Errorf("after a good read the failure is said again: %d times\n%s", n, out)
	}
	if m.View().Rate == nil {
		t.Errorf("the good read set no rate")
	}
}
