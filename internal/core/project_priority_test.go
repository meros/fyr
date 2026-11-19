// V0.project-priority: a project's priority is the owner's, set with
// project.update and shared across a link; normal is stored as none.
package core

import (
	"testing"
	"time"
)

func TestVProjectPriorityUpdate(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	ps := map[string]*Project{"p1": {ID: "p1", Name: "a"}}
	set := func(v any) (*Project, error) {
		_, p, _, err := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectUpdate, Args: Args{"id": "p1", "priority": v}}, now)
		if p != nil {
			ps["p1"] = p
		}
		return p, err
	}
	if p, err := set("high"); err != nil || p.Priority != PriorityHigh || p.Rank() != 1 {
		t.Fatalf("high: %v %+v", err, p)
	}
	if p, err := set("normal"); err != nil || p.Priority != "" || p.Rank() != 0 {
		t.Fatalf("normal: %v %+v", err, p)
	}
	if p, err := set("low"); err != nil || p.Rank() != -1 {
		t.Fatalf("low: %v %+v", err, p)
	}
	if p, err := set(nil); err != nil || p.Priority != "" {
		t.Fatalf("null is normal: %v %+v", err, p)
	}
	if _, err := set("urgent"); err == nil {
		t.Fatal("urgent was accepted")
	}
	// An update that does not name it keeps it.
	set("high")
	_, p, _, _ := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectUpdate, Args: Args{"id": "p1", "name": "b"}}, now)
	if p.Priority != PriorityHigh {
		t.Fatalf("a rename dropped the priority: %+v", p)
	}
	// Only the owner sets it.
	if _, _, _, err := ReduceProject(ps, nil, Input{Source: FromAgent, Kind: KindProjectUpdate, Args: Args{"id": "p1", "priority": "low"}}, now); err == nil {
		t.Fatal("an agent set the priority")
	}
	var none *Project
	if none.Rank() != 0 {
		t.Fatal("no project is not normal")
	}
}

func TestVProjectPriorityShared(t *testing.T) {
	t0 := time.Unix(100, 0).UTC()
	const link = "l_0123456789abcdef"
	ps := map[string]*Project{"p1": {ID: "p1", Name: "a", Link: link, Priority: PriorityLow}}
	_, p, _, err := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectShared, Args: Args{"id": "p1", "link": link, "at": t0.Format(time.RFC3339Nano), "name": "a", "priority": "high"}}, t0)
	if err != nil || p.Priority != PriorityHigh {
		t.Fatalf("shared high: %v %+v", err, p)
	}
	ps["p1"] = p
	_, p, _, err = ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectShared, Args: Args{"id": "p1", "link": link, "at": t0.Add(time.Second).Format(time.RFC3339Nano), "name": "a", "priority": nil}}, t0)
	if err != nil || p.Priority != "" {
		t.Fatalf("shared normal: %v %+v", err, p)
	}
}
