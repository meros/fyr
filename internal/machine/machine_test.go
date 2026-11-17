package machine

import (
	"os"
	"path/filepath"
	"testing"
)

// SPEC V0.170: a host says what kind of machine it is, for its mark.
func TestKindFromBatteryAndOS(t *testing.T) {
	for _, c := range []struct {
		goos    string
		battery bool
		want    string
	}{
		{"linux", true, Laptop},
		{"linux", false, Server},
		{"darwin", true, Laptop},
		{"darwin", false, Desktop},
		{"windows", false, Desktop},
		{"freebsd", false, Server},
	} {
		if got := kind(c.goos, c.battery); got != c.want {
			t.Errorf("kind(%s, battery %v) = %s, want %s", c.goos, c.battery, got, c.want)
		}
	}
}

func TestLinuxBatteryReadsPowerSupplies(t *testing.T) {
	root := t.TempDir()
	if linuxBattery(root) {
		t.Fatal("no power supplies: a battery")
	}
	ac := filepath.Join(root, "AC")
	os.MkdirAll(ac, 0o755)
	os.WriteFile(filepath.Join(ac, "type"), []byte("Mains\n"), 0o644)
	if linuxBattery(root) {
		t.Fatal("mains only: a battery")
	}
	// A wireless mouse reports a battery too, with scope Device: not the machine's.
	mouse := filepath.Join(root, "hidpp_battery_0")
	os.MkdirAll(mouse, 0o755)
	os.WriteFile(filepath.Join(mouse, "type"), []byte("Battery\n"), 0o644)
	os.WriteFile(filepath.Join(mouse, "scope"), []byte("Device\n"), 0o644)
	if linuxBattery(root) {
		t.Fatal("a mouse's battery made a laptop")
	}
	bat := filepath.Join(root, "BAT0")
	os.MkdirAll(bat, 0o755)
	os.WriteFile(filepath.Join(bat, "type"), []byte("Battery\n"), 0o644)
	if !linuxBattery(root) {
		t.Fatal("BAT0: no battery")
	}
}

func TestDarwinBatteryReadsPmset(t *testing.T) {
	if !darwinBattery("Now drawing from 'AC Power'\n -InternalBattery-0 (id=4653155)\t100%; charged; 0:00 remaining present: true\n") {
		t.Fatal("a MacBook's pmset: no battery")
	}
	if darwinBattery("Now drawing from 'AC Power'\n") {
		t.Fatal("a Mac mini's pmset: a battery")
	}
}
