package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/chat"
)

func TestChatConditionalViewsRevalidateRevokedSession(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	_, service, base, _ := n1App(t, f, "a1", nil)
	u, lease, cookie := n1User(t, f, service, "conditional_reader")
	n1Execute(t, service, u, lease, "message.send", map[string]any{"kind": "global", "body": "Current authority before conditional reads"})
	get := func(tag string) *http.Response {
		t.Helper()
		r, _ := http.NewRequest("GET", base+"/chat/api/v1/history?kind=global", nil)
		r.AddCookie(cookie)
		r.Header.Set("If-None-Match", tag)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	first := get("")
	_, _ = io.Copy(io.Discard, first.Body)
	first.Body.Close()
	tag := first.Header.Get("ETag")
	if first.StatusCode != 200 || tag == "" || first.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("initial private view", first.StatusCode, first.Header)
	}
	unchanged := get(tag)
	body, _ := io.ReadAll(unchanged.Body)
	unchanged.Body.Close()
	if unchanged.StatusCode != 304 || len(body) != 0 {
		t.Fatal("unchanged authorized view must be empty 304", unchanged.StatusCode)
	}
	f.exec(`DELETE FROM sessions WHERE user_id=$1`, u.ID)
	denied := get(tag)
	denied.Body.Close()
	if denied.StatusCode != 401 {
		t.Fatal("cached tag bypassed session revocation", denied.StatusCode)
	}
}

type outboxQueryCapture struct {
	mu   sync.Mutex
	sql  string
	args []any
	mode pgx.QueryExecMode
}

func (c *outboxQueryCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "FROM chat_outbox") && strings.Contains(data.SQL, "ORDER BY id LIMIT") {
		c.mu.Lock()
		c.sql, c.args = data.SQL, append([]any(nil), data.Args...)
		if len(c.args) > 0 {
			if mode, ok := c.args[0].(pgx.QueryExecMode); ok {
				c.mode, c.args = mode, c.args[1:]
			}
		}
		c.mu.Unlock()
	}
	return ctx
}
func (*outboxQueryCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestChatOutboxOverlapDoesNotScanRetainedHistory(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	// A caught-up dispatcher must revisit only its recent overlap, even when
	// retained history is much larger. The production query is captured from
	// pgx, rather than copied into this test's EXPLAIN statement.
	f.exec(`INSERT INTO chat_outbox(event_kind,created_at) SELECT 'channel.changed',clock_timestamp()-interval '1 day' FROM generate_series(1,200000)`)
	f.exec(`INSERT INTO chat_outbox(event_kind) SELECT 'channel.changed' FROM generate_series(1,200)`)
	f.exec(`ANALYZE chat_outbox`)
	capture := &outboxQueryCapture{}
	cfg := f.db.Config().Copy()
	cfg.ConnConfig.Tracer = capture
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := &chat.Service{DB: pool}
	window, err := service.OutboxWindow(f.ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := service.OutboxPage(f.ctx, window, window.Max, 0, 1000)
	if err != nil || len(rows) != 200 {
		t.Fatal("recent overlap", len(rows), err)
	}
	capture.mu.Lock()
	query, args, mode := capture.sql, capture.args, capture.mode
	capture.mu.Unlock()
	if query == "" {
		t.Fatal("outbox query not captured")
	}
	if mode != pgx.QueryExecModeExec {
		t.Fatal("moving overlap bounds must not reuse a named generic plan")
	}
	var raw []byte
	if err = f.db.QueryRow(f.ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []struct {
		Plan struct {
			Hits  int `json:"Shared Hit Blocks"`
			Reads int `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}
	if err = json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
		t.Fatal("EXPLAIN", err)
	}
	blocks := plan[0].Plan.Hits + plan[0].Plan.Reads
	t.Logf("200 recent / 200000 retained outbox rows: %d shared blocks", blocks)
	if blocks > 300 {
		t.Fatalf("overlap read grew with retained history: %d blocks", blocks)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(f.ctx, `SET LOCAL plan_cache_mode='force_generic_plan'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "PREPARE chat_overlap_check(bigint,bigint,bigint,timestamptz,bigint) AS "+query); err != nil {
		t.Fatal(err)
	}
	statement := fmt.Sprintf("EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) EXECUTE chat_overlap_check(0,%d,%d,'%s',1000)", window.Max, window.Max, window.Cutoff.UTC().Format(time.RFC3339Nano))
	if err = tx.QueryRow(f.ctx, statement).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	blocks = plan[0].Plan.Hits + plan[0].Plan.Reads
	t.Logf("generic prepared plan: %d shared blocks", blocks)
	// Demonstrate why the moving window deliberately avoids the named plan
	// cache. Check the custom plan as well, matching its production execution.
	if _, err = tx.Exec(f.ctx, `SET LOCAL plan_cache_mode='force_custom_plan'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(f.ctx, statement).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	blocks = plan[0].Plan.Hits + plan[0].Plan.Reads
	t.Logf("per-execution plan: %d shared blocks", blocks)
	if blocks > 300 {
		t.Fatalf("per-execution overlap plan scans retained history: %d blocks", blocks)
	}
}
