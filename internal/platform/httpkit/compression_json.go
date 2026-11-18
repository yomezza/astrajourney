package httpkit

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

type jsonGzipResponse struct {
	http.ResponseWriter
	stream *gzip.Writer
	wrote  bool
}

var jsonGzipWriters = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return w
}}

func (w *jsonGzipResponse) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified && w.Header().Get("Content-Encoding") == "" && strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.stream = jsonGzipWriters.Get().(*gzip.Writer)
		w.stream.Reset(w.ResponseWriter)
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *jsonGzipResponse) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.stream != nil {
		return w.stream.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

// GzipJSON compresses JSON HTTP views without buffering WebSocket upgrades or
// writing a gzip body for 304/204 responses. Writers are reused across requests.
func GzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.Header.Get("Upgrade") == "" {
			w.Header().Add("Vary", "Accept-Encoding")
			if acceptsGzip(r.Header.Get("Accept-Encoding")) {
				response := &jsonGzipResponse{ResponseWriter: w}
				defer func() {
					if response.stream != nil {
						_ = response.stream.Close()
						response.stream.Reset(io.Discard)
						jsonGzipWriters.Put(response.stream)
					}
				}()
				next.ServeHTTP(response, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
