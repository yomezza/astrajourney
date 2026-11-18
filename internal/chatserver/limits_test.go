package chatserver

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdmissionAbsorbsBurstWithoutExpandingActiveWork(t *testing.T) {
	var active, peak, rejected atomic.Int64
	entered := make(chan struct{}, 100)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	h := bounded(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		w.WriteHeader(200)
	}))
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/chat/api/v1/bootstrap", nil))
			if w.Code != 200 {
				rejected.Add(1)
			}
		}()
	}
	for range 64 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("active handlers did not start")
		}
	}
	unblock()
	wg.Wait()
	if rejected.Load() != 0 || peak.Load() > 64 {
		t.Fatal("burst lost or concurrency bound exceeded", rejected.Load(), peak.Load())
	}
}

func TestAdmissionStillRejectsPersistentOverload(t *testing.T) {
	entered := make(chan struct{}, 64)
	release := make(chan struct{})
	defer close(release)
	h := bounded(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
	}))
	for range 64 {
		go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/chat/api/v1/bootstrap", nil))
	}
	for range 64 {
		<-entered
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/chat/api/v1/history", nil))
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("persistent overload must remain bounded", w.Code, w.Header())
	}
}
