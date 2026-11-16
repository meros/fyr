package main

import (
	"fmt"
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
// threads, the providers and T3 adoption.
type host struct {
	st       *store.Store
	eng      *engine.Engine
	reg      *provider.Registry
	folders  *hostfs.FS
	t3       engine.T3
	profiles func() []provider.Profile
}

func openHost(cfg config, data, t3DB string, kept map[string]bool) *host {
	st, err := store.Open(filepath.Join(data, "fyr.db"))
	if err != nil {
		log.Fatal(err)
	}
	eng := engine.New(st)
	eng.Kept = kept // V0.90
	for _, r := range cfg.ScratchRoots {
		eng.ScratchRoots = append(eng.ScratchRoots, hostfs.ExpandHome(r, home()))
	}
	// V0.55: Load gives each project one folder (a migration, logged),
	// matches threads by folder and ends auto projects. The start does not
	// seed projects from T3 (t3.Projects stays for a later "Import from T3").
	if err := eng.Load(); err != nil {
		log.Fatal(err)
	}
	folders := &hostfs.FS{Home: home(), Folders: eng.ProjectFolders, Recent: eng.RecentFolders}
	for _, r := range cfg.ScanRoots {
		folders.ScanRoots = append(folders.ScanRoots, hostfs.ExpandHome(r, home()))
	}
	// V0.50: providers. The first start imports today's profiles (same ids)
	// as providers; then they come from Settings → Providers and the
	// config's or T3's profiles only feed detection.
	reg := &provider.Registry{DB: st, Secrets: secrets.Files(filepath.Join(data, "secrets")), Home: home(),
		PermissionMode: cfg.PermissionMode, Getenv: os.Getenv, InUse: eng.ThreadsOn}
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
	h.t3 = engine.T3{E: eng, DB: t3DB, Home: home(), Profiles: func() []string {
		var ids []string
		for _, p := range h.profiles() {
			ids = append(ids, p.ID)
		}
		return ids
	}}
	eng.Adopt = h.t3.Adopt
	eng.Providers = reg // #48: provider commands go down the engine's path
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

// importT3 lists T3's threads that are not adopted yet and, without -list,
// adopts them.
func importT3(x engine.T3, list bool) {
	cs, err := x.Candidates()
	if err != nil {
		log.Fatal(err)
	}
	var ids []string
	for _, c := range cs {
		fmt.Printf("%s  %-14s %-40.40s %s\n", c.UpdatedAt.Local().Format("01-02 15:04"), c.Profile, c.Title, c.Cwd)
		ids = append(ids, c.T3ID)
	}
	if list {
		fmt.Printf("%d threads can be adopted\n", len(ids))
		return
	}
	n, err := x.Adopt(ids)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("adopted %d threads\n", n)
}
