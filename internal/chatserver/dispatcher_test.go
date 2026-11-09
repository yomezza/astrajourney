package chatserver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/chat"
)

func testHandle(t *testing.T, r *WSRegistry, id, gen int64) *WSHandle {
	t.Helper()
	h, ok := r.Register(chat.Identity{ID: id}, chat.Lease{User: id, Generation: gen}, func() {})
	if !ok {
		t.Fatal("register")
	}
	return h
}
func TestRegistryGenerationAndBoundedSync(t *testing.T) {
	r := NewWSRegistry()
	old := testHandle(t, r, 1, 1)
	for i := 0; i < 1000; i++ {
		r.Notify(old)
	}
	if len(old.Sync) != 1 || r.coalesced.Load() != 999 {
		t.Fatal("unbounded or missing coalescing")
	}
	fresh := testHandle(t, r, 1, 2)
	r.Unregister(old)
	if !r.Current(fresh) || r.Notify(old) {
		t.Fatal("stale generation replaced current handle")
	}
	for i := int64(2); i <= 256; i++ {
		testHandle(t, r, i, 1)
	}
	if _, ok := r.Register(chat.Identity{ID: 257}, chat.Lease{User: 257, Generation: 1}, func() {}); ok {
		t.Fatal("capacity")
	}
	r.Close()
	if r.Notify(fresh) || len(r.Snapshot()) != 0 {
		t.Fatal("closed registry")
	}
}
func TestRegistryConcurrentReplacement(t *testing.T) {
	r := NewWSRegistry()
	var wg sync.WaitGroup
	var generation atomic.Int64
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				h, ok := r.Register(chat.Identity{ID: 1}, chat.Lease{User: 1, Generation: generation.Add(1)}, func() {})
				if ok {
					r.Notify(h)
					r.Unregister(h)
				}
			}
		}()
	}
	wg.Wait()
	r.Close()
}

type fakeDispatch struct {
	mu       sync.Mutex
	events   []chat.DispatchEvent
	fail     bool
	resolved int
}

func (f *fakeDispatch) OutboxWindow(_ context.Context, overlap time.Duration) (chat.DispatchWindow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, e := range f.events {
		n = max(n, e.ID)
	}
	return chat.DispatchWindow{Max: n, Cutoff: time.Now().Add(-overlap)}, nil
}
func (f *fakeDispatch) OutboxPage(_ context.Context, w chat.DispatchWindow, water, after int64, limit int) ([]chat.DispatchEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []chat.DispatchEvent{}
	for _, e := range f.events {
		if e.ID > after && e.ID <= w.Max && (e.ID > water || !e.Created.Before(w.Cutoff)) {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
func (f *fakeDispatch) ResolveRecipients(_ context.Context, e []chat.DispatchEvent, c []chat.DispatchConnection) (map[int64]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("db unavailable")
	}
	f.resolved += len(e)
	return map[int64]bool{1: true}, nil
}
func TestDispatcherLateCommitRetryAndPaging(t *testing.T) {
	f := &fakeDispatch{}
	r := NewWSRegistry()
	h := testHandle(t, r, 1, 1)
	d := NewEventDispatcher(f, r, NewLocalWakeSource(), DefaultDispatcherConfig())
	now := time.Now()
	f.events = []chat.DispatchEvent{{ID: 1, Created: now}, {ID: 3, Created: now}}
	f.fail = true
	if _, e := d.step(context.Background()); e == nil || d.highwater != 0 || d.initialized {
		t.Fatal("initial failure advanced cursor")
	}
	d.cycle = nil
	f.fail = false
	if _, e := d.step(context.Background()); e != nil {
		t.Fatal(e)
	}
	<-h.Sync
	// ID 2 becomes visible after ID 3 was already processed.
	f.events = []chat.DispatchEvent{{ID: 1, Created: now}, {ID: 2, Created: now}, {ID: 3, Created: now}}
	f.fail = true
	if _, e := d.step(context.Background()); e == nil || d.highwater != 3 || d.seen[2] {
		t.Fatal("failed read advanced delivery")
	}
	d.cycle = nil
	f.fail = false
	if _, e := d.step(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(h.Sync) != 1 || !d.seen[2] || f.resolved != 3 {
		t.Fatal("late lower ID lost or duplicate resolution")
	}
	<-h.Sync
	for i := int64(4); i <= 1504; i++ {
		f.events = append(f.events, chat.DispatchEvent{ID: i, Created: now})
	}
	more, e := d.step(context.Background())
	if e != nil || !more || d.highwater != 3 {
		t.Fatal("partial page committed cursor", more, e)
	}
	more, e = d.step(context.Background())
	if e != nil || more || d.highwater != 1504 || len(h.Sync) != 1 {
		t.Fatal("paging or mailbox", more, e)
	}
}

func TestDispatcherReconcilePastOverlapAndBoundedDedupe(t *testing.T) {
	f := &fakeDispatch{events: []chat.DispatchEvent{{ID: 1, Created: time.Now()}}}
	r := NewWSRegistry()
	h := testHandle(t, r, 1, 1)
	d := NewEventDispatcher(f, r, NewLocalWakeSource(), DefaultDispatcherConfig())
	if _, e := d.step(context.Background()); e != nil {
		t.Fatal(e)
	}
	<-h.Sync
	f.events[0].Created = time.Now().Add(-2 * time.Minute)
	d.reconcile = true
	if _, e := d.step(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(h.Sync) != 1 || d.reconcile {
		t.Fatal("recovery failed to refresh current sessions outside overlap")
	}
	for id := int64(2); id < 100000; id++ {
		d.remember(id)
	}
	if len(d.seen) > 32768 || len(d.ring) > 32768 {
		t.Fatal("dedupe memory unbounded")
	}
}

type advancingBackend struct {
	*fakeDispatch
	source *LocalWakeSource
	once   sync.Once
}

func (f *advancingBackend) OutboxPage(ctx context.Context, w chat.DispatchWindow, water, after int64, limit int) ([]chat.DispatchEvent, error) {
	rows, e := f.fakeDispatch.OutboxPage(ctx, w, water, after, limit)
	f.once.Do(func() {
		f.mu.Lock()
		f.events = append(f.events, chat.DispatchEvent{ID: 1501, Created: time.Now()})
		f.mu.Unlock()
		f.source.AfterCommit()
	})
	return rows, e
}
func TestDispatcherWakeDuringPagedWindow(t *testing.T) {
	s := NewLocalWakeSource()
	f := &advancingBackend{fakeDispatch: &fakeDispatch{}, source: s}
	for i := int64(1); i <= 1500; i++ {
		f.events = append(f.events, chat.DispatchEvent{ID: i, Created: time.Now()})
	}
	r := NewWSRegistry()
	testHandle(t, r, 1, 1)
	d := NewEventDispatcher(f, r, s, DefaultDispatcherConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.Run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(time.Second)
	for d.water.Load() != 1501 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if d.water.Load() != 1501 {
		t.Fatal("wake lost while paging old snapshot")
	}
}
func TestDispatcherFallbackAndShutdown(t *testing.T) {
	f := &fakeDispatch{events: []chat.DispatchEvent{{ID: 1, Created: time.Now()}}}
	r := NewWSRegistry()
	h := testHandle(t, r, 1, 1)
	s := NewLocalWakeSource()
	c := DefaultDispatcherConfig()
	c.PollInterval = 10 * time.Millisecond
	c.DegradedInterval = 5 * time.Millisecond
	d := NewEventDispatcher(f, r, s, c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.Run(ctx) }()
	select {
	case <-h.Sync:
	case <-time.After(time.Second):
		t.Fatal("initial reconcile")
	}
	f.mu.Lock()
	f.fail = true
	f.events = append(f.events, chat.DispatchEvent{ID: 2, Created: time.Now()})
	f.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for d.failures.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if d.failures.Load() == 0 {
		t.Fatal("DB failure not detected")
	}
	f.mu.Lock()
	f.fail = false
	f.mu.Unlock()
	select {
	case <-h.Sync:
	case <-time.After(time.Second):
		t.Fatal("fallback missed commit without local wake")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatcher goroutine leaked")
	}
}
func TestDispatcherConfigCompatibility(t *testing.T) {
	for _, mode := range []string{"", "legacy", "A1", "a1"} {
		c, e := dispatcherConfig(func(k string) string {
			if k == "CHAT_DISPATCHER_MODE" {
				return mode
			}
			return ""
		})
		if e != nil || c.Mode == "" {
			t.Fatal(mode, e)
		}
	}
	for key, value := range map[string]string{"CHAT_DISPATCHER_MODE": "b1", "CHAT_FALLBACK_POLL_INTERVAL": "0s", "CHAT_DEGRADED_POLL_INTERVAL": "30s", "CHAT_OUTBOX_OVERLAP": "15s"} {
		if _, e := dispatcherConfig(func(k string) string {
			if k == key {
				return value
			}
			return ""
		}); e == nil {
			t.Fatal("invalid setting accepted", key)
		}
	}
}
