package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/store"
)

// host is a host's parts before it serves: the store, the engine with its
// threads, and the providers.
type host struct {
	st       *store.Store
	eng      *engine.Engine
	reg      *provider.Registry
	folders  *hostfs.FS
	profiles func() []provider.Profile
}

func openHost(cfg config, data string, kept map[string]bool) *host {
	st, err := store.Open(filepath.Join(data, "fyr.db"))
	if err != nil {
		log.Fatal(err)
	}
	eng := engine.New(st)
	log.SetOutput(io.MultiWriter(log.Writer(), eng.LogTap())) // V0.load-invariants: a card shows the guard's last lines
	eng.Kept = kept                                           // V0.90
	for _, r := range cfg.ScratchRoots {
		eng.ScratchRoots = append(eng.ScratchRoots, hostfs.ExpandHome(r, home()))
	}
	// V0.55: Load gives each project one folder (a migration, logged),
	// matches threads by folder and ends auto projects.
	if err := eng.Load(); err != nil {
		log.Fatal(err)
	}
	folders := &hostfs.FS{Home: home(), Folders: eng.ProjectFolders, Recent: eng.RecentFolders}
	for _, r := range cfg.ScanRoots {
		folders.ScanRoots = append(folders.ScanRoots, hostfs.ExpandHome(r, home()))
	}
	// V0.50: providers. The first start imports today's profiles (same ids)
	// as providers; then they come from Settings → Providers and the
	// config's profiles only feed detection.
	reg := &provider.Registry{DB: st, Secrets: secrets.Files(filepath.Join(data, "secrets")), Home: home(),
		PermissionMode: cfg.PermissionMode, Getenv: os.Getenv, InUse: eng.ThreadsOn, AutoCheck: true}
	detectHost := provider.Host{Home: home(), Profiles: cfg.Profiles, Getenv: os.Getenv, LookPath: exec.LookPath}
	reg.Detect = func() []provider.Found { return provider.Detect(detectHost) }
	if err := reg.Load(); err != nil {
		log.Fatal(err)
	}
	if reg.Empty() {
		var ids []string
		for _, p := range cfg.Profiles {
			ids = append(ids, p.ID)
		}
		if n, err := reg.Cutover(reg.Found(), ids); err != nil {
			log.Printf("providers: import: %v", err)
		} else if n > 0 {
			log.Printf("providers: imported %d profiles", n)
		}
	}
	h := &host{st: st, eng: eng, reg: reg, folders: folders}
	h.profiles = func() []provider.Profile {
		if ps := reg.Profiles(); len(ps) > 0 {
			return ps
		}
		return cfg.Profiles
	}
	return h
}

func (h *host) profile(id string) (provider.Profile, bool) {
	for _, p := range h.profiles() {
		if p.ID == id {
			return p, true
		}
	}
	return provider.Profile{}, false
}
