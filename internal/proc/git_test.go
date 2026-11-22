package proc

import (
	"context"
	"slices"
	"testing"
)

func TestGitEnvAndArgs(t *testing.T) {
	c := Git(context.Background(), "/d", "status")
	if want := []string{"git", "-C", "/d", "status"}; !slices.Equal(c.Args, want) {
		t.Fatalf("args %v", c.Args)
	}
	for _, e := range []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C"} {
		if !slices.Contains(c.Env, e) {
			t.Fatalf("missing %s", e)
		}
	}
	if slices.Contains(c.Env, "GIT_OPTIONAL_LOCKS=0") {
		t.Fatal("locks off without the option")
	}
	c = GitWith(context.Background(), "", GitOpts{NoOptionalLocks: true, SSHBatch: true}, "fetch")
	if !slices.Equal(c.Args, []string{"git", "fetch"}) {
		t.Fatalf("args %v", c.Args)
	}
	for _, e := range []string{"GIT_OPTIONAL_LOCKS=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"} {
		if !slices.Contains(c.Env, e) {
			t.Fatalf("missing %s", e)
		}
	}
}
