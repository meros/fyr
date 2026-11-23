package usage

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/store"
)

// V0.usage-page (decision 0041, phase 3): what a host answers to GET
// /api/usage. The sums add up, so a front joins its hosts' answers by key.

// Query is a rollup: the keys to group by, a day range (inclusive; "" is
// open) and column = value filters, all by their API names.
type Query struct {
	From, To string
	Group    []string
	Where    map[string]string
}

var day = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ParseQuery reads ?from=&to=&group=a,b&<key>=<value>. An unknown group or
// filter key, or a day that is not YYYY-MM-DD, is an error.
func ParseQuery(v url.Values) (Query, error) {
	q := Query{From: v.Get("from"), To: v.Get("to"), Where: map[string]string{}}
	for _, d := range []string{q.From, q.To} {
		if d != "" && !day.MatchString(d) {
			return q, fmt.Errorf("a day is YYYY-MM-DD, not %q", d)
		}
	}
	if g := strings.TrimSpace(v.Get("group")); g != "" {
		for _, k := range strings.Split(g, ",") {
			k = strings.TrimSpace(k)
			if _, ok := store.UsageColumns[k]; !ok {
				return q, fmt.Errorf("unknown group %q", k)
			}
			q.Group = append(q.Group, k)
		}
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "from" || k == "to" || k == "group" {
			continue
		}
		if _, ok := store.UsageColumns[k]; !ok {
			return q, fmt.Errorf("unknown filter %q", k)
		}
		q.Where[k] = v.Get(k)
	}
	return q, nil
}

// ProjectName is a project as the page names it; Link joins a linked
// project's records on several hosts into one (decision 0034).
type ProjectName struct {
	Name string `json:"name"`
	Link string `json:"link,omitempty"`
}

// Names are what the page shows for the ids in the groups: thread titles,
// project names, provider names, and each account by the names of the
// providers on it (an account id is a hash, never a login folder).
type Names struct {
	Thread   map[string]string      `json:"thread"`
	Project  map[string]ProjectName `json:"project"`
	Provider map[string]string      `json:"provider"`
	Account  map[string]string      `json:"account"`
}

// View is a host's answer.
type View struct {
	Groups []store.UsageGroup `json:"groups"`
	Names  Names              `json:"names"`
}

// Lookup resolves names on the host; any of it may be nil.
type Lookup struct {
	Threads   func(ids []string) map[string]string
	Projects  func(ids []string) map[string]ProjectName
	Providers func() map[string]string // provider id → name
	AccountOf func(provider string) (account, billing string)
}

// Answer runs a query on the meter and names what it returns.
func (m *Meter) Answer(q Query, l Lookup) (View, error) {
	gs, err := m.Rollup(q.Group, store.UsageFilter{From: q.From, To: q.To, Where: q.Where})
	if err != nil {
		return View{}, err
	}
	if gs == nil {
		gs = []store.UsageGroup{}
	}
	v := View{Groups: gs, Names: Names{Thread: map[string]string{}, Project: map[string]ProjectName{}, Provider: map[string]string{}, Account: map[string]string{}}}
	seen := func(key string) []string {
		set := map[string]bool{}
		for _, g := range gs {
			if id := g.Key[key]; id != "" {
				set[id] = true
			}
		}
		if id := q.Where[key]; id != "" {
			set[id] = true
		}
		out := make([]string, 0, len(set))
		for id := range set {
			out = append(out, id)
		}
		sort.Strings(out)
		return out
	}
	if ids := seen("thread"); len(ids) > 0 && l.Threads != nil {
		v.Names.Thread = l.Threads(ids)
	}
	if ids := seen("project"); len(ids) > 0 && l.Projects != nil {
		v.Names.Project = l.Projects(ids)
	}
	if l.Providers != nil {
		provs := l.Providers()
		for _, id := range seen("provider") {
			if n, ok := provs[id]; ok {
				v.Names.Provider[id] = n
			}
		}
		if accts := seen("account"); len(accts) > 0 && l.AccountOf != nil {
			byAcct := map[string][]string{}
			for id, name := range provs {
				a, _ := l.AccountOf(id)
				byAcct[a] = append(byAcct[a], name)
			}
			for _, a := range accts {
				ns := byAcct[a]
				sort.Strings(ns)
				if len(ns) > 0 {
					v.Names.Account[a] = strings.Join(ns, " · ")
				}
			}
		}
	}
	return v, nil
}
