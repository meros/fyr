package server

import (
	"encoding/json"
	"testing"
)

// An unset value is left out of /state; a set one is sent whole.
func TestPresent(t *testing.T) {
	type st struct {
		A any `json:"a,omitempty"`
		B any `json:"b,omitempty"`
	}
	b, _ := json.Marshal(st{A: present(struct{ X int }{3}, true), B: present(struct{ X int }{}, false)})
	if string(b) != `{"a":{"X":3}}` {
		t.Fatal(string(b))
	}
}
