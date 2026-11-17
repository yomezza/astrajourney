package cdnserver

import (
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"io"
	"net/http"
	"runtime"
	"sync/atomic"
)

type counters struct{ Requests, Bytes, NotModified, Missing, MediaHints, InvalidHints atomic.Uint64 }
type responseCount struct {
	http.ResponseWriter
	status int
	bytes  uint64
}

func (w *responseCount) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseCount) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += uint64(n)
	return n, err
}
func (w *responseCount) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseCount) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	var n int64
	var err error
	if target, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err = target.ReadFrom(r)
	} else {
		n, err = io.Copy(w.ResponseWriter, r)
	}
	w.bytes += uint64(n)
	return n, err
}
func (s *Service) record(w *responseCount) {
	s.count.Requests.Add(1)
	s.count.Bytes.Add(w.bytes)
	if w.status == 304 {
		s.count.NotModified.Add(1)
	}
}
func (s *Service) metrics(w http.ResponseWriter, r *http.Request) {
	if !mediastore.Authorized(r, s.token) {
		httpkit.Fail(w, 403, "service authentication required")
		return
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	httpkit.Reply(w, 200, map[string]any{"requests": s.count.Requests.Load(), "bytes": s.count.Bytes.Load(), "not_modified": s.count.NotModified.Load(), "missing": s.count.Missing.Load(), "media_hints": s.count.MediaHints.Load(), "invalid_hints": s.count.InvalidHints.Load(), "heap_bytes": memory.HeapAlloc, "goroutines": runtime.NumGoroutine(), "nats": s.nats.Diagnostics()})
}
