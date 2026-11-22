package httpguard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

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
