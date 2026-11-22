package proc

import (
	"context"
	"os"
	"os/exec"
)

// GitOpts are the few ways Fyr's git calls differ in their environment.
type GitOpts struct {
	// NoOptionalLocks: a look at a repository never rewrites its index
	// (GIT_OPTIONAL_LOCKS=0), so it never contends with an agent's own git
	// commands and never counts as work in the worktree.
	NoOptionalLocks bool
	// SSHBatch: ssh never asks for a password or a host-key answer
	// (BatchMode=yes), for a call that reaches a remote over ssh.
	SSHBatch bool
}

// GitArgv is the command line of git in dir with args: git -C dir args...
// For a Runner that takes a command line. An empty dir leaves -C out.
func GitArgv(dir string, args ...string) []string {
	argv := []string{"git"}
	if dir != "" {
		argv = append(argv, "-C", dir)
	}
	return append(argv, args...)
}

// Git is Command running git in dir (git -C dir; no -C when dir is empty)
// with the environment every Fyr git call shares: no terminal prompt
// (GIT_TERMINAL_PROMPT=0) and git's messages in C locale (LC_ALL=C), so a
// caller can read them.
func Git(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return GitWith(ctx, dir, GitOpts{}, args...)
}

// GitWith is Git with options.
func GitWith(ctx context.Context, dir string, o GitOpts, args ...string) *exec.Cmd {
	argv := GitArgv(dir, args...)
	cmd := Command(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if o.NoOptionalLocks {
		cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	}
	if o.SSHBatch {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return cmd
}
