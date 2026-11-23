package cgroup

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/kvstore"
)

// UndeclaredSettings are the host's thresholds for undeclared heavy work
// (V0.164 4.3): the config file's, and what the owner saved in Settings (kept
// in the store's KV, which wins). Readers never wait.
type UndeclaredSettings struct {
	KV   kvstore.Table
	Base UndeclaredConfig

	mu  sync.Mutex
	cur atomic.Pointer[UndeclaredConfig]
}

const settingsKey = "undeclared-settings"

// UndeclaredView is the settings as GET /api/load shows them.
type UndeclaredView struct {
	On     bool    `json:"on"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	After  float64 `json:"after"` // seconds
}

// Load reads what the owner saved; call it at start.
func (u *UndeclaredSettings) Load() {
	c := u.Base
	if u.KV != nil {
		if raw, _ := u.KV.GetKV(settingsKey); raw != "" {
			var v struct {
				Off    *bool    `json:"off"`
				CPU    *float64 `json:"cpu"`
				Memory *float64 `json:"memory"`
				After  *float64 `json:"after"`
			}
			if json.Unmarshal([]byte(raw), &v) == nil {
				if v.Off != nil {
					c.Off = *v.Off
				}
				if v.CPU != nil {
					c.CPU = *v.CPU
				}
				if v.Memory != nil {
					c.Memory = *v.Memory
				}
				if v.After != nil {
					c.After = time.Duration(*v.After * float64(time.Second))
				}
			}
		}
	}
	c = c.Norm().withOff(c.Off)
	u.cur.Store(&c)
}

func (c UndeclaredConfig) withOff(off bool) UndeclaredConfig { c.Off = off; return c }

// Get is the settings now.
func (u *UndeclaredSettings) Get() UndeclaredConfig {
	if c := u.cur.Load(); c != nil {
		return *c
	}
	return u.Base.Norm().withOff(u.Base.Off)
}

// View is Get for the front.
func (u *UndeclaredSettings) View() UndeclaredView {
	c := u.Get()
	return UndeclaredView{On: !c.Off, CPU: c.CPU, Memory: c.Memory, After: c.After.Seconds()}
}

// Set applies the owner's change (host.undeclaredSettings: on, cpu, memory,
// after) and keeps it.
func (u *UndeclaredSettings) Set(args map[string]any) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	c := u.Get()
	if v, ok := args["on"].(bool); ok {
		c.Off = !v
	}
	if v, ok := args["cpu"].(float64); ok {
		c.CPU = v
	}
	if v, ok := args["memory"].(float64); ok {
		c.Memory = v
	}
	if v, ok := args["after"].(float64); ok {
		if v < 30 || v > 120 {
			return errors.New("after must be 30 to 120 seconds")
		}
		c.After = time.Duration(v * float64(time.Second))
	}
	if c.CPU <= 0 || c.CPU > 1 || c.Memory <= 0 || c.Memory > 1 {
		return errors.New("the thresholds are shares above 0 and at most 1")
	}
	if u.KV != nil {
		raw, _ := json.Marshal(map[string]any{"off": c.Off, "cpu": c.CPU, "memory": c.Memory, "after": c.After.Seconds()})
		if err := u.KV.SetKV(settingsKey, string(raw)); err != nil {
			return err
		}
	}
	u.cur.Store(&c)
	return nil
}
