package provider

import (
	"time"

	"github.com/meros/fyr/internal/cli"
)

// NewFor is how long a model the provider newly reports stays New (V0.120).
const NewFor = 14 * 24 * time.Hour

// CatalogModel is one model a provider reports (V0.120). Whether it is on
// is not here: Provider.Models is the list that is on, in the owner's order.
type CatalogModel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
	// FirstSeen: when a check first listed it (or the owner typed it).
	FirstSeen time.Time `json:"firstSeen"`
	// New: first listed after the provider's first check, and the owner has
	// not switched it since (isNew adds the 14 days).
	New bool `json:"new,omitempty"`
	// Gone: on, and the last successful check did not list it.
	Gone bool `json:"gone,omitempty"`
	// Manual: the owner typed its id; no check has listed it.
	Manual bool `json:"manual,omitempty"`
}

func isNew(m CatalogModel, now time.Time) bool { return m.New && now.Sub(m.FirstSeen) < NewFor }

// mergeCatalog takes a successful check's list. Models never moves. The
// first check marks nothing New; an on model it does not list was typed
// into V0.50's text area and becomes Manual. Later, an unlisted on model is
// Gone and an unlisted off one leaves.
func mergeCatalog(p Provider, found []cli.Model, now time.Time) Provider {
	first := p.CheckedAt.IsZero()
	on := map[string]bool{}
	for _, m := range p.Models {
		on[m.ID] = true
	}
	listed := map[string]bool{}
	at := map[string]int{}
	cat := make([]CatalogModel, 0, len(found)+len(p.Catalog))
	old := map[string]CatalogModel{}
	for _, m := range p.Catalog {
		old[m.ID] = m
	}
	for _, f := range found {
		if f.ID == "" || listed[f.ID] {
			continue
		}
		listed[f.ID] = true
		m, ok := old[f.ID]
		if !ok {
			m = CatalogModel{ID: f.ID, FirstSeen: now, New: !first && !on[f.ID]}
		}
		m.Name, m.Efforts, m.Gone, m.Manual = f.Name, f.Efforts, false, false
		at[m.ID] = len(cat)
		cat = append(cat, m)
	}
	for _, m := range p.Catalog {
		if listed[m.ID] {
			continue
		}
		switch {
		case m.Manual:
			cat = append(cat, m) // never listed: it is the owner's, not gone
		case on[m.ID]:
			m.Gone, m.New = true, false
			cat = append(cat, m)
		}
		listed[m.ID] = true
	}
	for _, m := range p.Models {
		if listed[m.ID] {
			continue
		}
		listed[m.ID] = true
		c := CatalogModel{ID: m.ID, Name: m.Name, Efforts: m.Efforts, FirstSeen: now}
		if first {
			c.Manual = true
		} else {
			c.Gone = true
		}
		cat = append(cat, c)
	}
	p.Catalog, p.CheckedAt, p.CheckError = cat, now, ""
	return p
}

// setOn applies the owner's on-list (provider.update {models}): an id the
// catalog lacks joins it as Manual, and a model whose switch changed is no
// longer New.
func setOn(p Provider, next []cli.Model, now time.Time) Provider {
	was := map[string]bool{}
	for _, m := range p.Models {
		was[m.ID] = true
	}
	is := map[string]bool{}
	for _, m := range next {
		is[m.ID] = true
	}
	have := map[string]bool{}
	cat := append([]CatalogModel(nil), p.Catalog...)
	for i, m := range cat {
		have[m.ID] = true
		if was[m.ID] != is[m.ID] {
			cat[i].New = false
		}
	}
	for _, m := range next {
		if !have[m.ID] {
			cat = append(cat, CatalogModel{ID: m.ID, Name: m.Name, Efforts: m.Efforts, FirstSeen: now, Manual: true})
		}
	}
	p.Catalog, p.Models = cat, next
	return p
}

// forget drops hand-typed models that are off (provider.update {forget}).
func forget(p Provider, ids []string) Provider {
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	for _, m := range p.Models {
		delete(drop, m.ID)
	}
	kept := make([]CatalogModel, 0, len(p.Catalog))
	for _, m := range p.Catalog {
		if !(drop[m.ID] && m.Manual) {
			kept = append(kept, m)
		}
	}
	p.Catalog = kept
	return p
}

// catalogModel is the catalog's entry for id.
func (p Provider) catalogModel(id string) (CatalogModel, bool) {
	for _, m := range p.Catalog {
		if m.ID == id {
			return m, true
		}
	}
	return CatalogModel{}, false
}

// CatalogDTO is a catalog model as clients see it: New already has its 14
// days applied.
type CatalogDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Efforts   []string  `json:"efforts,omitempty"`
	FirstSeen time.Time `json:"firstSeen"`
	New       bool      `json:"new,omitempty"`
	Gone      bool      `json:"gone,omitempty"`
	Manual    bool      `json:"manual,omitempty"`
}

func catalogDTO(cs []CatalogModel, now time.Time) []CatalogDTO {
	out := make([]CatalogDTO, 0, len(cs))
	for _, m := range cs {
		out = append(out, CatalogDTO{ID: m.ID, Name: m.Name, Efforts: m.Efforts, FirstSeen: m.FirstSeen, New: isNew(m, now), Gone: m.Gone, Manual: m.Manual})
	}
	return out
}
