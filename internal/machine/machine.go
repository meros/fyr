// Package machine says what kind of machine a host runs on (SPEC V0.170): a
// laptop (it has a battery), a desktop (a Mac or a Windows PC without one) or
// a server (anything else). The fronts draw each host's mark from it.
package machine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/proc"
)

const (
	Laptop  = "laptop"
	Desktop = "desktop"
	Server  = "server"
)

var (
	once   sync.Once
	cached string
)

// Kind is this machine's kind, read once.
func Kind() string {
	once.Do(func() { cached = kind(runtime.GOOS, battery(runtime.GOOS)) })
	return cached
}

// OS is the operating system as Go names it ("linux", "darwin").
func OS() string { return runtime.GOOS }

func kind(goos string, battery bool) string {
	switch {
	case battery:
		return Laptop
	case goos == "darwin" || goos == "windows":
		return Desktop
	default:
		return Server
	}
}

func battery(goos string) bool {
	switch goos {
	case "linux":
		return linuxBattery("/sys/class/power_supply")
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := proc.Command(ctx, "pmset", "-g", "batt").Output()
		return err == nil && darwinBattery(string(out))
	}
	return false
}

// linuxBattery: a power supply of type Battery that powers the system (a
// wireless mouse's or keyboard's battery has scope Device).
func linuxBattery(root string) bool {
	ents, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range ents {
		dir := filepath.Join(root, e.Name())
		t, _ := os.ReadFile(filepath.Join(dir, "type"))
		if strings.TrimSpace(string(t)) != "Battery" {
			continue
		}
		if s, err := os.ReadFile(filepath.Join(dir, "scope")); err == nil && strings.TrimSpace(string(s)) == "Device" {
			continue
		}
		return true
	}
	return false
}

func darwinBattery(pmset string) bool {
	return strings.Contains(pmset, "InternalBattery")
}
