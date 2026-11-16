package core

import (
	"testing"
	"time"
)

// SPEC 0.5 V0.23: the default colour is stable for a name and never the beam.
func TestDefaultColor(t *testing.T) {
	if DefaultColor("norban") != DefaultColor("norban") {
		t.Fatal("not stable")
	}
	seen := map[string]bool{}
	for _, n := range []string{"norban", "wayout", "nixos", "fyr", "recept", "utdelningar", "a", "b", "c", "d", "e", "f"} {
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

func TestMonogram(t *testing.T) {
	cases := map[string]string{"norban": "N", "my-app": "MA", "fyr_web": "FW", "åsa": "Å", "": "?"}
	for in, want := range cases {
		if got := Monogram(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// SPEC 0.5 V0.55: a thread lands in the project whose folder is its folder
// or contains it; the longest folder wins, ties by id; a folder never
// covers a sibling with the same prefix.
func TestMatchFolder(t *testing.T) {
	ps := []*Project{
		{ID: "p_home", Folder: "/home/me"},
		{ID: "p_fyr", Folder: "/home/me/git/fyr"},
		{ID: "p_b", Folder: "/home/me/git/fyr/web/"},
		{ID: "p_a", Folder: "/home/me/git/fyr/web"},
		{ID: "p_label"},
	}
	cases := map[string]string{
		"/home/me/git/fyr":         "p_fyr",
		"/home/me/git/fyr/":        "p_fyr",
		"/home/me/git/fyr/cmd":     "p_fyr",
		"/home/me/git/fyr/web/src": "p_a",
		"/home/me/git/fyr-projcut": "p_home",
		"/home/me":                 "p_home",
		"/tmp/x":                   "",
		"":                         "",
	}
	for dir, want := range cases {
		if got := MatchFolder(ps, dir); got != want {
			t.Errorf("%q: got %q want %q", dir, got, want)
		}
	}
	if got := MatchFolder([]*Project{{ID: "p_root", Folder: "/"}}, "/srv/x"); got != "p_root" {
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
	old := &Project{ID: "p_old", Name: "norban", Folder: "/src/norban", Color: DefaultColor("norban")}
	if old.SetBy("name") != "" || old.SetBy("color") != "" {
		t.Fatalf("an old placeholder is the owner's")
	}
	old2 := &Project{ID: "p_old2", Name: "Norban", Folder: "/src/norban", Color: "teal"}
	if DefaultColor("Norban") == "teal" {
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
