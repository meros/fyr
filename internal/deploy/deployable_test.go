package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const tagObj = "fedcba9876543210fedcba9876543210fedcba98"

// V0.173: the updater reads deployable with git ls-remote and spends no
// GitHub API call; an annotated tag is its peeled commit.
func TestDeployableByGitCostsNoAPICall(t *testing.T) {
	for _, out := range []string{
		relSHA + "\trefs/tags/deployable\n",
		tagObj + "\trefs/tags/deployable\n" + relSHA + "\trefs/tags/deployable^{}\n",
	} {
		api := 0
		var gitArgs []string
		r := &Releases{
			Git: func(_ context.Context, args ...string) ([]byte, error) { gitArgs = args; return []byte(out), nil },
			Gh:  func(context.Context, ...string) ([]byte, error) { api++; return nil, errors.New("no") },
		}
		sha, err := r.Deployable(context.Background())
		if err != nil || sha != relSHA || api != 0 {
			t.Fatalf("sha %q err %v api calls %d", sha, err, api)
		}
		if j := strings.Join(gitArgs, " "); j != "ls-remote --tags https://github.com/meros/fyr.git refs/tags/deployable" {
			t.Fatalf("git %s", j)
		}
	}
}

// V0.173: when git cannot say, the API answers.
func TestDeployableFallsBackToAPI(t *testing.T) {
	for _, gitOut := range []string{"", "garbage\n"} {
		api := 0
		r := &Releases{
			Git: func(context.Context, ...string) ([]byte, error) {
				if gitOut == "" {
					return nil, errors.New("network")
				}
				return []byte(gitOut), nil
			},
			Gh: func(_ context.Context, args ...string) ([]byte, error) { api++; return []byte(relSHA + "\n"), nil },
		}
		sha, err := r.Deployable(context.Background())
		if err != nil || sha != relSHA || api != 1 {
			t.Fatalf("sha %q err %v api %d", sha, err, api)
		}
	}
}
