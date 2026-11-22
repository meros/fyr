package httpguard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	var v struct{ A int }
	w := httptest.NewRecorder()
	if !DecodeJSON(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"A":2}`)), &v) || v.A != 2 {
		t.Fatal("valid body refused")
	}
	w = httptest.NewRecorder()
	if DecodeJSON(w, httptest.NewRequest("POST", "/", strings.NewReader(`{`)), &v) || w.Code != 400 {
		t.Fatalf("bad body: %d", w.Code)
	}
	w = httptest.NewRecorder()
	big := `{"A":1,"x":"` + strings.Repeat("a", 100) + `"}`
	if DecodeJSONLimit(w, httptest.NewRequest("POST", "/", strings.NewReader(big)), &v, 10) || w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: %d", w.Code)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, 201, map[string]int{"a": 1})
	if w.Code != 201 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestDecodeLooseAndRead(t *testing.T) {
	var v struct{ A int }
	w := httptest.NewRecorder()
	DecodeLoose(w, httptest.NewRequest("POST", "/", strings.NewReader("")), &v, 10)
	if v.A != 0 || w.Code != 200 {
		t.Fatal("empty body must be left alone")
	}
	DecodeLoose(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"A":3}`)), &v, 100)
	if v.A != 3 {
		t.Fatal("not decoded")
	}
	if ReadJSON(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"A":`+strings.Repeat("1", 50)+`}`)), &v, 10) == nil {
		t.Fatal("cap not enforced")
	}
}
