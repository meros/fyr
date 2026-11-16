package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func keys(t *testing.T) (priv, pub string) {
	t.Helper()
	p, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k.Seed()), base64.StdEncoding.EncodeToString(p)
}

// pack writes a release for one target into a temp folder, as CI does.
func pack(t *testing.T, priv string) string {
	t.Helper()
	dir := t.TempDir()
	commit := strings.Repeat("ab", 20)
	b, _ := json.Marshal(Info{Commit: commit, Build: Short(commit)})
	os.WriteFile(filepath.Join(dir, Manifest), b, 0o644)
	os.WriteFile(filepath.Join(dir, Asset("fyrd", "linux-amd64")), []byte("daemon"), 0o755)
	os.WriteFile(filepath.Join(dir, Asset("fyr", "linux-amd64")), []byte("launcher"), 0o755)
	sums, err := SumFile(dir, []string{Asset("fyr", "linux-amd64"), Asset("fyrd", "linux-amd64"), Manifest})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, Sums), sums, 0o644)
	sig, err := Sign(priv, sums)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, Sig), []byte(sig+"\n"), 0o644)
	return dir
}

// #149: a signed release verifies; its manifest names the commit.
func TestVerifyGood(t *testing.T) {
	priv, pub := keys(t)
	dir := pack(t, priv)
	info, err := Verify(dir, pub, Asset("fyrd", "linux-amd64"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Build != "abababababab" {
		t.Fatalf("build %q", info.Build)
	}
}

// #149: another key, a changed binary, a changed SHA256SUMS or a missing
// binary are each refused.
func TestVerifyRefuses(t *testing.T) {
	priv, pub := keys(t)
	_, other := keys(t)
	cases := map[string]func(dir string) string{
		"other key": func(string) string { return other },
		"binary changed": func(dir string) string {
			os.WriteFile(filepath.Join(dir, Asset("fyrd", "linux-amd64")), []byte("evil"), 0o755)
			return pub
		},
		"sums changed": func(dir string) string {
			p := filepath.Join(dir, Sums)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, []byte(strings.Repeat("0", 64)+"  extra\n")...), 0o644)
			return pub
		},
		"binary missing": func(dir string) string {
			os.Remove(filepath.Join(dir, Asset("fyrd", "linux-amd64")))
			return pub
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dir := pack(t, priv)
			key := spoil(dir)
			if _, err := Verify(dir, key, Asset("fyrd", "linux-amd64")); err == nil {
				t.Fatal("verified a spoiled release")
			}
		})
	}
}

func TestParseSumsRefusesPaths(t *testing.T) {
	if _, err := ParseSums([]byte(strings.Repeat("a", 64) + "  ../fyrd\n")); err == nil {
		t.Fatal("a path in SHA256SUMS passed")
	}
}
