package httpguard

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// DefaultClientTimeout bounds a whole request of a client from NewClient.
const DefaultClientTimeout = 30 * time.Second

// NewClient is an HTTP client with a timeout, for callers that need no more.
func NewClient() *http.Client { return &http.Client{Timeout: DefaultClientTimeout} }

// SmallBody is the cap for the body of a call whose fields are a few short
// strings and flags.
const SmallBody = 4 << 10

// ReadJSON reads the request body into v, at most limit bytes, and returns the
// error without answering: the caller picks the status and text.
func ReadJSON(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
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
