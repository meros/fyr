package engine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/store"
)

// V0.120: provider.refreshModels is the owner's command: it goes down the
// engine's path to the registry, keeps its receipt (a retried commandId
// checks once), and an agent's Input is refused.
func TestRefreshModelsOwnerCommand(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := &provider.Registry{DB: st, Secrets: secrets.Files(filepath.Join(dir, "secrets")), Home: dir}
	calls := 0
	reg.ListModels = func(context.Context, provider.Provider, string) (claudecli.Listing, error) {
		calls++
		return claudecli.Listing{Models: []cli.Model{{ID: "m1"}, {ID: "m2"}}}, nil
	}
	e := New(st)
	e.Load()
	e.Providers = reg
	cmd := func(id, action string, args core.Args) api.Reply {
		return e.Command(api.Cmd{CommandID: id, Action: action, Args: args})
	}
	if r := cmd("c0", "provider.create", core.Args{"id": "ds", "name": "DS", "kind": "endpoint", "baseURL": "https://e.example", "key": "k-123456789", "models": []any{"m1"}}); !r.OK {
		t.Fatal(r.Error)
	}
	for range 2 {
		if r := cmd("c1", core.KindRefreshModels, core.Args{"id": "ds"}); !r.OK {
			t.Fatal(r.Error)
		}
	}
	if calls != 1 {
		t.Fatalf("checked %d times", calls)
	}
	if d := reg.List()[0]; len(d.Catalog) != 2 || d.CheckedAt == nil {
		t.Fatalf("%+v", d)
	}
	if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindRefreshModels, Args: core.Args{"id": "ds"}}); err == nil {
		t.Fatal("an agent checked a provider")
	}
	if calls != 1 {
		t.Fatal("the agent's input ran a check")
	}
}
