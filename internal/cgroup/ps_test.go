package cgroup

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestPS(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("no ps")
	}
	out, err := PS(context.Background(), "-o", "pid=", "-p", "1")
	if err != nil || strings.TrimSpace(out) != "1" {
		t.Fatalf("ps: %q %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PS(ctx, "-A"); err == nil {
		t.Fatal("a cancelled ctx must stop ps")
	}
}
