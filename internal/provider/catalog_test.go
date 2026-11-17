package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
)

func catIDs(p Provider) []string {
	var out []string
	for _, m := range p.Catalog {
		out = append(out, m.ID)
	}
	return out
}

func catOf(p Provider, id string) CatalogModel {
	for _, m := range p.Catalog {
		if m.ID == id {
			return m
		}
	}
	return CatalogModel{}
}

func onIDs(p Provider) []string {
	var out []string
	for _, m := range p.Models {
		out = append(out, m.ID)
	}
	return out
}

// V0.120 (1)(2)(3): the first check fills the catalog and marks nothing
// New; a later check marks a new model New and an on model it no longer
// lists Gone; an off model that is gone leaves; Models never moves; an on
// model the first check does not list was typed by hand (the old text area)
// and is Manual, not Gone.
func TestCatalogMerge(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	p := Provider{ID: "ds", Models: []cli.Model{{ID: "b"}, {ID: "typed"}}, DefaultModel: "b"}
	before := slices.Clone(p.Models)

	p = mergeCatalog(p, []cli.Model{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c"}}, t0)
	if !slices.Equal(onIDs(p), onIDs(Provider{Models: before})) {
		t.Fatalf("a check moved Models: %v", onIDs(p))
	}
	if !p.CheckedAt.Equal(t0) || p.CheckError != "" {
		t.Fatalf("checkedAt %v err %q", p.CheckedAt, p.CheckError)
	}
	for _, m := range p.Catalog {
		if m.New {
			t.Fatalf("first check marked %s New", m.ID)
		}
	}
	if m := catOf(p, "typed"); !m.Manual || m.Gone {
		t.Fatalf("typed: %+v", m)
	}
	if catOf(p, "b").Name != "B" {
		t.Fatal("name not read")
	}

	t1 := t0.Add(24 * time.Hour)
	p = mergeCatalog(p, []cli.Model{{ID: "a"}, {ID: "d", Name: "D"}, {ID: "typed"}}, t1)
	if m := catOf(p, "d"); !m.New || !m.FirstSeen.Equal(t1) {
		t.Fatalf("d: %+v", m)
	}
	if m := catOf(p, "b"); !m.Gone {
		t.Fatalf("b is on and unlisted, not Gone: %+v", m)
	}
	if slices.Contains(catIDs(p), "c") {
		t.Fatal("an off model that is gone stays")
	}
	if m := catOf(p, "typed"); m.Manual {
		t.Fatal("a reported model stays Manual")
	}
	if !slices.Equal(onIDs(p), []string{"b", "typed"}) {
		t.Fatalf("Models moved: %v", onIDs(p))
	}
	// Reported again: no longer Gone.
	p = mergeCatalog(p, []cli.Model{{ID: "a"}, {ID: "b"}, {ID: "d"}, {ID: "typed"}}, t1.Add(time.Hour))
	if catOf(p, "b").Gone {
		t.Fatal("b still Gone")
	}

	// New lasts 14 days.
	d := catOf(p, "d")
	if !isNew(d, t1.Add(13*24*time.Hour)) || isNew(d, t1.Add(15*24*time.Hour)) {
		t.Fatal("14 days")
	}
}

// V0.120 (1): a failed check keeps the last list and stores the reason.
func TestFailedCheckChangesNothing(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	r.Run(cmd("provider.create", map[string]any{"id": "ds", "name": "DS", "kind": "endpoint", "baseURL": "https://e.example", "key": "k-123456789", "models": []any{"m1"}}))
	calls := 0
	r.ListModels = func(context.Context, Provider, string) (claudecli.Listing, error) {
		calls++
		if calls == 1 {
			return claudecli.Listing{Models: []cli.Model{{ID: "m1"}, {ID: "m2"}}}, nil
		}
		return claudecli.Listing{}, errors.New("e.example/v1/models answered 401")
	}
	if rep := r.Run(cmd("provider.refreshModels", map[string]any{"id": "ds"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	p, _ := r.get("ds")
	at := p.CheckedAt
	if rep := r.Run(cmd("provider.refreshModels", map[string]any{"id": "ds"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	q, _ := r.get("ds")
	if !q.CheckedAt.Equal(at) || !slices.Equal(catIDs(q), []string{"m1", "m2"}) || !slices.Equal(onIDs(q), []string{"m1"}) {
		t.Fatalf("a failed check changed %+v", q)
	}
	if !strings.Contains(q.CheckError, "401") {
		t.Fatalf("error %q", q.CheckError)
	}
	d := r.List()[0]
	if d.CheckError == "" || d.CheckedAt == nil || len(d.Catalog) != 2 {
		t.Fatalf("dto %+v", d)
	}
}

// V0.120 (2): provider.update keeps its meaning; a hand-typed id joins the
// catalog as Manual and forget drops it once it is off; the default cannot
// be turned off; switching a New model clears New.
func TestUpdateOnListAndManual(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	r.Run(cmd("provider.create", map[string]any{"id": "ds", "name": "DS", "kind": "endpoint", "baseURL": "https://e.example", "key": "k-123456789", "models": []any{"m1"}}))
	r.ListModels = func(context.Context, Provider, string) (claudecli.Listing, error) {
		return claudecli.Listing{Models: []cli.Model{{ID: "m1"}}}, nil
	}
	r.Run(cmd("provider.refreshModels", map[string]any{"id": "ds"}))
	r.ListModels = func(context.Context, Provider, string) (claudecli.Listing, error) {
		return claudecli.Listing{Models: []cli.Model{{ID: "m1"}, {ID: "m2"}}}, nil
	}
	r.Run(cmd("provider.refreshModels", map[string]any{"id": "ds"}))
	if p, _ := r.get("ds"); !catOf(p, "m2").New {
		t.Fatal("m2 not New")
	}

	if rep := r.Run(cmd("provider.update", map[string]any{"id": "ds", "models": []any{"m1", "m2", "mine"}})); !rep.OK {
		t.Fatal(rep.Error)
	}
	p, _ := r.get("ds")
	if !slices.Equal(onIDs(p), []string{"m1", "m2", "mine"}) || !catOf(p, "mine").Manual || catOf(p, "m2").New {
		t.Fatalf("%+v", p)
	}
	// The default stays on.
	if rep := r.Run(cmd("provider.update", map[string]any{"id": "ds", "models": []any{"m2"}})); rep.OK || !strings.Contains(rep.Error, "Star another model first") {
		t.Fatalf("turned the default off: %+v", rep)
	}
	// forget drops only a hand model that is off.
	r.Run(cmd("provider.update", map[string]any{"id": "ds", "forget": []any{"mine"}}))
	if p, _ := r.get("ds"); !slices.Contains(catIDs(p), "mine") {
		t.Fatal("forgot a model that is on")
	}
	if rep := r.Run(cmd("provider.update", map[string]any{"id": "ds", "models": []any{"m1"}, "forget": []any{"mine", "m2"}})); !rep.OK {
		t.Fatal(rep.Error)
	}
	p, _ = r.get("ds")
	if slices.Contains(catIDs(p), "mine") || !slices.Contains(catIDs(p), "m2") {
		t.Fatalf("forget: %v", catIDs(p))
	}
}

// V0.120 (1): a provider that needs a login or a key is not checked, by
// the owner's Refresh or by the daily check; the daily check runs once a day.
func TestCheckSkipsUnready(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	r.Run(cmd("provider.create", map[string]any{"id": "nokey", "name": "No key", "kind": "endpoint", "baseURL": "https://e.example", "models": []any{"m1"}}))
	r.Run(cmd("provider.create", map[string]any{"id": "ok", "name": "OK", "kind": "endpoint", "baseURL": "https://e.example", "key": "k-123456789", "models": []any{"m1"}}))
	var asked []string
	r.ListModels = func(_ context.Context, p Provider, key string) (claudecli.Listing, error) {
		asked = append(asked, p.ID)
		return claudecli.Listing{Models: []cli.Model{{ID: "m1"}}}, nil
	}
	if rep := r.Run(cmd("provider.refreshModels", map[string]any{"id": "nokey"})); rep.OK || rep.Status != 409 {
		t.Fatalf("checked a provider with no key: %+v", rep)
	}
	now := time.Now()
	r.CheckDaily(now)
	r.CheckDaily(now.Add(time.Hour))
	if !slices.Equal(asked, []string{"ok"}) {
		t.Fatalf("daily asked %v", asked)
	}
	r.CheckDaily(now.Add(25 * time.Hour))
	if !slices.Equal(asked, []string{"ok", "ok"}) {
		t.Fatalf("daily asked %v", asked)
	}
}

// Found live 16 Nov (after the V0.120 deploy): the stored Claude logins had
// a catalog of the 4 known models and no error, because the CLI answered
// with its aliases only and Fyr took that quietly. A check that only has
// Fyr's known list says so (checkNote), on the DTO, and the check is logged.
func TestCheckSaysWhenTheCLIListsOnlyAliases(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	const live = `{"id":"claudeAlt","name":"Claude","cli":"claude","auth":{"kind":"cli-login"},"endpoint":{},` +
		`"models":[{"id":"claude-opus-5-5","name":"Opus 5.5"},{"id":"claude-sonnet-5-5","name":"Sonnet 5.5"},{"id":"claude-haiku-5-5","name":"Haiku 5.5"}],"defaultModel":"claude-opus-5-5"}`
	var p Provider
	if err := json.Unmarshal([]byte(live), &p); err != nil {
		t.Fatal(err)
	}
	if err := r.add(p); err != nil {
		t.Fatal(err)
	}
	r.ListModels = func(context.Context, Provider, string) (claudecli.Listing, error) {
		return claudecli.Listing{Models: claudecli.Models, Reported: []string{"claude-opus-5-5"}, Note: "Claude answered with its short names only; this is Fyr's known list."}, nil
	}
	var logged strings.Builder
	r.Logf = func(format string, a ...any) { fmt.Fprintf(&logged, format+"\n", a...) }
	if rep := r.Run(cmd("provider.refreshModels", map[string]any{"id": "claudeAlt"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	d := r.List()[0]
	if !strings.Contains(d.CheckNote, "Fyr's known list") || d.CheckError != "" || len(d.Catalog) != 4 {
		t.Fatalf("%+v", d)
	}
	if !strings.Contains(logged.String(), "providers: check claudeAlt: 1 models reported (claude-opus-5-5)") || !strings.Contains(logged.String(), "known list") {
		t.Fatalf("log %q", logged.String())
	}
	// A full answer clears the note.
	r.ListModels = func(context.Context, Provider, string) (claudecli.Listing, error) {
		return claudecli.Listing{Models: claudecli.Models}, nil
	}
	r.Run(cmd("provider.refreshModels", map[string]any{"id": "claudeAlt"}))
	if d := r.List()[0]; d.CheckNote != "" {
		t.Fatalf("note stays: %q", d.CheckNote)
	}
}
