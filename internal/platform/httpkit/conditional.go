package httpkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// ReplyConditional must only be called after fresh authorization. Private
// callers keep Cache-Control:no-store; clients may revalidate their own bounded
// in-memory copy, but a tag is never an access token or an authorization cache.
func ReplyConditional(w http.ResponseWriter, r *http.Request, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		Internal(w, err)
		return
	}
	hash := sha256.Sum256(raw)
	// The digest describes the JSON value, independent of content encoding.
	tag := `W/"` + hex.EncodeToString(hash[:]) + `"`
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
