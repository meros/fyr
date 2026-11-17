package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// http answers like gh api -i: a head and a body.
func http(status int, etag, body string) string {
	s := "HTTP/2.0 200 OK"
	if status == 304 {
		s = "HTTP/2.0 304 Not Modified"
	}
	h := s + "\r\nContent-Type: application/json\r\n"
	if etag != "" {
		h += "Etag: " + etag + "\r\n"
	}
	return h + "\r\n" + body
}

// fakeAPI serves REST paths with ETags; a request with the current ETag is
// answered 304 (gh exits 1 on it, as gh api does for a status >= 300).
type fakeAPI struct {
	body  map[string]string
	calls []string
	cost  int // calls GitHub counts: not the 304s
}

func (f *fakeAPI) run(_ context.Context, args ...string) ([]byte, error) {
	j := strings.Join(args, " ")
	f.calls = append(f.calls, j)
	path := args[len(args)-1]
	b, ok := f.body[path]
	if !ok {
		return nil, errors.New("unexpected gh " + j)
	}
	tag := `W/"` + b + `"`
	if strings.Contains(j, "If-None-Match: "+tag) {
		return []byte(http(304, tag, "")), errors.New("gh: HTTP 304")
	}
	f.cost++
	return []byte(http(200, tag, b)), nil
}

// V0.173: a 304 costs nothing: the second read of an unchanged path sends
// the ETag and gets the stored body back.
func TestETagSecondReadIsFree(t *testing.T) {
	resetETags()
	f := &fakeAPI{body: map[string]string{"repos/o/r/x": `[1]`}}
	for i := 0; i < 3; i++ {
		out, err := RestGet(context.Background(), f.run, "repos/o/r/x")
		if err != nil || string(out) != `[1]` {
			t.Fatalf("read %d: %q %v", i, out, err)
		}
	}
	if f.cost != 1 || len(f.calls) != 3 {
		t.Fatalf("cost %d calls %v", f.cost, f.calls)
	}
	f.body["repos/o/r/x"] = `[2]`
	if out, _ := RestGet(context.Background(), f.run, "repos/o/r/x"); string(out) != `[2]` || f.cost != 2 {
		t.Fatalf("changed: %q cost %d", out, f.cost)
	}
}

// V0.173: the meter counts a 304 as free.
func TestMeterCounts304AsFree(t *testing.T) {
	resetETags()
	f := &fakeAPI{body: map[string]string{"repos/o/r/deployments?sha=m&per_page=30": `[]`}}
	m := &Meter{}
	gh := m.Wrap(f.run)
	RestGet(context.Background(), gh, "repos/o/r/deployments?sha=m&per_page=30")
	RestGet(context.Background(), gh, "repos/o/r/deployments?sha=m&per_page=30")
	v := m.View()
	if len(v.Uses) != 1 || v.Uses[0] != (Use{Kind: "rest deployments", Repo: "o/r", Calls: 2, Free: 1}) || v.Calls != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestKind(t *testing.T) {
	for _, c := range []struct {
		args       []string
		kind, repo string
	}{
		{[]string{"api", "-i", "-H", "If-None-Match: x", "repos/o/r/deployments/12/statuses?per_page=1"}, "rest deployments/:id/statuses", "o/r"},
		{[]string{"api", "repos/o/r/commits/0123abc/check-runs?per_page=100"}, "rest commits/:id/check-runs", "o/r"},
		{[]string{"api", "repos/o/r", "--jq", ".default_branch"}, "rest repo", "o/r"},
		{[]string{"api", "graphql", "-f", "query=q", "-F", "owner=o", "-F", "name=r"}, "graphql", "o/r"},
		{[]string{"pr", "view", "--json", "state", "--", "https://github.com/o/r/pull/3"}, "graphql pr view", "o/r"},
		{[]string{"run", "list", "--repo", "o/r"}, "rest run list", "o/r"},
		{[]string{"release", "download", "v1", "-R", "o/r"}, "rest release download", "o/r"},
	} {
		k, r := Kind(c.args)
		if k != c.kind || r != c.repo {
			t.Errorf("%v: %q %q, want %q %q", c.args, k, r, c.kind, c.repo)
		}
	}
}

// V0.173: the hourly line names the top kinds and the account's rate.
func TestMeterRollSummary(t *testing.T) {
	at := time.Date(2026, 11, 17, 19, 0, 0, 0, time.Local)
	m := &Meter{Now: func() time.Time { return at }}
	m.Count("rest deployments", "o/r", false)
	m.Count("rest deployments", "o/r", true)
	m.Count("graphql pr view", "o/r", false)
	m.SetRate(Rate{Limit: 5000, Remaining: 4000, Reset: at.Add(30 * time.Minute)})
	at = at.Add(time.Hour)
	s := m.Roll()
	for _, w := range []string{"gh calls 19:00–20:00: 3 (1 counted REST)", "rest deployments o/r 2 (1 free)", "graphql pr view o/r 1", "account REST 4000 of 5000 left, resets 19:30"} {
		if !strings.Contains(s, w) {
			t.Fatalf("%q lacks %q", s, w)
		}
	}
	if v := m.View(); len(v.Uses) != 2 {
		t.Fatalf("after the roll the view shows the last hour: %+v", v)
	}
}

func TestReadRate(t *testing.T) {
	gh := func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "api rate_limit" {
			t.Fatalf("%v", args)
		}
		return []byte(`{"resources":{"core":{"limit":5000,"remaining":12,"reset":1763409057},"graphql":{"remaining":4966}}}`), nil
	}
	r, err := ReadRate(context.Background(), gh, time.Now())
	if err != nil || r.Limit != 5000 || r.Remaining != 12 || r.GraphQL != 4966 || r.Reset.Unix() != 1763409057 {
		t.Fatalf("%+v %v", r, err)
	}
}
