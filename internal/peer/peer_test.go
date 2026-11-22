package peer

// SPEC V0.158 (decision 0030 3.6): condition 1, case by case.

import (
	"errors"
	"net/http"
	"testing"

	"github.com/meros/fyr/internal/tailnet"
	"github.com/meros/fyr/internal/testkit/authtest"
)

func TestCondition1(t *testing.T) {
	ca, err := authtest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	answer := `{"fyr":1,"name":"b","id":"ABCD-EFGH-IJKL-MNOP","peer":7779,"owner":"owner@example.com"}`
	front, err := ca.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(answer)) }), "b.ts.net")
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	e := &Env{Serve: front.Client(), ServePort: 7443}
	self := tailnet.StatusNode{Name: "a.ts.net", UserID: 1, Login: "owner@example.com"}
	node := tailnet.StatusNode{Name: "b.ts.net", UserID: 1, Login: "owner@example.com", Online: true}
	ctx := t.Context()

	f, err := e.Condition1(ctx, self, node)
	if err != nil || f.ID != "ABCD-EFGH-IJKL-MNOP" || f.Port != 7779 || f.Name != "b" {
		t.Fatalf("the same user, untagged: %+v %v", f, err)
	}
	for name, c := range map[string]struct {
		self, node tailnet.StatusNode
		want       error
	}{
		"another user":        {self, with(node, func(n *tailnet.StatusNode) { n.UserID = 2 }), ErrNotSameUser},
		"a tagged node":       {self, with(node, func(n *tailnet.StatusNode) { n.Tags = []string{"tag:server"} }), ErrTagged},
		"this node is tagged": {with(self, func(n *tailnet.StatusNode) { n.Tags = []string{"tag:server"} }), node, ErrTagged},
		"shared in":           {self, with(node, func(n *tailnet.StatusNode) { n.Shared = true }), ErrShared},
		"expired":             {self, with(node, func(n *tailnet.StatusNode) { n.Expired = true }), ErrExpired},
	} {
		if _, err := e.Condition1(ctx, c.self, c.node); !errors.Is(err, c.want) {
			t.Errorf("%s: %v (want %v)", name, err, c.want)
		}
	}
	// Serve does not answer for the node (a name its certificate does not carry)
	other := with(node, func(n *tailnet.StatusNode) { n.Name = "c.ts.net" })
	if _, err := e.Condition1(ctx, self, other); !errors.Is(err, ErrNoServe) {
		t.Errorf("a node with another name at the same address: %v", err)
	}
	// Serve answers something that is not Fyr, or no peer port
	for _, bad := range []string{`<html>`, `{"fyr":1,"id":"X"}`, `{"fyr":2,"id":"X","peer":1}`, `{"fyr":1,"peer":5}`} {
		answer = bad
		if _, err := e.Condition1(ctx, self, node); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// V0.owner-without-tailnet (#197): when OwnerLogin is wired, Condition 1
	// requires a configured owner matching self.Login.
	owner := ""
	e.OwnerLogin = func() string { return owner }
	answer = `{"fyr":1,"name":"b","id":"ABCD-EFGH-IJKL-MNOP","peer":7779,"owner":"owner@example.com"}`
	if _, err := e.Condition1(ctx, self, node); !errors.Is(err, ErrNoOwner) {
		t.Errorf("no owner configured: %v (want ErrNoOwner)", err)
	}
	owner = "someone-else@example.com"
	if _, err := e.Condition1(ctx, self, node); !errors.Is(err, ErrNotSameUser) {
		t.Errorf("mismatched owner: %v (want ErrNotSameUser)", err)
	}
	owner = "owner@example.com"
	if f, err := e.Condition1(ctx, self, node); err != nil || f.ID != "ABCD-EFGH-IJKL-MNOP" {
		t.Errorf("matching owner: %+v %v", f, err)
	}
}

func with(n tailnet.StatusNode, f func(*tailnet.StatusNode)) tailnet.StatusNode { f(&n); return n }
