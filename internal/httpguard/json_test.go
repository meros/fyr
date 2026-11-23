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

func TestDecodeJSONAnswersTooLargeAndBad(t *testing.T) {
	for _, c := range []struct {
		body string
		want int
	}{{`{"a":1}`, 0}, {`{"a":`, 400}, {`{"a":"` + strings.Repeat("x", 100) + `"}`, 413}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(c.body))
		var v map[string]any
		ok := DecodeJSON(w, r, &v, 32, "bad")
		if ok != (c.want == 0) || (c.want != 0 && w.Code != c.want) {
			t.Fatalf("%q: ok=%v code=%d", c.body, ok, w.Code)
		}
	}
}
