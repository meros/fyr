package provider

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
)

// CheckEvery is how often the host checks each provider by itself (V0.120).
const CheckEvery = 24 * time.Hour

// cliTimeout bounds a CLI login's check: the CLI may need a second start
// while its own model list arrives (claudecli.ListModels).
const cliTimeout = 40 * time.Second

// listModels asks the provider: its Anthropic-compatible endpoint's /v1/models
// with its key, or the provider's CLI adapter (ModelLister).
func (r *Registry) listModels(ctx context.Context, p Provider, key string) (cli.Listing, error) {
	if r.ListModels != nil {
		return r.ListModels(ctx, p, key)
	}
	if p.Auth.Kind == cli.APIKey && p.Endpoint.BaseURL != "" && (p.CLI == "" || p.CLI == claudecli.ID) {
		c := r.Client
		if c == nil {
			c = &http.Client{}
		}
		ms, err := fetchModels(ctx, c, p.Endpoint.BaseURL, key, fetchLimits{})
		out := cli.Listing{Models: ms}
		for _, m := range ms {
			out.Reported = append(out.Reported, m.ID)
		}
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	dir := ""
	if p.Auth.ConfigDir != "" || p.CLI == "" || p.CLI == claudecli.ID {
		dir = r.dirFor(p.CLI, p.Auth.ConfigDir)
	}
	l := cli.Launch{Binary: r.LoginBinary, Dir: os.TempDir(), Auth: cli.Auth{Kind: p.Auth.Kind, ConfigDir: dir},
		Endpoint: p.Endpoint}
	if l.Auth.Kind == cli.CLILogin && (p.TokenLogin || key != "") {
		l.Key = key
	}
	if l.Auth.Kind == cli.APIKey {
		l.Key = key
	}
	ad := AdapterFor(p.CLI)
	if ml, ok := ad.(cli.ModelLister); ok {
		return ml.ListModels(ctx, l)
	}
	return cli.Listing{Models: ad.Caps().Models}, nil
}

func (r *Registry) isChecking(id string) bool {
	r.cmu.Lock()
	defer r.cmu.Unlock()
	_, ok := r.checking[id]
	return ok
}

// refresh checks one provider now (provider.refreshModels). A provider that
// needs a login or a key is not checked. While a check of it runs, a second
// waits for that one instead of starting another. A failed check stores its
// reason and changes nothing else; the reply is OK either way, since the
// check ran.
func (r *Registry) refresh(id string, now time.Time) api.Reply {
	p, ok := r.get(id)
	if !ok {
		return api.Reply{Error: "no provider " + id, Status: 404}
	}
	if st, need := r.status(p); st != Ready {
		return api.Reply{Error: "Not checked: " + map[string]string{NeedsLogin: "needs a login", NeedsKey: "needs a key"}[st] + ". " + need, Status: 409}
	}
	r.cmu.Lock()
	if ch, busy := r.checking[id]; busy {
		r.cmu.Unlock()
		<-ch
		return r.checkReply(id)
	}
	if r.checking == nil {
		r.checking = map[string]chan struct{}{}
	}
	ch := make(chan struct{})
	r.checking[id] = ch
	r.cmu.Unlock()
	r.changed() // the page shows Checking…
	defer func() {
		r.cmu.Lock()
		delete(r.checking, id)
		r.cmu.Unlock()
		close(ch)
	}()

	key, _ := r.key(p)
	found, err := r.listModels(context.Background(), p, key)
	if err != nil {
		r.logf("providers: check %s failed: %s", id, redact(err, key))
	} else {
		r.logf("providers: check %s: %d models reported (%s)%s", id, len(found.Reported), idList(found.Reported), map[bool]string{true: "; " + found.Note}[found.Note != ""])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, x := range r.list {
		if x.ID != id {
			continue
		}
		if err != nil {
			x.CheckError = redact(err, key)
		} else {
			x = mergeCatalog(x, found.Models, now)
			x.CheckNote = found.Note
		}
		x.TriedAt = now
		if perr := r.put(x, i); perr != nil {
			return fail(perr)
		}
		r.list[i] = x
		return api.Reply{Result: checkResult(x)}
	}
	return api.Reply{Error: "no provider " + id, Status: 404}
}

func checkResult(p Provider) map[string]any {
	return map[string]any{"checkedAt": p.CheckedAt, "checkError": p.CheckError, "models": len(p.Catalog)}
}

func (r *Registry) checkReply(id string) api.Reply {
	p, ok := r.get(id)
	if !ok {
		return api.Reply{Error: "no provider " + id, Status: 404}
	}
	return api.Reply{Result: checkResult(p)}
}

// refreshAsync checks a provider outside a command and tells the clients.
func (r *Registry) refreshAsync(id string) {
	if rep := r.refresh(id, time.Now().UTC()); rep.Error == "" {
		r.changed()
	}
}

// CheckDaily checks each provider that can run and has not been checked in
// CheckEvery (V0.120). The host calls it every hour.
func (r *Registry) CheckDaily(now time.Time) {
	ran := false
	for _, p := range r.Providers() {
		if now.Sub(p.TriedAt) < CheckEvery {
			continue
		}
		if st, _ := r.status(p); st != Ready {
			continue
		}
		ran = true
		r.refresh(p.ID, now.UTC()) // it logs its outcome
	}
	if ran {
		r.changed()
	}
}

func (r *Registry) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// idList is the first 20 ids of a check for the log; the rest are counted.
func idList(ids []string) string {
	if len(ids) <= 20 {
		return strings.Join(ids, ", ")
	}
	return strings.Join(ids[:20], ", ") + fmt.Sprintf(", … %d more", len(ids)-20)
}
