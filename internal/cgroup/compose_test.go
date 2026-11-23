package cgroup

import "testing"

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
