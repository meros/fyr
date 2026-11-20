package heavy

import "testing"

// V0.services-not-jobs: the service patterns and the compose project a command names.
func TestServicesMatch(t *testing.T) {
	s := &Services{}
	for cmd, want := range map[string]bool{
		"process-compose up": true,
		"process-compose --config process-compose.yaml --tui=false":                       true,
		"/nix/store/x/bin/process-compose -f pc.yaml":                                     true,
		"process-compose down":                                                            false,
		"docker compose -f devdocker/docker-compose.yaml --project-name acme-dev up -d": true,
		"docker compose logs -f":                                                          false,
		"docker-compose up":                                                               true,
		"npm run dev":                                                                     true,
		"cd web && yarn dev --host":                                                       true,
		"pnpm dev":                                                                        true,
		"node /w/node_modules/.bin/vite":                                                  true,
		"vite build":                                                                      false,
		"npm run build":                                                                   false,
		"curl -fsS http://127.0.0.1:8000/health":                                          false,
	} {
		if got := s.Match(cmd); got != want {
			t.Errorf("%q: %v", cmd, got)
		}
	}
}

func TestServicesEdit(t *testing.T) {
	kv := mapKV{}
	s := &Services{KV: kv}
	s.Load()
	if err := s.Edit(`(^|[\s/])rails\s+server\b`, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Edit("", DefaultServices[3]); err != nil {
		t.Fatal(err)
	}
	if !s.Match("bin/rails server") || s.Match("npm run dev") {
		t.Fatal("edit not applied")
	}
	if s.Edit("(", "") == nil || s.Edit("", "nope") == nil {
		t.Fatal("a bad edit was taken")
	}
	t2 := &Services{KV: kv}
	t2.Load()
	if !t2.Match("bin/rails server") || t2.Match("npm run dev") {
		t.Fatal("not kept")
	}
	if err := t2.Edit(DefaultServices[3], ""); err != nil || !t2.Match("npm run dev") {
		t.Fatal("a removed built-in pattern was not put back")
	}
}

func TestComposeProject(t *testing.T) {
	cases := []struct {
		cmd, cwd, want string
		up, down       bool
	}{
		{"docker compose -f devdocker/docker-compose.yaml --project-name acme-dev up -d", "/w", "acme-dev", true, false},
		{"docker compose -p=Shop up", "/w", "shop", true, false},
		{"cd x && docker compose -f devdocker/docker-compose.yaml up", "/w/repo", "devdocker", true, false},
		{"COMPOSE_PROJECT_NAME=abc docker-compose up -d", "/w", "abc", true, false},
		{"docker compose down", "/home/me/My.App", "myapp", false, true},
		{"docker compose --project-directory /srv/stack ps", "", "stack", false, false},
		{"docker ps", "/w", "", false, false},
	}
	for _, c := range cases {
		p, up, down := ComposeProject(c.cmd, c.cwd)
		if p != c.want || up != c.up || down != c.down {
			t.Errorf("%q: %q up=%v down=%v", c.cmd, p, up, down)
		}
	}
}
