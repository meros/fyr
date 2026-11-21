package github

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// V0.pr-create-tool: CreatePR builds safe argv for gh pr create and extracts
// the canonical pull request URL from multi-line output.
func TestCreatePR(t *testing.T) {
	var got []string
	r := func(_ context.Context, args ...string) ([]byte, error) {
		got = slices.Clone(args)
		return []byte("Creating pull request for feat/x into main in acme/app\n\nhttps://github.com/acme/app/pull/42\n"), nil
	}
	url, err := CreatePR(context.Background(), r, CreatePRReq{
		Dir:       "/repo",
		Title:     "Add feature X",
		Body:      "In short, adds X.",
		Head:      "feat/x",
		Base:      "main",
		Draft:     true,
		Labels:    []string{"area:core", "type:work"},
		Reviewers: []string{"alice"},
	})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if url != "https://github.com/acme/app/pull/42" {
		t.Fatalf("url = %q", url)
	}
	want := []string{
		"pr", "create",
		"--title", "Add feature X",
		"--body", "In short, adds X.",
		"--head", "feat/x",
		"--base", "main",
		"--draft",
		"--label", "area:core",
		"--label", "type:work",
		"--reviewer", "alice",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %v\nwant %v", got, want)
	}
}

func TestCreatePRValidation(t *testing.T) {
	never := func(_ context.Context, _ ...string) ([]byte, error) {
		t.Fatal("runner should not be called on invalid input")
		return nil, nil
	}
	cases := []struct {
		name string
		req  CreatePRReq
	}{
		{"empty title", CreatePRReq{Dir: "/repo", Body: "body"}},
		{"empty dir", CreatePRReq{Title: "T", Body: "body"}},
		{"dash head", CreatePRReq{Dir: "/repo", Title: "T", Head: "--repo=evil/r"}},
		{"dash base", CreatePRReq{Dir: "/repo", Title: "T", Base: "-x"}},
		{"dash label", CreatePRReq{Dir: "/repo", Title: "T", Labels: []string{"--draft"}}},
		{"dash reviewer", CreatePRReq{Dir: "/repo", Title: "T", Reviewers: []string{"-u"}}},
	}
	for _, tc := range cases {
		if _, err := CreatePR(context.Background(), never, tc.req); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	noURL := func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("something unexpected"), nil
	}
	if _, err := CreatePR(context.Background(), noURL, CreatePRReq{Dir: "/repo", Title: "T"}); err == nil || !strings.Contains(err.Error(), "no pull request URL") {
		t.Fatalf("expected missing URL error, got %v", err)
	}
}
