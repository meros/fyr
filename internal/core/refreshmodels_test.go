package core

import (
	"testing"
	"time"
)

// V0.120: checking a provider's models is the owner's command, with its
// receipt (Await); an agent cannot send it.
func TestRefreshModelsIsTheOwners(t *testing.T) {
	now := time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)
	k := Kinds[KindRefreshModels]
	if k == nil || len(k.From) != 1 || k.From[0] != ByOwner || !k.Await {
		t.Fatalf("not the owner's alone: %+v", k)
	}
	if _, err := ReduceHost(Input{Source: FromAgent, Kind: KindRefreshModels, Args: Args{"id": "claude"}}, now); err == nil {
		t.Fatal("an agent checked a provider")
	}
	r, err := ReduceHost(Input{Source: FromOwner, Kind: KindRefreshModels, Args: Args{"id": "claude"}}, now)
	if err != nil || len(r.Effects) != 1 || r.Effects[0].Kind != EffectProvider {
		t.Fatalf("%v %+v", err, r)
	}
}
