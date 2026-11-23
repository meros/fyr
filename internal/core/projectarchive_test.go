package core

import (
	"strings"
	"testing"
	"time"
)

var at0 = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

// archiveProject reduces one project kind on ps and stores the result.
func archiveProject(t *testing.T, ps map[string]*Project, kind, id string) (before, after *Project, res Result) {
	t.Helper()
	before, after, res, err := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: kind, Args: Args{"id": id}}, at0)
	if err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
	ps[id] = after
	return before, after, res
}

// V0.project-archive: archive stamps the project and shelves each of its
// threads that is not archived already; unarchive clears the stamp and
// brings the shelved ones back. Another project's threads are left alone.
func TestProjectArchiveShelvesItsThreads(t *testing.T) {
	ps := projectMap(&Project{ID: "p_a", Name: "acme"}, &Project{ID: "p_b", Name: "other"})
	ths := map[string]*Thread{
		"t_2": {ID: "t_2", ProjectID: "p_a"},
		"t_1": {ID: "t_1", ProjectID: "p_a"},
		"t_x": {ID: "t_x", ProjectID: "p_a", ArchivedAt: &at0},
		"t_o": {ID: "t_o", ProjectID: "p_b"},
	}
	before, after, _ := archiveProject(t, ps, KindProjectArchive, "p_a")
	if after.ArchivedAt == nil || !after.ArchivedAt.Equal(at0) {
		t.Fatalf("archivedAt %v", after.ArchivedAt)
	}
	fs := ArchiveFollows(FromOwner, before, after, ths)
	if len(fs) != 2 || fs[0].Thread != "t_1" || fs[1].Thread != "t_2" || fs[0].Kind != KindThreadShelve {
		t.Fatalf("follows %+v", fs)
	}
	for _, f := range fs {
		th, v := ths[f.Thread], &View{}
		step(t, th, v, f, at0)
		if th.ShelvedAt == nil {
			t.Fatalf("%s not shelved", th.ID)
		}
	}
	// Again: archived already, nothing changes and nothing follows.
	before, after, res := archiveProject(t, ps, KindProjectArchive, "p_a")
	if kept, _ := res.Reply.(map[string]any)["kept"].(bool); !kept || len(ArchiveFollows(FromOwner, before, after, ths)) != 0 {
		t.Fatalf("a second archive: %+v", res.Reply)
	}
	before, after, _ = archiveProject(t, ps, KindProjectUnarchive, "p_a")
	if after.ArchivedAt != nil {
		t.Fatal("unarchive kept archivedAt")
	}
	fs = ArchiveFollows(FromOwner, before, after, ths)
	if len(fs) != 2 || fs[0].Kind != KindThreadUnshelve {
		t.Fatalf("unarchive follows %+v", fs)
	}
	for _, f := range fs {
		th, v := ths[f.Thread], &View{}
		step(t, th, v, f, at0)
		if th.ShelvedAt != nil {
			t.Fatalf("%s still shelved", th.ID)
		}
	}
	// Another change of the project shelves nothing.
	before, after, _ = archiveProject(t, ps, KindProjectUpdate, "p_a")
	if fs := ArchiveFollows(FromOwner, before, after, ths); len(fs) != 0 {
		t.Fatalf("an update follows %+v", fs)
	}
}

// V0.project-archive: a thread that works or asks cannot be shelved, and the
// reason names it; that refusal is what keeps the project from archiving.
func TestShelveRefusesLiveWork(t *testing.T) {
	cases := map[string]struct {
		th   Thread
		want string
	}{
		"running":  {Thread{ID: "t_1", Title: "Deploy", Conversation: Conversation{Running: true}}, "“Deploy” is working"},
		"starting": {Thread{ID: "t_1", Title: "Deploy", Conversation: Conversation{Starting: &at0}}, "“Deploy” is working"},
		"queued":   {Thread{ID: "t_1", Conversation: Conversation{Outbox: []OutMsg{{ID: "m_1", State: "queued"}}}}, "“t_1” is working"},
		"asking":   {Thread{ID: "t_1", Title: "Deploy", Decisions: []Decision{{ID: "d_1"}}}, "“Deploy” has an open question"},
	}
	for name, c := range cases {
		th := c.th
		_, _, _, err := Reduce(&th, &View{}, Input{Source: FromOwner, Kind: KindThreadShelve}, at0)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	th := Thread{ID: "t_1", Title: "Done"}
	if _, _, _, err := Reduce(&th, &View{}, Input{Source: FromOwner, Kind: KindThreadShelve}, at0); err != nil {
		t.Fatalf("an idle thread: %v", err)
	}
}

// V0.project-archive: new work in a shelved thread brings that thread back:
// the owner's message, a run another thread starts, the agent's question. The
// project stays archived (nothing here touches it).
func TestShelvedThreadReturnsOnNewWork(t *testing.T) {
	shelved := func() (*Thread, *View) {
		th, v := &Thread{ID: "t_1", Title: "Old", ProjectID: "p_a", Cwd: "/w"}, &View{}
		step(t, th, v, Input{Source: FromOwner, Kind: KindThreadShelve}, at0)
		if th.ShelvedAt == nil {
			t.Fatal("not shelved")
		}
		return th, v
	}
	th, v := shelved()
	res := step(t, th, v, Input{Source: FromOwner, Kind: "thread.send", Args: Args{"text": "one more thing"}}, at0.Add(time.Hour))
	if th.ShelvedAt != nil || hasEvent(res, "thread.unshelved") == nil {
		t.Fatalf("a message left it shelved: %+v", res.Events)
	}
	th, v = shelved()
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, at0.Add(time.Hour))
	if th.ShelvedAt != nil {
		t.Fatal("a run left it shelved")
	}
	th, v = shelved() // an async question asked between runs (Fyr's ask tool)
	step(t, th, v, Input{Source: FromAgent, Kind: KindAsk, Args: AskArgs{ID: "d_1", Questions: []Question{{Question: "Ship it?"}}}}, at0.Add(time.Hour))
	if len(th.Decisions) != 1 || th.ShelvedAt != nil {
		t.Fatalf("a question left it shelved: %d decisions, shelved %v", len(th.Decisions), th.ShelvedAt)
	}
	// Facts that are no new work leave it shelved: a clock tick, a PR seen.
	th, v = shelved()
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, at0.Add(time.Hour))
	if th.ShelvedAt == nil {
		t.Fatal("a clock tick brought it back")
	}
}
