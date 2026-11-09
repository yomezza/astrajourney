package chatserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"golang.org/x/sys/unix"
)

// Native Linux test-only process. No diagnostics endpoint or browser protocol is
// added to the product. CLOCK_MONOTONIC is shared with the load-generator process.
func TestN1ProcessChild(t *testing.T) {
	if os.Getenv("N1_PROCESS_CHILD") != "1" {
		t.Skip("owned benchmark helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cfg, e := LoadConfig(os.Getenv)
	if e != nil {
		t.Fatal(e)
	}
	pc, _, e := postgres.Config(cfg.DatabaseURL, cfg.Schema)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasSuffix(pc.ConnConfig.Database, "_test") || !strings.HasPrefix(os.Getenv("N1_RUNTIME_ROLE"), "n1rt_") {
		t.Fatal("disposable database/runtime role required")
	}
	pc.MaxConns = 32
	pc.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, e := c.Exec(ctx, `SET ROLE `+pgx.Identifier{os.Getenv("N1_RUNTIME_ROLE")}.Sanitize())
		return e
	}
	db, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &chat.Service{DB: db}
	j, e := chat.OpenJournal(ctx, s, cfg.Journal)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	s.Journal = j
	a, e := New(s, cfg)
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	emit := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		if e := json.NewEncoder(os.Stdout).Encode(v); e != nil {
			cancel()
		}
	}
	mono := func() int64 {
		var ts unix.Timespec
		if e := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); e != nil {
			panic(e)
		}
		return ts.Nano()
	}
	var unsubscribe func()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.Serve(ctx, l, func() {
			unsubscribe = s.ObserveCommits(func(kind string) {
				at := mono()
				if a.source != nil {
					a.source.AfterCommit()
				}
				if kind == "mutation" {
					emit(map[string]any{"probe": "commit", "at": at})
				}
			})
			close(ready)
			emit(map[string]any{"probe": "started", "url": "http://" + l.Addr().String()})
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatal("helper startup", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "sample":
			var usage unix.Rusage
			if e = unix.Getrusage(unix.RUSAGE_SELF, &usage); e != nil {
				t.Fatal(e)
			}
			memory, _ := os.ReadFile("/proc/self/statm")
			rss := int64(0)
			if fields := strings.Fields(string(memory)); len(fields) > 1 {
				pages, _ := strconv.ParseInt(fields[1], 10, 64)
				rss = pages * int64(os.Getpagesize())
			}
			emit(map[string]any{"probe": "sample", "at": mono(), "cpu_ns": usage.Utime.Nano() + usage.Stime.Nano(), "rss_bytes": rss, "metrics": a.Diagnostics()})
		case "stop":
			cancel()
		default:
			t.Error("unknown test probe command")
			cancel()
		}
		if ctx.Err() != nil {
			break
		}
	}
	cancel()
	if unsubscribe != nil {
		unsubscribe()
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	fmt.Println("N1_HELPER_STOPPED")
}
