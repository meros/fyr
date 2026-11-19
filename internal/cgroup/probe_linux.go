package cgroup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func probe(dir string) error {
	bin, err := exec.LookPath("true")
	if err != nil {
		return nil // nothing to try it with; the first agent will tell
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(f.Fd())}
	return cmd.Run()
}

// probeScopeScript prints the cgroup it runs in and its memory.oom.group,
// after proving it may make a child cgroup there (the delegation).
const probeScopeScript = `p=; while IFS= read -r l; do case $l in 0::*) p=${l#0::};; esac; done </proc/self/cgroup
d=/sys/fs/cgroup$p
mkdir "$d/fyr-probe" && rmdir "$d/fyr-probe" || { echo "the scope $p is not delegated to $(id -un)" >&2; exit 3; }
printf '%s\n' "$p"; cat "$d/memory.oom.group" 2>/dev/null || echo none`

// probeScope runs probeScopeScript through prefix (V0.agents-unit).
func probeScope(prefix []string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := append(append([]string{}, prefix[1:]...), "sh", "-c", probeScopeScript)
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, prefix[0], args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("%s: %v: %s", filepath.Base(prefix[0]), err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("the probe said %q", out)
	}
	return lines[0], strings.TrimSpace(lines[1]), nil
}
