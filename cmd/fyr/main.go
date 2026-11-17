// fyr is the launcher (SPEC 0.5 V0.13). It runs ~/.local/share/fyr/current/fyrd,
// switches to a new build when the daemon exits with 75, and rolls back when
// the new build does not come up. Keep it small: it is the part that cannot
// replace itself.
//
//	fyr [-data DIR] [fyrd flags…]
//	fyr install BUILD_DIR   make BUILD_DIR/fyrd the current build
//	fyr open [-app]         sign this machine's browser in and open Fyr (V0.130)
//	fyr auth status|observe|enforce [way]   the modes of the ways in (V0.157)
//	fyr device add|approve CODE|revoke ID|list   the signed-in devices (V0.157)
//	fyr pair [CODE [-addr host:port]]   open an invite, or join with one (V0.159)
//	fyr host list|remove ID|drive ID on|off   (V0.158, V0.160)
//	fyr rotate-key          move this install to a new key (V0.160)
//	fyr host list|drive ID on|off   the installs this one is paired with (V0.158)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/deploy"
)

func main() {
	log.SetFlags(log.LstdFlags)
	home, _ := os.UserHomeDir()
	fl := flag.NewFlagSet("fyr", flag.ExitOnError)
	data := fl.String("data", filepath.Join(home, ".local", "share", "fyr"), "data directory")
	seed := fl.String("seed", "", "known-good fyrd to run when there is no current build or when pinned")
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "install" {
		fl.Parse(args[1:])
		if fl.NArg() != 1 {
			log.Fatal("usage: fyr install BUILD_DIR")
		}
		if err := install(*data, fl.Arg(0)); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(args) > 0 && (args[0] == "auth" || args[0] == "device" || args[0] == "host" || args[0] == "pair" || args[0] == "rotate-key") { // V0.157: through the owner socket
		if err := authCommand(args[0], args[1:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(args) > 0 && args[0] == "open" {
		app := fl.Bool("app", false, "open Fyr in a Chrome app window")
		fl.Parse(args[1:])
		if err := open(*data, *app); err != nil {
			log.Fatal(err)
		}
		return
	}
	fl.Parse(args)
	l := &deploy.Launcher{Data: *data, Seed: *seed, Args: append([]string{"serve", "-data", *data}, fl.Args()...)}
	// V0.90: the runner holds the agent processes for as long as the
	// launcher lives, so a new fyrd (a deploy, a crash) takes them over.
	// #167: only the fyrd this launcher starts may use it.
	srv, err := l.ServeRunner(log.Printf)
	if err != nil {
		log.Printf("fyr: runner: %v; agents run as fyrd's children", err)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { l.Signal(<-sig) }()
	err = l.Run()
	if srv != nil {
		srv.Close(5 * time.Second) // the agents end with the launcher
	}
	if err != nil {
		log.Fatal(err)
	}
}

// install copies a built fyrd into builds/<stamp> and points current at it.
func install(data, buildDir string) error {
	b, err := os.ReadFile(filepath.Join(buildDir, "fyrd"))
	if err != nil {
		return err
	}
	id := time.Now().Format("20060102-150405") + "-install"
	dir := filepath.Join(data, "builds", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "fyrd"), b, 0o755); err != nil {
		return err
	}
	cur, prev := filepath.Join(data, "current"), filepath.Join(data, "previous")
	if t, err := os.Readlink(cur); err == nil {
		os.Remove(prev)
		os.Symlink(t, prev)
	}
	tmp := cur + ".tmp"
	os.Remove(tmp)
	if err := os.Symlink(filepath.Join("builds", id), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, cur); err != nil {
		return err
	}
	fmt.Println("current →", dir)
	return nil
}
