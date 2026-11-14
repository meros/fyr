package claude

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/meros/fyr/internal/claude/fake"
)

func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_CLAUDE") == "1" {
		fake.Main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func start(t *testing.T) *Client {
	t.Helper()
	c, err := Start(Spec{Binary: os.Args[0], Args: []string{"--session-id", "s1"}, Env: []string{"FYR_FAKE_CLAUDE=1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Kill(); <-c.Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return c
}

func untilResult(t *testing.T, c *Client) (all []Msg) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m, ok := <-c.Out:
			if !ok {
				t.Fatal("stdout closed")
			}
			all = append(all, m)
			if m["type"] == "result" {
				return
			}
		case <-timeout:
			t.Fatal("no result")
		}
	}
}

func TestEchoTurn(t *testing.T) {
	c := start(t)
	c.SendUser([]Msg{{"type": "text", "text": "hello"}})
	msgs := untilResult(t, c)
	if msgs[0]["session_id"] != "s1" {
		t.Fatalf("session id not passed: %v", msgs[0])
	}
	if r := msgs[len(msgs)-1]; r["result"] != "echo: hello" {
		t.Fatalf("result %v", r)
	}
}

func TestAskUserQuestionRoundTrip(t *testing.T) {
	c := start(t)
	c.OnCanUseTool = func(req Msg) Msg {
		in := req["input"].(Msg)
		in["answers"] = Msg{"Which colour?": "Blue"}
		return Msg{"behavior": "allow", "updatedInput": in}
	}
	c.SendUser([]Msg{{"type": "text", "text": "ask"}})
	msgs := untilResult(t, c)
	found := false
	for _, m := range msgs {
		if m["type"] == "assistant" {
			b := m["message"].(Msg)["content"].([]any)[0].(Msg)
			found = b["text"] == "You chose Blue"
		}
	}
	if !found {
		t.Fatalf("answer did not reach the agent: %v", msgs)
	}
}

func TestInterruptCancelsPendingQuestion(t *testing.T) {
	c := start(t)
	cancelled := make(chan string, 1)
	c.OnCancel = func(id string) { cancelled <- id }
	c.OnCanUseTool = func(req Msg) Msg { time.Sleep(10 * time.Second); return Msg{} }
	c.SendUser([]Msg{{"type": "text", "text": "ask"}})
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-cancelled:
		if id != "ask1" {
			t.Fatalf("cancelled %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no cancel")
	}
	if r := untilResult(t, c); r[len(r)-1]["subtype"] != "error_during_execution" {
		t.Fatalf("unexpected result %v", r[len(r)-1])
	}
}

func TestCrashReportsStderr(t *testing.T) {
	c := start(t)
	c.SendUser([]Msg{{"type": "text", "text": "crash"}})
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	if c.Err() == nil || !contains(c.Err().Error(), "boom") {
		t.Fatalf("err %v", c.Err())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
