package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secretKey = "sk-live-NEVERSHOWN-4242"

// V0.120 (1): an endpoint is asked at <base>/v1/models with its key in the
// Anthropic style; pages are followed.
func TestFetchModelsAnthropicStyleAndPaging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/models" || r.Header.Get("x-api-key") != secretKey || r.Header.Get("anthropic-version") == "" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Query().Get("after_id") {
		case "":
			fmt.Fprint(w, `{"data":[{"id":"m1","display_name":"Model One"},{"id":"m2"}],"has_more":true,"last_id":"m2"}`)
		case "m2":
			fmt.Fprint(w, `{"data":[{"id":"m3","name":"Three"}],"has_more":false}`)
		}
	}))
	defer srv.Close()
	ms, err := fetchModels(context.Background(), srv.Client(), srv.URL+"/anthropic", secretKey, fetchLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || ms[0].Name != "Model One" || ms[2].Name != "Three" || ms[1].ID != "m2" {
		t.Fatalf("%+v", ms)
	}
}

// When x-api-key answers 401 or 404, the key goes as a Bearer token.
func TestFetchModelsBearerFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secretKey {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"z-ai/glm","name":"GLM"}]}`)
	}))
	defer srv.Close()
	ms, err := fetchModels(context.Background(), srv.Client(), srv.URL, secretKey, fetchLimits{})
	if err != nil || len(ms) != 1 || ms[0].ID != "z-ai/glm" {
		t.Fatalf("%v %+v", err, ms)
	}
}

// The limits: 2 MB a page, 15 s, no redirect to another host, and the key
// in no error text.
func TestFetchModelsLimits(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"stolen"}]}`)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/big"):
			w.Write([]byte(`{"data":[{"id":"` + strings.Repeat("x", 3<<20) + `"}]}`))
		case strings.HasPrefix(r.URL.Path, "/slow"):
			time.Sleep(300 * time.Millisecond)
			fmt.Fprint(w, `{"data":[]}`)
		case strings.HasPrefix(r.URL.Path, "/away"):
			http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/echo"):
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"bad key `+secretKey+`"}`)
		}
	}))
	defer srv.Close()
	lim := fetchLimits{Timeout: 100 * time.Millisecond}
	for _, base := range []string{"/big", "/slow", "/away", "/echo"} {
		_, err := fetchModels(context.Background(), srv.Client(), srv.URL+base, secretKey, lim)
		if err == nil {
			t.Fatalf("%s: no error", base)
		}
		if strings.Contains(err.Error(), secretKey) || strings.Contains(err.Error(), "NEVERSHOWN") {
			t.Fatalf("%s: key in error %q", base, err)
		}
	}
	// A redirect on the same host is followed.
	same := httptest.NewServer(nil)
	defer same.Close()
	same.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old/v1/models" {
			http.Redirect(w, r, "/new/v1/models", http.StatusMovedPermanently)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"ok"}]}`)
	})
	if ms, err := fetchModels(context.Background(), same.Client(), same.URL+"/old", secretKey, lim); err != nil || len(ms) != 1 {
		t.Fatalf("same-host redirect: %v %+v", err, ms)
	}
}
