package httpguard

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

// DefaultBodyLimit caps a request body DecodeJSON reads.
const DefaultBodyLimit int64 = 1 << 20

// DefaultClientTimeout bounds a whole request of a client from NewClient.
const DefaultClientTimeout = 30 * time.Second

// WriteJSON answers with code and v as JSON.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("httpguard: write json: %v", err)
	}
}

// DecodeJSON reads the request body into v, at most DefaultBodyLimit bytes.
// On failure it answers 400 (413 for a body over the limit) and returns false.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return DecodeJSONLimit(w, r, v, DefaultBodyLimit)
}

// DecodeJSONLimit is DecodeJSON with its own cap.
func DecodeJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
	if err == nil {
		return true
	}
	if _, big := err.(*http.MaxBytesError); big {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
	} else {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
	}
	return false
}

// NewClient is an HTTP client with a timeout, for callers that need no more.
func NewClient() *http.Client { return &http.Client{Timeout: DefaultClientTimeout} }

// ReadJSON reads the request body into v, at most limit bytes, and returns the
// error without answering: the caller picks the status and text.
func ReadJSON(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
}

// DecodeLoose is for a call whose fields are all optional: an empty body is
// fine and leaves v as it was, and the handler's own validation refuses what
// is missing. Any other failure (bad JSON, over the cap) is logged, and v keeps
// what was decoded before it.
func DecodeLoose(w http.ResponseWriter, r *http.Request, v any, limit int64) {
	if err := ReadJSON(w, r, v, limit); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("httpguard: %s %s body: %v", r.Method, r.URL.Path, err)
	}
}
