package httpguard

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

// DefaultClientTimeout bounds a whole request of a client from NewClient.
const DefaultClientTimeout = 30 * time.Second

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

// DecodeJSON reads the request body into v like ReadJSON and answers a failure
// itself: 413 when the body is over limit, else 400 with badText. It reports
// whether v was filled.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any, limit int64, badText string) bool {
	err := ReadJSON(w, r, v, limit)
	if err == nil {
		return true
	}
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
	} else {
		http.Error(w, badText, http.StatusBadRequest)
	}
	return false
}
