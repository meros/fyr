package core

import (
	"testing"
	"time"
)

// SPEC 0.5 V0.23: the default colour is stable for a name and never the beam.
func TestDefaultColor(t *testing.T) {
	if DefaultColor("acme") != DefaultColor("acme") {
		t.Fatal("not stable")
	}
	seen := map[string]bool{}
	for _, n := range []string{"acme", "garden", "nixos", "fyr", "recept", "utdelningar", "a", "b", "c", "d", "e", "f"} {
		c := DefaultColor(n)
		if !ValidColor(c) || c == "amber" {
			t.Fatalf("%s → %q", n, c)
		}
		seen[c] = true
	}
	if len(seen) < 4 {
		t.Fatalf("palette barely used: %v", seen)
	}
	if ValidColor("amber") || ValidColor("#ff0000") {
		t.Fatal("amber and raw colours must be refused")
	}
}

// SPEC 0.5 V0.55: a thread lands in the project whose folder is its folder
// or contains it; the longest folder wins, ties by id; a folder never
// covers a sibling with the same prefix.
func TestMatchFolder(t *testing.T) {
	ps := projectMap(
		&Project{ID: "p_home", Folder: "/home/me"},
		&Project{ID: "p_fyr", Folder: "/home/me/git/fyr"},
		&Project{ID: "p_b", Folder: "/home/me/git/fyr/web/"},
		&Project{ID: "p_a", Folder: "/home/me/git/fyr/web"},
		&Project{ID: "p_label"},
	)
	cases := map[string]string{
		"/home/me/git/fyr":     "p_fyr",
		"/home/me/git/fyr/":    "p_fyr",
		"/home/me/git/fyr/cmd": "p_fyr",
		// V0.change-worktree (4): a worktree Fyr made is under the main checkout.
		"/home/me/git/fyr/.claude/worktrees/feat-x": "p_fyr",
		"/home/me/git/fyr/web/src":                  "p_a",
		"/home/me/git/fyr-projcut":                  "p_home",
		"/home/me":                                  "p_home",
		"/tmp/x":                                    "",
		"":                                          "",
	}
	for dir, want := range cases {
		if got := MatchFolder(ps, dir); got != want {
			t.Errorf("%q: got %q want %q", dir, got, want)
		}
	}
	if got := MatchFolder(projectMap(&Project{ID: "p_root", Folder: "/"}), "/srv/x"); got != "p_root" {
		t.Errorf("root folder: %q", got)
	}
}

func reduceP(t *testing.T, p *Project, src Source, kind string, args Args) (*Project, Result, error) {
	t.Helper()
	ps := map[string]*Project{}
	fresh := &Project{ID: "p_new"}
	if p != nil {
		ps[p.ID] = p
		args["id"] = p.ID
	}
	_, after, res, err := ReduceProject(ps, fresh, Input{Source: src, Kind: kind, Args: args, CmdID: "c"}, time.Unix(0, 0))
	return after, res, err
}

// SPEC 0.5 V0.116: who set each field. A name equal to the folder's last
// element and the default colour are placeholders, open to the setup
// agent; a typed name is the owner's. Projects from before V0.116 derive it.
func TestProjectSetBy(t *testing.T) {
	p, _, err := reduceP(t, nil, FromOwner, KindProjectCreate, Args{"folder": "/src/fyr"})
	if err != nil || p.SetBy("name") != "" || p.SetBy("color") != "" || p.SetBy("image") != "" {
		t.Fatalf("%v %+v", err, p.By)
	}
	p, _, _ = reduceP(t, nil, FromOwner, KindProjectCreate, Args{"name": "Fyr", "folder": "/src/fyr"})
	if p.SetBy("name") != ByOwner || p.SetBy("color") != "" {
		t.Fatalf("a typed name is not the owner's: %+v", p.By)
	}
	old := &Project{ID: "p_old", Name: "acme", Folder: "/src/acme", Color: DefaultColor("acme")}
	if old.SetBy("name") != "" || old.SetBy("color") != "" {
		t.Fatalf("an old placeholder is the owner's")
	}
	old2 := &Project{ID: "p_old2", Name: "Acme", Folder: "/src/acme", Color: "teal"}
	if DefaultColor("Acme") == "teal" {
		old2.Color = "rose"
	}
	if old2.SetBy("name") != ByOwner || old2.SetBy("color") != ByOwner {
		t.Fatalf("an old project's own name and colour are not the owner's")
	}
}

// SPEC 0.5 V0.116: project.setup fills only the fields the owner has not
// set; each value it sets is marked the agent's, and the reply says what
// it set and what it kept.
func TestProjectSetupKeepsTheOwners(t *testing.T) {
	p := &Project{ID: "p_1", Name: "fyr", Folder: "/src/fyr", Color: DefaultColor("fyr")}
	after, res, err := reduceP(t, p, FromAgent, KindProjectSetup, Args{"name": "Fyr", "color": "teal", "image": "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Fyr" || after.Color != "teal" || after.Image != "abc123" || after.SetBy("name") != ByAgent || after.SetBy("color") != ByAgent || after.SetBy("image") != ByAgent {
		t.Fatalf("%+v", after)
	}
	if r := res.Reply.(map[string]any); len(r["set"].([]string)) != 3 || len(r["kept"].([]string)) != 0 {
		t.Fatalf("%+v", r)
	}
	// The agent may replace its own values.
	after2, _, _ := reduceP(t, after, FromAgent, KindProjectSetup, Args{"color": "rose"})
	if after2.Color != "rose" {
		t.Fatal("the agent cannot change its own colour")
	}
	// The owner changes the name and removes the image: both are the owner's now.
	own, _, err := reduceP(t, after2, FromOwner, KindProjectUpdate, Args{"name": "Fyr app", "color": "rose", "image": ""})
	if err != nil || own.SetBy("name") != ByOwner || own.SetBy("image") != ByOwner || own.Image != "" {
		t.Fatalf("%v %+v", err, own)
	}
	if own.SetBy("color") != ByAgent {
		t.Fatal("saving an unchanged colour made it the owner's")
	}
	again, res, err := reduceP(t, own, FromAgent, KindProjectSetup, Args{"name": "X", "image": "def", "color": "blue"})
	if err != nil || again.Name != "Fyr app" || again.Image != "" || again.Color != "blue" {
		t.Fatalf("%v %+v", err, again)
	}
	if kept := res.Reply.(map[string]any)["kept"].([]string); len(kept) != 2 {
		t.Fatalf("kept %v", kept)
	}
	// Only an agent sends project.setup; the owner uses project.update.
	if _, _, err := reduceP(t, own, FromOwner, KindProjectSetup, Args{"name": "Y"}); err == nil {
		t.Fatal("the owner sent project.setup")
	}
	if _, _, err := reduceP(t, own, FromAgent, KindProjectSetup, Args{"color": "amber"}); err == nil {
		t.Fatal("a colour outside the palette")
	}
}

// SPEC 0.5 V0.116: the setup run is recorded on the project: its thread,
// when, and why it could not start.
func TestProjectSetupRunRecorded(t *testing.T) {
	p := &Project{ID: "p_1", Name: "fyr", Folder: "/src/fyr"}
	after, _, err := reduceP(t, p, Observer(ObsHost), KindProjectSetupRun, Args{"thread": "t_1"})
	if err != nil || after.Setup == nil || after.Setup.Thread != "t_1" || after.Setup.At.IsZero() {
		t.Fatalf("%v %+v", err, after.Setup)
	}
	after, _, _ = reduceP(t, p, FromOwner, KindProjectSetupRun, Args{"error": "no Haiku"})
	if after.Setup.Error != "no Haiku" {
		t.Fatalf("%+v", after.Setup)
	}
	if _, _, err := reduceP(t, p, FromAgent, KindProjectSetupRun, Args{"thread": "t_2"}); err == nil {
		t.Fatal("an agent recorded a setup run")
	}
}

// Decision 0034: only the owner links, a link has one shape, and linking
// makes the shown fields the owner's, so a setup agent cannot change them.
func TestProjectLink(t *testing.T) {
	now := time.Unix(100, 0)
	ps := map[string]*Project{"p1": {ID: "p1", Name: "fyr", Folder: "/w/fyr"}}
	const link = "l_0123456789abcdef"
	in := func(src Source, kind string, a Args) Input { return Input{Source: src, Kind: kind, Args: a} }
	if _, _, _, err := ReduceProject(ps, nil, in(FromAgent, KindProjectLink, Args{"id": "p1", "link": link}), now); err == nil {
		t.Fatal("an agent linked a project")
	}
	if _, _, _, err := ReduceProject(ps, nil, in(FromOwner, KindProjectLink, Args{"id": "p1", "link": "nope"}), now); err == nil {
		t.Fatal("a bad link was taken")
	}
	_, after, _, err := ReduceProject(ps, nil, in(FromOwner, KindProjectLink, Args{"id": "p1", "link": link}), now)
	if err != nil || after.Link != link {
		t.Fatalf("link: %v %+v", err, after)
	}
	ps["p1"] = after
	_, set, res, err := ReduceProject(ps, nil, in(FromAgent, KindProjectSetup, Args{"id": "p1", "name": "Agent's", "color": "teal"}), now)
	if err != nil || set.Name != "fyr" || set.Color != "" {
		t.Fatalf("setup changed a linked project: %v %+v %v", err, set, res.Reply)
	}
	_, off, _, err := ReduceProject(ps, nil, in(FromOwner, KindProjectLink, Args{"id": "p1", "link": ""}), now)
	if err != nil || off.Link != "" {
		t.Fatalf("unlink: %v %+v", err, off)
	}
}

// Decision 0034: the newest shared values win, and only they change.
func TestProjectShared(t *testing.T) {
	t0 := time.Unix(100, 0).UTC()
	const link = "l_0123456789abcdef"
	ps := map[string]*Project{"p1": {ID: "p1", Name: "a", Link: link, Folder: "/w", Model: "m", SharedAt: t0}}
	send := func(at time.Time, l string) (*Project, error) {
		_, p, _, err := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectShared, Args: Args{"id": "p1", "link": l, "at": at.Format(time.RFC3339Nano), "name": "new", "folder": "/x", "model": "z"}}, t0)
		return p, err
	}
	if p, err := send(t0, link); err != nil || p.Name != "a" {
		t.Fatalf("the same stamp: %v %+v", err, p)
	}
	if _, err := send(t0.Add(time.Second), "l_ffffffffffffffff"); err == nil {
		t.Fatal("another link's values were taken")
	}
	p, err := send(t0.Add(time.Second), link)
	if err != nil || p.Name != "new" || p.Folder != "/w" || p.Model != "m" || !p.SharedAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("newer: %v %+v", err, p)
	}
}

func projectMap(ps ...*Project) map[string]*Project {
	m := map[string]*Project{}
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

// V0.review-correctness (2): Filed is the one filing rule, and the reduce applies
// it, so the rules and the invariants of that reduce see where the thread
// lands.
func TestFiled(t *testing.T) {
	ps := projectMap(&Project{ID: "p_home", Folder: "/home/me"}, &Project{ID: "p_web", Folder: "/home/me/web"})
	cases := []struct {
		name   string
		t      Thread
		id     string
		manual bool
	}{
		{"unfiled, covered", Thread{Cwd: "/home/me/web/src"}, "p_web", false},
		{"filed, a closer folder", Thread{Cwd: "/home/me/web", ProjectID: "p_home"}, "p_web", false},
		{"filed by hand stays", Thread{Cwd: "/home/me/web", ProjectID: "p_home", ProjectManual: true}, "p_home", true},
		{"unfiled by hand stays", Thread{Cwd: "/home/me/web", ProjectManual: true}, "", true},
		{"gone project, by hand", Thread{Cwd: "/srv", ProjectID: "p_gone", ProjectManual: true}, "", false},
		{"gone project, covered", Thread{Cwd: "/home/me/x", ProjectID: "p_gone", ProjectManual: true}, "p_home", false},
		{"nothing covers, keeps", Thread{Cwd: "/srv", ProjectID: "p_web"}, "p_web", false},
	}
	for _, c := range cases {
		if id, manual := Filed(&c.t, ps); id != c.id || manual != c.manual {
			t.Errorf("%s: got %q %v, want %q %v", c.name, id, manual, c.id, c.manual)
		}
	}
	th := &Thread{ID: "t", Cwd: "/home/me/web", ProjectID: "p_gone"}
	in := Input{Source: Observer(ObsClock), Kind: KindClock, Thread: "t"}
	got, _, _, err := ReduceIn(Env{Projects: ps}, th, nil, in, nt0)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "p_web" || th.ProjectID != "p_gone" {
		t.Errorf("reduce filed %q (input left %q)", got.ProjectID, th.ProjectID)
	}
	if got, _, _, _ := Reduce(th, nil, in, nt0); got.ProjectID != "p_gone" {
		t.Errorf("a reduce without the host's projects filed %q", got.ProjectID)
	}
}

// V0.handover-filed: a handed-in thread is filed from the Input's Project,
// not from a project the received thread carries: a chosen project is a
// filing by hand, no choice files it by its folder.
func TestHandedInFiledFromTheInput(t *testing.T) {
	ps := map[string]*Project{"p_a": {ID: "p_a", Folder: "/w/acme"}, "p_b": {ID: "p_b", Folder: "/w/beta"}}
	for _, c := range []struct {
		project, want string
		manual        bool
	}{{"", "p_a", false}, {"p_b", "p_b", true}} {
		in := Input{Source: Observer(ObsHost), Kind: KindThreadHandedIn, Thread: "t_1", Args: Adopted{
			Thread:  &Thread{ID: "t_x", Cwd: "/w/acme/app", ProjectID: "p_b", ProjectManual: true},
			View:    &View{User: Owner},
			Project: c.project,
		}}
		got, _, _, err := ReduceIn(Env{Projects: ps}, &Thread{ID: "t_1"}, nil, in, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got.ProjectID != c.want || got.ProjectManual != c.manual {
			t.Fatalf("project %q: filed in %q manual=%v, want %q manual=%v", c.project, got.ProjectID, got.ProjectManual, c.want, c.manual)
		}
	}
}
