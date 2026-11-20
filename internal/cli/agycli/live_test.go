//go:build live

package agycli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// Run with: go test -tags live -v ./internal/cli/agycli
func TestLiveAgyAdapter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ad := Adapter{}
	if !ad.LoggedIn(cli.Auth{Kind: cli.CLILogin}) {
		t.Skip("agy is not logged in on this host")
	}

	lst, err := ad.ListModels(ctx, cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(lst.Models) == 0 {
		t.Fatalf("ListModels returned 0 models")
	}
	t.Logf("ListModels returned %d models: %v", len(lst.Models), lst.Reported)

	out, err := ad.Print(ctx, cli.Launch{
		Auth:  cli.Auth{Kind: cli.CLILogin},
		Model: "gemini-3.8-flash",
	}, "Reply with only the exact word PONG and nothing else.", "ping")
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	t.Logf("Print returned: %q", out)
	if !strings.Contains(strings.ToUpper(out), "PONG") {
		t.Fatalf("unexpected Print output: %q", out)
	}
}
