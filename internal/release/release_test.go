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

// V0.release-without-gh: the key a host verifies with is named by its
// fingerprint, the same for the same key and different for another.
func TestFingerprint(t *testing.T) {
	_, pub := keys(t)
	_, other := keys(t)
	a, err := Fingerprint(pub)
	b, _ := Fingerprint(" " + pub + "\n")
	c, _ := Fingerprint(other)
	if err != nil || !strings.HasPrefix(a, "SHA256:") || len(a) != len("SHA256:")+43 || a != b || a == c {
		t.Fatalf("fingerprints %q %q %q (%v)", a, b, c, err)
	}
	if _, err := Fingerprint(PublicKey); err != nil {
		t.Fatalf("the built-in key: %v", err)
	}
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := Fingerprint(bad); err == nil {
			t.Fatalf("%q has a fingerprint", bad)
		}
	}
}

// V0.versioned-releases: version tags and their order.
func TestParseVersionAndOrder(t *testing.T) {
	for _, bad := range []string{"", "v", "1.2.3", "v1.2", "v1.2.3.4", "v01.2.3", "v1.2.3-", "v1.2.3-rc..1", "build-0123456789ab", "deployable", "v1.2.x"} {
		if IsVersion(bad) {
			t.Errorf("%q is a version", bad)
		}
	}
	order := []string{"v0.9.9", "v1.0.0-alpha", "v1.0.0-rc.1", "v1.0.0-rc.2", "v1.0.0-rc.10", "v1.0.0-rc1", "v1.0.0", "v1.0.1", "v1.2.0", "v1.10.0", "v2.0.0"}
	for i := range order {
		a, ok := ParseVersion(order[i])
		if !ok {
			t.Fatalf("%q is not a version", order[i])
		}
		if (a.Pre != "") != strings.Contains(order[i], "-") || a.Prerelease() != strings.Contains(order[i], "-") {
			t.Errorf("%q prerelease %v", order[i], a.Prerelease())
		}
		for j := range order {
			b, _ := ParseVersion(order[j])
			if a.Less(b) != (i < j) {
				t.Errorf("%s < %s is %v", order[i], order[j], a.Less(b))
			}
		}
	}
}
