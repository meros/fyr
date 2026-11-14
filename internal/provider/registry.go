package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/store"
)

// cutoverKey marks that the first start with providers ran (kv table).
const cutoverKey = "providers.cutover"

// Registry is the host's providers, kept in Fyr's database.
type Registry struct {
	DB             *store.Store
	Secrets        *secrets.Store
	Home           string
	PermissionMode string
	Getenv         func(string) string
	// Detect lists the finds (Detect with this host); optional.
	Detect func() []Found
	// InUse counts the threads on a provider; optional.
	InUse func(id string) int
	// OnChange runs after a provider changed (quota, clients).
	OnChange func()

	mu   sync.Mutex
	list []Provider
	seen map[string]engine.Reply
}

// Load reads the stored providers.
func (r *Registry) Load() error {
	rows, err := r.DB.Providers()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = nil
	for _, b := range rows {
		var p Provider
		if err := json.Unmarshal(b, &p); err != nil {
			return err
		}
		r.list = append(r.list, p)
	}
	return nil
}

func (r *Registry) getenv(k string) string {
	if r.Getenv != nil {
		return r.Getenv(k)
	}
	return ""
}

// Empty reports whether no provider exists.
func (r *Registry) Empty() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.list) == 0 }

// Cutover runs once on the first start with providers (V0.50): with no
// providers, it imports each find whose id is one of today's profiles,
// marked imported. Keys stay where they are (KeyRef): nothing is copied
// without the owner's click.
func (r *Registry) Cutover(found []Found, profileIDs []string) (int, error) {
	if v, _ := r.DB.GetKV(cutoverKey); v != "" {
		return 0, nil
	}
	n := 0
	if r.Empty() {
		want := map[string]bool{}
		for _, id := range profileIDs {
			want[id] = true
		}
		for _, f := range found {
			if !want[f.ID] {
				continue
			}
			if _, err := r.importFound(f, false); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, r.DB.SetKV(cutoverKey, time.Now().UTC().Format(time.RFC3339))
}

func (r *Registry) put(p Provider, pos int) error {
	b, _ := json.Marshal(p)
	return r.DB.PutProvider(p.ID, pos, b)
}

// importFound creates a provider from a find. copyKey (the owner's click)
// copies the key into the secret store; otherwise the provider reads it
// where it was found.
func (r *Registry) importFound(f Found, copyKey bool) (Provider, error) {
	p := Provider{ID: f.ID, Name: f.Name, Color: f.Color, CLI: claudecli.ID, Models: f.Models, DefaultModel: f.Default,
		QuotaKind: f.QuotaKind, Imported: f.Source, CreatedAt: time.Now().UTC()}
	if p.Models == nil {
		p.Models = []cli.Model{}
	}
	if f.Kind == KindEndpoint {
		p.Auth = cli.Auth{Kind: cli.APIKey, Secret: SecretName(f.ID)}
		p.Endpoint = cli.Endpoint{BaseURL: f.BaseURL, ModelEnv: f.ModelEnv}
	} else {
		p.Auth = cli.Auth{Kind: cli.CLILogin, ConfigDir: f.ConfigDir}
		if f.KeyRef != "" || f.TokenLogin {
			p.Auth.Secret = SecretName(f.ID)
			p.TokenLogin = f.TokenLogin
		}
	}
	if f.KeyRef != "" {
		if copyKey {
			v, err := secrets.Ref(f.KeyRef, r.Getenv)
			if err != nil {
				return p, fmt.Errorf("the key is no longer at %s", f.KeyFrom)
			}
			if err := r.Secrets.Put(p.Auth.Secret, v); err != nil {
				return p, err
			}
		} else {
			p.KeyRef = f.KeyRef
		}
	}
	if err := r.add(p); err != nil {
		return p, err
	}
	return p, nil
}

func (r *Registry) add(p Provider) error {
	if err := p.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.list {
		if x.ID == p.ID {
			return fmt.Errorf("a provider %s exists", p.ID)
		}
	}
	if err := r.put(p, len(r.list)); err != nil {
		return err
	}
	r.list = append(r.list, p)
	return nil
}

// key reads a provider's secret: the stored one, else its KeyRef.
func (r *Registry) key(p Provider) (string, error) {
	if p.Auth.Secret != "" {
		if v, err := r.Secrets.Get(p.Auth.Secret); err == nil {
			return v, nil
		}
	}
	if p.KeyRef != "" {
		return secrets.Ref(p.KeyRef, r.Getenv)
	}
	return "", secrets.ErrNotFound
}

func (r *Registry) keyFn(id string) func() (string, error) {
	return func() (string, error) {
		p, ok := r.get(id)
		if !ok {
			return "", secrets.ErrNotFound
		}
		return r.key(p)
	}
}

func (r *Registry) get(id string) (Provider, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.list {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Providers returns a copy of the list.
func (r *Registry) Providers() []Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Provider(nil), r.list...)
}

// Profiles are the providers as launch configs, in order.
func (r *Registry) Profiles() []agent.Profile {
	out := []agent.Profile{}
	for _, p := range r.Providers() {
		out = append(out, p.Profile(r.keyFn(p.ID), r.PermissionMode))
	}
	return out
}

// QuotaProfiles are the providers for the quota service, keyed by auth
// identity.
func (r *Registry) QuotaProfiles() []quota.Profile {
	out := []quota.Profile{}
	for _, p := range r.Providers() {
		out = append(out, quota.Profile{ID: p.ID, Binary: "claude", Spec: p.quotaSpec(), Identity: p.identity(r.Home), Key: r.keyFn(p.ID)})
	}
	return out
}

// status is ready, needs-login or needs-key, with what to do.
func (r *Registry) status(p Provider) (string, string) {
	_, kerr := r.key(p)
	switch p.Auth.Kind {
	case cli.APIKey:
		if kerr != nil {
			return NeedsKey, "Paste the API key for " + p.Name + "."
		}
	case cli.CLILogin:
		if kerr == nil || (!p.TokenLogin && (claudecli.Adapter{}).LoggedIn(cli.Auth{Kind: cli.CLILogin, ConfigDir: r.dir(p.Auth.ConfigDir)})) {
			return Ready, ""
		}
		if p.TokenLogin {
			return NeedsLogin, "This account logs in with a token. Run `claude setup-token` while logged in to it and paste the token as its key."
		}
		cmd := "claude"
		if p.Auth.ConfigDir != "" {
			cmd = "CLAUDE_CONFIG_DIR=" + p.Auth.ConfigDir + " claude"
		}
		return NeedsLogin, "Log in once in a terminal on this host: run `" + cmd + "` and type /login. Or paste a token from `claude setup-token` as its key."
	}
	return Ready, ""
}

func (r *Registry) dir(d string) string { return Host{Home: r.Home}.dir(d) }

// DTO is what clients see of a provider: never a secret, only whether one
// is set and its last 4 characters.
type DTO struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	CLI           string            `json:"cli"`
	Color         string            `json:"color,omitempty"`
	Auth          string            `json:"auth"`
	ConfigDir     string            `json:"configDir,omitempty"`
	BaseURL       string            `json:"baseURL,omitempty"`
	ModelEnv      map[string]string `json:"modelEnv,omitempty"`
	Models        []cli.Model       `json:"models"`
	DefaultModel  string            `json:"defaultModel,omitempty"`
	DefaultEffort string            `json:"defaultEffort,omitempty"`
	Efforts       []string          `json:"efforts"`
	Quota         string            `json:"quota"`
	Status        string            `json:"status"`
	Need          string            `json:"need,omitempty"`
	KeySet        bool              `json:"keySet"`
	KeyLast4      string            `json:"keyLast4,omitempty"`
	KeyFrom       string            `json:"keyFrom,omitempty"`
	Imported      string            `json:"imported,omitempty"`
	TokenLogin    bool              `json:"tokenLogin,omitempty"`
	Threads       int               `json:"threads"`
}

// List is GET /api/providers.
func (r *Registry) List() []DTO {
	out := []DTO{}
	for _, p := range r.Providers() {
		d := DTO{ID: p.ID, Name: p.Name, Kind: p.Kind(), CLI: p.CLI, Color: p.Color, Auth: string(p.Auth.Kind), ConfigDir: p.Auth.ConfigDir,
			BaseURL: p.Endpoint.BaseURL, ModelEnv: p.Endpoint.ModelEnv, Models: p.Models, DefaultModel: p.DefaultModel,
			DefaultEffort: p.DefaultEffort, Efforts: claudecli.Efforts, Quota: p.quotaSpec().Kind, Imported: p.Imported, TokenLogin: p.TokenLogin}
		d.Status, d.Need = r.status(p)
		if v, err := r.key(p); err == nil {
			d.KeySet, d.KeyLast4 = true, secrets.Last4(v)
			if p.Auth.Secret == "" || !r.Secrets.Has(p.Auth.Secret) {
				d.KeyFrom = strings.TrimPrefix(strings.Replace(p.KeyRef, "env:", "", 1), "file:")
			}
		}
		if r.InUse != nil {
			d.Threads = r.InUse(p.ID)
		}
		out = append(out, d)
	}
	return out
}

// Found is GET /api/providers/found: detection, each find marked when a
// provider with its id exists.
func (r *Registry) Found() []Found {
	if r.Detect == nil {
		return []Found{}
	}
	fs := r.Detect()
	out := make([]Found, 0, len(fs))
	for _, f := range fs {
		_, f.Imported = r.get(f.ID)
		out = append(out, f)
	}
	return out
}

func (r *Registry) changed() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

// Command runs provider.* commands; each commandId runs once.
func (r *Registry) Command(c engine.Cmd) engine.Reply {
	if c.CommandID == "" {
		return engine.Reply{Error: "commandId is required", Status: 400}
	}
	r.mu.Lock()
	if r.seen == nil {
		r.seen = map[string]engine.Reply{}
	}
	if rep, ok := r.seen[c.CommandID]; ok {
		r.mu.Unlock()
		return rep
	}
	r.mu.Unlock()
	rep := r.run(c)
	if rep.Error == "" {
		rep.OK = true
		r.changed()
	} else if rep.Status == 0 {
		rep.Status = 400
	}
	r.mu.Lock()
	if len(r.seen) > 500 {
		r.seen = map[string]engine.Reply{}
	}
	r.seen[c.CommandID] = rep
	r.mu.Unlock()
	return rep
}

func fail(err error) engine.Reply { return engine.Reply{Error: err.Error()} }

func (r *Registry) run(c engine.Cmd) engine.Reply {
	a := c.Args
	id := a.Str("id")
	switch c.Action {
	case "provider.import":
		fid := a.Str("foundId")
		for _, f := range r.Found() {
			if f.ID != fid {
				continue
			}
			if f.Imported {
				return engine.Reply{Error: "already imported", Status: 409}
			}
			p, err := r.importFound(f, true)
			if err != nil {
				return fail(err)
			}
			return engine.Reply{Result: map[string]string{"id": p.ID}}
		}
		return engine.Reply{Error: "nothing found as " + fid, Status: 404}
	case "provider.create":
		p, err := r.fromArgs(Provider{CLI: claudecli.ID, CreatedAt: time.Now().UTC()}, a, true)
		if err != nil {
			return fail(err)
		}
		if err := r.add(p); err != nil {
			return fail(err)
		}
		if k := a.Str("key"); k != "" {
			if err := r.Secrets.Put(p.Auth.Secret, k); err != nil {
				return fail(err)
			}
		}
		return engine.Reply{Result: map[string]string{"id": p.ID}}
	case "provider.update":
		p, ok := r.get(id)
		if !ok {
			return engine.Reply{Error: "no provider " + id, Status: 404}
		}
		p, err := r.fromArgs(p, a, false)
		if err != nil {
			return fail(err)
		}
		return r.replace(p)
	case "provider.setKey":
		p, ok := r.get(id)
		if !ok {
			return engine.Reply{Error: "no provider " + id, Status: 404}
		}
		if p.Auth.Kind == cli.NoAuth {
			return fail(errors.New("this provider takes no key"))
		}
		if p.Auth.Secret == "" {
			p.Auth.Secret = SecretName(p.ID)
		}
		if err := r.Secrets.Put(p.Auth.Secret, a.Str("key")); err != nil {
			return fail(err)
		}
		p.KeyRef = "" // the stored key wins from now on
		return r.replace(p)
	case "provider.delete":
		p, ok := r.get(id)
		if !ok {
			return engine.Reply{Error: "no provider " + id, Status: 404}
		}
		if r.InUse != nil {
			if n := r.InUse(id); n > 0 {
				return engine.Reply{Error: fmt.Sprintf("%d threads use %s; hand them over first", n, p.Name), Status: 409}
			}
		}
		if err := r.DB.DeleteProvider(id); err != nil {
			return fail(err)
		}
		if p.Auth.Secret != "" {
			r.Secrets.Delete(p.Auth.Secret)
		}
		r.mu.Lock()
		for i, x := range r.list {
			if x.ID == id {
				r.list = append(r.list[:i:i], r.list[i+1:]...)
				break
			}
		}
		r.mu.Unlock()
		return engine.Reply{}
	}
	return engine.Reply{Error: "unknown action " + c.Action}
}

func (r *Registry) replace(p Provider) engine.Reply {
	if err := p.validate(); err != nil {
		return fail(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, x := range r.list {
		if x.ID == p.ID {
			if err := r.put(p, i); err != nil {
				return fail(err)
			}
			r.list[i] = p
			return engine.Reply{}
		}
	}
	return engine.Reply{Error: "no provider " + p.ID, Status: 404}
}

// fromArgs applies create or update arguments. A key is never stored in
// the provider; create stores it in the secret store afterwards.
func (r *Registry) fromArgs(p Provider, a map[string]any, create bool) (Provider, error) {
	str := func(k string) (string, bool) { v, ok := a[k].(string); return strings.TrimSpace(v), ok }
	if v, ok := str("name"); ok {
		p.Name = v
	}
	if create {
		kind, _ := str("kind")
		switch kind {
		case KindAccount:
			p.Auth.Kind = cli.CLILogin
		case KindEndpoint:
			p.Auth.Kind = cli.APIKey
		default:
			return p, fmt.Errorf("kind must be %s or %s", KindAccount, KindEndpoint)
		}
		p.ID, _ = str("id")
		if p.ID == "" {
			p.ID = r.freeID(p.Name)
		}
		if p.Auth.Kind == cli.APIKey || a["key"] != nil {
			p.Auth.Secret = SecretName(p.ID)
		}
	}
	if v, ok := str("configDir"); ok {
		p.Auth.ConfigDir = tildeHome(v, r.Home)
	}
	if v, ok := str("baseURL"); ok {
		p.Endpoint.BaseURL = strings.TrimRight(v, "/")
	}
	if p.Auth.Kind == cli.APIKey && p.Endpoint.BaseURL == "" && create {
		return p, errors.New("an endpoint needs a base URL")
	}
	if m, ok := a["modelEnv"].(map[string]any); ok {
		p.Endpoint.ModelEnv = map[string]string{}
		for k, v := range m {
			if s, _ := v.(string); s != "" {
				p.Endpoint.ModelEnv[k] = s
			}
		}
	}
	if raw, ok := a["models"].([]any); ok {
		var ids []string
		for _, x := range raw {
			switch v := x.(type) {
			case string:
				ids = append(ids, v)
			case map[string]any:
				s, _ := v["id"].(string)
				ids = append(ids, s)
			}
		}
		p.Models = modelsFor(ids, p.Auth.Kind == cli.APIKey)
	} else if create && p.Auth.Kind == cli.CLILogin {
		p.Models = modelsFor(ids(claudecli.Models), false)
	}
	if p.Models == nil {
		p.Models = []cli.Model{}
	}
	if v, ok := str("defaultModel"); ok {
		p.DefaultModel = v
	}
	if !hasModel(p.Models, p.DefaultModel) {
		p.DefaultModel = ""
		if len(p.Models) > 0 {
			p.DefaultModel = p.Models[0].ID
		}
	}
	if v, ok := str("defaultEffort"); ok {
		if v != "" && !contains(claudecli.Efforts, v) {
			return p, fmt.Errorf("unknown effort %q", v)
		}
		p.DefaultEffort = v
	}
	if v, ok := str("quotaKind"); ok {
		p.QuotaKind = v
	}
	if v, ok := str("color"); ok {
		p.Color = v
	}
	return p, nil
}

func hasModel(ms []cli.Model, id string) bool {
	for _, m := range ms {
		if m.ID == id {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// freeID makes an id from a name that no provider has.
func (r *Registry) freeID(name string) string {
	base := strings.ToLower(slug(strings.TrimSpace(name)))
	base = strings.Trim(base, "-")
	if base == "" || !ValidID(base) {
		base = "p-" + base
		if !ValidID(base) {
			base = "provider"
		}
	}
	if len(base) > 40 {
		base = base[:40]
	}
	id := base
	for i := 2; ; i++ {
		if _, ok := r.get(id); !ok {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}
