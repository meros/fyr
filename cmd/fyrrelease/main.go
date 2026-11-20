// fyrrelease is CI's tool for publishing a release (#158). It is not part of
// an install.
//
//	fyrrelease keygen                      print a new private and public key
//	fyrrelease pack -commit SHA -dir DIR   write manifest.json, SHA256SUMS and
//	                                       SHA256SUMS.sig (key in $FYR_RELEASE_KEY)
//	                                       for the files already in DIR; -version vX.Y.Z
//	                                       names a version release (V0.versioned-releases)
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/release"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		log.Fatal("usage: fyrrelease keygen | pack -commit SHA -dir DIR [-src DIR]")
	}
	switch os.Args[1] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("private:", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("public: ", base64.StdEncoding.EncodeToString(pub))
	case "pack":
		fl := flag.NewFlagSet("pack", flag.ExitOnError)
		commit := fl.String("commit", "", "full commit SHA")
		dir := fl.String("dir", "", "folder with the built binaries")
		src := fl.String("src", ".", "the checkout, for server.sum and flake.lock")
		goV := fl.String("go", "", "go version")
		nodeV := fl.String("node", "", "node version")
		run := fl.String("run", "", "CI run URL")
		ver := fl.String("version", "", "the version tag (vX.Y.Z) of a version release; empty for a build of main")
		fl.Parse(os.Args[2:])
		if len(*commit) != 40 || *dir == "" {
			log.Fatal("pack needs -commit (40 hex) and -dir")
		}
		if *ver != "" && !release.IsVersion(*ver) {
			log.Fatalf("-version %q is not vX.Y.Z or vX.Y.Z-pre", *ver)
		}
		if err := pack(*commit, *dir, *src, *goV, *nodeV, *run, *ver); err != nil {
			log.Fatal(err)
		}
	case "verify":
		// verify DIR [KEY]: check a downloaded release against the built-in
		// key, or a fork's (V0.release-without-gh).
		if len(os.Args) < 3 {
			log.Fatal("usage: fyrrelease verify DIR [KEY]")
		}
		key := release.PublicKey
		if len(os.Args) > 3 {
			key = os.Args[3]
		}
		info, err := release.Verify(os.Args[2], key)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("verified: commit %s, build %s\n", info.Commit, info.Build)
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func pack(commit, dir, src, goV, nodeV, run, ver string) error {
	key := os.Getenv("FYR_RELEASE_KEY")
	if key == "" {
		return fmt.Errorf("FYR_RELEASE_KEY is empty")
	}
	sum, err := deploy.SourceSum(src)
	if err != nil {
		return fmt.Errorf("server sum: %w", err)
	}
	info := release.Info{Commit: commit, Build: release.Short(commit), ServerSum: sum, Go: goV, Node: nodeV, Run: run,
		Time: time.Now().UTC().Format(time.RFC3339), Version: ver}
	if s, err := release.SumFile(src, []string{"flake.lock"}); err == nil && len(s) >= 64 {
		info.FlakeLock = string(s[:64])
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, release.Manifest), append(b, '\n'), 0o644); err != nil {
		return err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range ents {
		if n := e.Name(); e.Type().IsRegular() && n != release.Sums && n != release.Sig {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	sums, err := release.SumFile(dir, names)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, release.Sums), sums, 0o644); err != nil {
		return err
	}
	sig, err := release.Sign(key, sums)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, release.Sig), []byte(sig+"\n"), 0o644)
}
