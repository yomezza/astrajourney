package httpkit

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"
)

// StaticContent owns immutable bytes and their prepared gzip representation.
// The caller must not modify body after construction. Headers such as content
// type and cache policy belong to the handler, not this representation.
type StaticContent struct {
	body, compressed []byte
	etag             string
}

func NewStaticContent(body []byte) *StaticContent {
	hash := sha256.Sum256(body)
	var compressed bytes.Buffer
	writer := jsonGzipWriters.Get().(*gzip.Writer)
	writer.Reset(&compressed)
	_, _ = writer.Write(body)
	_ = writer.Close()
	writer.Reset(io.Discard)
	jsonGzipWriters.Put(writer)
	return &StaticContent{body: body, compressed: compressed.Bytes(), etag: `"` + hex.EncodeToString(hash[:]) + `"`}
}

// Size counts retained buffer capacity, including unused allocation space.
func (c *StaticContent) Size() int { return cap(c.body) + cap(c.compressed) }

func (c *StaticContent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	body, etag := c.body, c.etag
	// Ranges address the original representation. Never reinterpret their byte
	// offsets as offsets into a compressed stream.
	if r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		body, etag = c.compressed, "W/"+c.etag
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.Header().Set("ETag", etag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}
