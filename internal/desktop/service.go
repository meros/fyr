package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Service is fyrd's systemd unit: fyr.service, a system unit that runs as
// the owner, made by the NixOS module or by scripts/install-linux.sh. Pause
// stops it and Resume starts it; polkit lets the owner do that without a
// password where the install says so (the module's desktop option), and
// asks for one otherwise.
type Service struct {
	Unit string // default fyr.service
	// Run runs systemctl; nil runs the real one.
	Run func(args ...string) (string, error)
}

func (s Service) unit() string {
	if s.Unit == "" {
		return "fyr.service"
	}
	return s.Unit
}

func (s Service) run(args ...string) (string, error) {
	if s.Run != nil {
		return s.Run(args...)
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// State is what the tray says about fyrd.
type State struct {
	Installed bool   // systemd knows the unit
	Active    string // ActiveState: active, inactive, activating, failed…
	Enabled   bool   // starts at boot
}

// Running reports whether fyrd runs or is on its way up.
func (st State) Running() bool {
	return st.Active == "active" || st.Active == "activating" || st.Active == "reloading"
}

// Words is the tray's status line.
func (st State) Words() string {
	switch {
	case !st.Installed:
		return "Fyr is not installed as a service"
	case st.Active == "active", st.Active == "reloading":
		return "Fyr is running"
	case st.Active == "activating":
		return "Fyr is starting"
	case st.Active == "deactivating":
		return "Fyr is stopping"
	case st.Active == "failed":
		return "Fyr has stopped with an error"
	}
	return "Fyr is paused"
}

// State reads the unit's state.
func (s Service) State() State {
	out, err := s.run("show", s.unit(), "-p", "LoadState,ActiveState,UnitFileState")
	if err != nil {
		return State{}
	}
	kv := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			kv[k] = v
		}
	}
	return State{
		Installed: kv["LoadState"] == "loaded",
		Active:    kv["ActiveState"],
		Enabled:   kv["UnitFileState"] == "enabled" || kv["UnitFileState"] == "enabled-runtime" || kv["UnitFileState"] == "linked",
	}
}

// Pause stops fyrd; the agents' turns end as on any stop.
func (s Service) Pause() error { return s.verb("stop") }

// Resume starts fyrd.
func (s Service) Resume() error { return s.verb("start") }

func (s Service) verb(v string) error {
	if out, err := s.run(v, s.unit()); err != nil {
		return fmt.Errorf("systemctl %s %s: %v %s", v, s.unit(), err, out)
	}
	return nil
}

// Ensure is what the app does with the unit when it starts. On NixOS the
// module owns the unit (decision 0007): the app starts nothing there and
// only shows the state. Elsewhere (scripts/install-linux.sh) a unit that is
// installed but disabled is enabled, and one that is stopped is started,
// since opening the app is asking for Fyr. A unit nobody installed is left
// to the install.
func (s Service) Ensure(nixos bool) (State, error) {
	st := s.State()
	if nixos || !st.Installed {
		return st, nil
	}
	if !st.Enabled {
		if out, err := s.run("enable", s.unit()); err != nil {
			return st, fmt.Errorf("systemctl enable %s: %v %s", s.unit(), err, out)
		}
	}
	if !st.Running() {
		if err := s.Resume(); err != nil {
			return st, err
		}
	}
	return s.State(), nil
}

// NixOS reports whether this machine runs NixOS.
func NixOS() bool {
	_, err := os.Stat("/etc/NIXOS")
	return err == nil
}
