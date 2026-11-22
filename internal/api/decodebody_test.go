package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeBody(t *testing.T) {
	var v struct{ A int }
	w := httptest.NewRecorder()
	if !DecodeBody(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"A":2}`)), &v, 100) || v.A != 2 {
		t.Fatal("valid body refused")
	}
	w = httptest.NewRecorder()
	if DecodeBody(w, httptest.NewRequest("POST", "/", strings.NewReader(`{`)), &v, 100) || w.Code != 400 || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("bad body: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	big := `{"A":1,"x":"` + strings.Repeat("a", 100) + `"}`
	if DecodeBody(w, httptest.NewRequest("POST", "/", strings.NewReader(big)), &v, 10) || w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: %d", w.Code)
	}
}

type failWriter struct{ httptest.ResponseRecorder }

func (failWriter) Write([]byte) (int, error) { return 0, http.ErrHandlerTimeout }

func TestWriteJSONSurvivesAFailedWrite(t *testing.T) {
	WriteJSON(&failWriter{*httptest.NewRecorder()}, 200, map[string]int{"a": 1}) // logs, does not panic
}
