package agent

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/runner"
)

// SPEC 0.5 V0.153: a thrashing host makes everything slow, a CLI's start and
// its answers included. Killing a CLI for being slow then and starting it
// again only adds work to the same thrash (17 Nov, 14:04–14:11: two starts
// killed at the 60 s initialize deadline while memory PSI full avg60 was
// 22 %). While the host is under pressure Fyr waits, says so, and kills
// nothing for slowness.

// SetPressure sets Config.Pressure after New (the sampler comes later).
func (m *Manager) SetPressure(f func() (bool, string)) {
	m.mu.Lock()
	m.cfg.Pressure = f
	m.mu.Unlock()
}

// pressured says whether the host thrashes now, and why. Not under m.mu.
func (m *Manager) pressured() (bool, string) {
	m.mu.Lock()
	f := m.cfg.Pressure
	m.mu.Unlock()
	if f == nil {
		return false, ""
	}
	return f()
}

// poll is how often a wait looks at the pressure again.
func (m *Manager) poll() time.Duration {
	d := m.cfg.InitTimeout / 4
	if d > time.Second {
		d = time.Second
	}
	if d < 20*time.Millisecond {
		d = 20 * time.Millisecond
	}
	return d
}

// initialize asks the CLI to start. Past InitTimeout it gives up only on
// a calm host; under pressure it waits on, up to PressureMaxWait more, and
// the thread says it waits for the machine.
func (m *Manager) initialize(threadID string, c cli.Session, hooks claude.Msg) (claude.Msg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.InitTimeout+m.cfg.PressureMaxWait)
	defer cancel()
	done, exited := make(chan struct{}), make(chan struct{})
	var waited, stuck atomic.Bool
	go func() {
		defer close(exited)
		t := time.NewTimer(m.cfg.InitTimeout)
		defer t.Stop()
		select {
		case <-done:
			return
		case <-t.C:
		}
		for {
			ok, why := m.pressured()
			if !ok {
				stuck.Store(true)
				cancel() // a calm host: the CLI is stuck, not slow
				return
			}
			if !waited.Swap(true) {
				log.Printf("thread %s: the agent starts slowly under %s; waiting", threadID, why)
			}
			m.observe(threadID, core.KindMachineWait, core.MachineWaitArgs{Why: why})
			select {
			case <-done:
				return
			case <-time.After(m.poll()):
			}
		}
	}()
	answer, err := c.Initialize(ctx, hooks)
	close(done)
	<-exited
	if waited.Load() {
		m.observe(threadID, core.KindMachineWait, core.MachineWaitArgs{})
	}
	if stuck.Load() && errors.Is(err, context.Canceled) {
		err = context.DeadlineExceeded // the deadline it always was
	}
	return answer, err
}

// startOrWait starts the thread's process; a start that fails while the
// host is under pressure is tried again with backoff, up to
// PressureMaxWait, the thread saying it waits for the machine.
func (m *Manager) startOrWait(threadID string, o *as) (*session, error) {
	s, err := m.start(threadID, o)
	if err == nil || o != nil {
		return s, err
	}
	var ex *runner.ExistsError
	deadline := time.Now().Add(m.cfg.PressureMaxWait)
	back := m.cfg.InitTimeout / 2
	waited := false
	for err != nil && !errors.As(err, &ex) && time.Now().Before(deadline) {
		ok, why := m.pressured()
		if !ok {
			break
		}
		waited = true
		log.Printf("thread %s: the agent did not start under %s (%v); trying again in %s", threadID, why, err, back)
		m.observe(threadID, core.KindMachineWait, core.MachineWaitArgs{Why: why})
		time.Sleep(back)
		if back *= 2; back > 2*time.Minute {
			back = 2 * time.Minute
		}
		s, err = m.start(threadID, o)
	}
	if waited {
		m.observe(threadID, core.KindMachineWait, core.MachineWaitArgs{})
	}
	return s, err
}
