// review 2026-11-15: answer sends on the ask channel under the manager's
// lock and never blocks, so it cannot race the close in read().
package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func askSession(m *Manager, thread, id string) (*session, chan core.Effect) {
	ch := make(chan core.Effect, 1)
	s := &session{thread: thread, asks: map[string]chan core.Effect{id: ch}, reqs: map[string]string{}, digest: newDigest()}
	m.mu.Lock()
	m.sessions[thread] = s
	m.mu.Unlock()
	return s, ch
}

// A second answer to a question nobody has read yet must return, not
// block the caller forever.
func TestAnswerTwiceDoesNotBlock(t *testing.T) {
	m := New(rlHost{}, Config{})
	askSession(m, "t1", "d1")
	e := core.Effect{Kind: "answer", DecisionID: "d1", Allow: true}
	if err := m.answer("t1", e); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.answer("t1", e) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("second answer to the same question was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second answer blocked")
	}
}

// The process exit closes every ask channel under the lock (read()); an
// answer racing it must not panic with "send on closed channel".
func TestAnswerRacesProcessExit(t *testing.T) {
	m := New(rlHost{}, Config{})
	for i := 0; i < 2000; i++ {
		s, _ := askSession(m, "t1", "d1")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.answer("t1", core.Effect{Kind: "answer", DecisionID: "d1", Allow: true})
		}()
		go func() {
			defer wg.Done()
			m.mu.Lock()
			for id, ch := range s.asks {
				close(ch)
				delete(s.asks, id)
			}
			m.mu.Unlock()
		}()
		wg.Wait()
	}
}
