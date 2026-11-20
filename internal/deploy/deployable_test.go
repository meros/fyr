package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const tagObj = "fedcba9876543210fedcba9876543210fedcba98"

// V0.173: the updater reads deployable with git ls-remote and spends no
// GitHub API call; an annotated tag is its peeled commit.
// V0.release-without-gh: with no credential helper, so no gh.
func TestDeployableByGitCostsNoAPICall(t *testing.T) {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	for _, out := range []string{
		relSHA + "\trefs/tags/deployable\n",
		tagObj + "\trefs/tags/deployable\n" + relSHA + "\trefs/tags/deployable^{}\n",
	} {
		g := newFakeGitHub(t, relSHA, k)
		var gitArgs []string
		r := g.releases()
		r.Web = "" // the real github.com, in git's arguments only
		r.Git = func(_ context.Context, args ...string) ([]byte, error) { gitArgs = args; return []byte(out), nil }
		sha, err := r.Deployable(context.Background())
		if err != nil || sha != relSHA || len(g.requests()) != 0 {
			t.Fatalf("sha %q err %v api calls %v", sha, err, g.requests())
		}
		if j := strings.Join(gitArgs, " "); j != "-c credential.helper= ls-remote --tags https://github.com/meros/fyr.git refs/tags/deployable" {
			t.Fatalf("git %s", j)
		}
	}
}

// V0.173: when git cannot say, the API answers: one call for a tag that
// names a commit, a second for an annotated tag's commit.
func TestDeployableFallsBackToAPI(t *testing.T) {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	for _, gitOut := range []string{"", "garbage\n"} {
		g := newFakeGitHub(t, relSHA, k)
		r := g.releases()
		r.Git = func(context.Context, ...string) ([]byte, error) {
			if gitOut == "" {
				return nil, errors.New("network")
			}
			return []byte(gitOut), nil
		}
		sha, err := r.Deployable(context.Background())
		if err != nil || sha != relSHA || len(g.requests()) != 1 {
			t.Fatalf("sha %q err %v api %v", sha, err, g.requests())
		}
	}
	g := newFakeGitHub(t, relSHA, k)
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		switch q.URL.Path {
		case "/repos/meros/fyr/git/ref/tags/deployable":
			fmt.Fprintf(w, `{"object":{"sha":%q,"type":"tag"}}`, tagObj)
		case "/repos/meros/fyr/git/tags/" + tagObj:
			fmt.Fprintf(w, `{"object":{"sha":%q,"type":"commit"}}`, relSHA)
		default:
			return false
		}
		return true
	}
	r := g.releases()
	r.Git = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("network") }
	if sha, err := r.Deployable(context.Background()); err != nil || sha != relSHA || len(g.requests()) != 2 {
		t.Fatalf("annotated: sha %q err %v api %v", sha, err, g.requests())
	}
}
