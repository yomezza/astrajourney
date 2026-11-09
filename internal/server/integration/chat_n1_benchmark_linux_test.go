package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"golang.org/x/sys/unix"
)

type n1Probe struct {
	Probe, URL          string
	At, CPUNs, RSSBytes int64
	Metrics             map[string]any
}

func (p *n1Probe) UnmarshalJSON(raw []byte) error {
	var v struct {
		Probe, URL string
		At         int64          `json:"at"`
		CPU        int64          `json:"cpu_ns"`
		RSS        int64          `json:"rss_bytes"`
		Metrics    map[string]any `json:"metrics"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return e
	}
	p.Probe = v.Probe
	p.URL = v.URL
	p.At = v.At
	p.CPUNs = v.CPU
	p.RSSBytes = v.RSS
	p.Metrics = v.Metrics
	return nil
}
func n1Mono() int64 {
	var ts unix.Timespec
	if e := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); e != nil {
		panic(e)
	}
	return ts.Nano()
}

type n1LoadClient struct {
	conn        *websocket.Conn
	count, last atomic.Int64
	acks        chan string
	done        chan struct{}
}

func n1LoadDial(t *testing.T, base, origin, token string) *n1LoadClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/chat/ws/v1", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {"session=" + token}}})
	if e != nil {
		t.Fatal(e)
	}
	l := &n1LoadClient{conn: c, acks: make(chan string, 8), done: make(chan struct{})}
	go func() {
		defer close(l.done)
		for {
			_, raw, e := c.Read(context.Background())
			if e != nil {
				return
			}
			var v struct{ Type, Error string }
			if json.Unmarshal(raw, &v) != nil {
				return
			}
			switch v.Type {
			case "sync":
				l.last.Store(n1Mono())
				l.count.Add(1)
			case "ack":
				l.acks <- v.Error
			}
		}
	}()
	t.Cleanup(func() { c.CloseNow(); <-l.done })
	return l
}

type n1PhaseRow struct {
	Mode           string  `json:"mode"`
	Clients        int     `json:"clients"`
	Phase          string  `json:"phase"`
	Seconds        float64 `json:"seconds"`
	SQLCallsPerSec float64 `json:"sql_round_trips_per_sec"`
	PGTopPerSec    float64 `json:"pg_toplevel_statements_per_sec"`
	PGAllPerSec    float64 `json:"pg_tracked_statements_per_sec"`
	ChangesCalls   uint64  `json:"changes_calls"`
	ScopeChecks    uint64  `json:"scope_checks"`
	ChatCPU        float64 `json:"chat_cpu_percent_one_core"`
	PGCPU          float64 `json:"postgres_container_cpu_percent_one_core"`
	RSS            int64   `json:"chat_rss_bytes_end"`
	Heap           float64 `json:"chat_heap_bytes_end"`
	Goroutines     float64 `json:"chat_goroutines_end"`
	P50            float64 `json:"commit_sync_p50_ms"`
	P95            float64 `json:"commit_sync_p95_ms"`
	P99            float64 `json:"commit_sync_p99_ms"`
	Samples        int     `json:"latency_samples"`
	Missing        int64   `json:"missing_sync"`
	Extra          int64   `json:"extra_sync"`
	Disconnects    int     `json:"unexpected_disconnects"`
	Reconnects     int     `json:"reconnect_attempts"`
}

func TestChatN1Benchmark(t *testing.T) {
	if os.Getenv("RUN_CHAT_N1_BENCHMARK") != "1" {
		t.Skip("owned native Linux benchmark only")
	}
	if !strings.HasPrefix(os.Getenv("N1_PG_CONTAINER"), "cosmos-n1-") {
		t.Fatal("owned PostgreSQL container required for CPU telemetry")
	}
	t.Setenv("CHAT_ENABLED", "true")
	binary := filepath.Join(t.TempDir(), "chat-helper")
	build := exec.Command("go", "test", "-c", "-o", binary, "../../chatserver")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("helper build %v %s", e, out)
	}
	rows := []n1PhaseRow{}
	for _, n := range []int{50, 100, 200, 256} {
		for _, mode := range []string{"legacy", "a1"} {
			t.Run(fmt.Sprintf("%s/%d", mode, n), func(t *testing.T) { rows = append(rows, n1Profile(t, binary, mode, n)...) })
		}
	}
	if t.Failed() {
		return
	}
	if report := os.Getenv("CHAT_N1_BENCHMARK_REPORT"); report != "" {
		raw, _ := json.MarshalIndent(rows, "", "  ")
		if e := os.WriteFile(report, append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
func n1Profile(t *testing.T, binary, mode string, n int) []n1PhaseRow {
	f := newFixture033(t)
	_, role, definer := n1RuntimePool(t, f)
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	tokens := []string{}
	for i := 0; i < n; i++ {
		token := strings.ReplaceAll(chat.UUID()+chat.UUID(), "-", "")
		var id int64
		if e := f.db.QueryRow(f.ctx, `INSERT INTO users(username,password_hash) VALUES($1,'') RETURNING id`, fmt.Sprintf("n1load_%d", i)).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 hour')`, account.TokenHash(token), id)
		tokens = append(tokens, token)
	}
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}))
	defer health.Close()
	command := exec.Command(binary, "-test.run=^TestN1ProcessChild$", "-test.v")
	command.Env = append(os.Environ(), "N1_PROCESS_CHILD=1", "N1_RUNTIME_ROLE="+role, "CHAT_DATABASE_URL="+os.Getenv("TEST_DATABASE_URL"), "DB_SCHEMA="+schema, "CHAT_HTTP_ADDR=127.0.0.1:0", "COSMIC_INTERNAL_URL="+health.URL, "PLANET_INTERNAL_URL="+health.URL, "CHAT_ALLOWED_ORIGINS="+health.URL, "CHAT_ERASURE_JOURNAL="+filepath.Join(t.TempDir(), "journal"), "CHAT_DISPATCHER_MODE="+mode, "CHAT_FALLBACK_POLL_INTERVAL=10s", "CHAT_DEGRADED_POLL_INTERVAL=2s", "CHAT_OUTBOX_OVERLAP=60s")
	in, e := command.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := command.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	command.Stderr = os.Stderr
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	probes := make(chan n1Probe, 32)
	go func() {
		defer close(probes)
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var p n1Probe
			if json.Unmarshal(scanner.Bytes(), &p) == nil && p.Probe != "" {
				probes <- p
			}
		}
	}()
	t.Cleanup(func() {
		io.WriteString(in, "stop\n")
		in.Close()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case e := <-done:
			if e != nil {
				t.Error("helper shutdown", e)
			}
		case <-time.After(15 * time.Second):
			command.Process.Kill()
			<-done
			t.Error("helper shutdown timeout")
		}
	})
	receive := func(kind string) n1Probe {
		select {
		case p, ok := <-probes:
			if !ok || p.Probe != kind {
				t.Fatalf("probe %s got %s", kind, p.Probe)
			}
			return p
		case <-time.After(15 * time.Second):
			t.Fatalf("probe %s timeout", kind)
			return n1Probe{}
		}
	}
	base := receive("started").URL
	clients := []*n1LoadClient{}
	for _, token := range tokens {
		clients = append(clients, n1LoadDial(t, base, health.URL, token))
	}
	// Initial bootstrap/scope notifications are outside both measured phases.
	time.Sleep(3 * time.Second)
	sample := func() n1Probe { io.WriteString(in, "sample\n"); return receive("sample") }
	pgStatements := func() (float64, float64) {
		var top, all float64
		if e := f.db.QueryRow(f.ctx, `SELECT COALESCE(sum(calls) FILTER(WHERE toplevel),0)::float8,COALESCE(sum(calls),0)::float8 FROM public.pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND userid IN(SELECT oid FROM pg_roles WHERE rolname=ANY($1::text[]))`, []string{role, definer}).Scan(&top, &all); e != nil {
			t.Fatal(e)
		}
		return top, all
	}
	pgCPU := func() uint64 {
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
		}}
		defer transport.CloseIdleConnections()
		client := http.Client{Transport: transport, Timeout: 5 * time.Second}
		r, e := client.Get("http://docker/containers/" + os.Getenv("N1_PG_CONTAINER") + "/stats?stream=false&one-shot=true")
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		var v struct {
			CPU struct {
				Usage struct {
					Total uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
			} `json:"cpu_stats"`
		}
		if e = json.NewDecoder(r.Body).Decode(&v); e != nil || v.CPU.Usage.Total == 0 {
			t.Fatal("PostgreSQL CPU unavailable", e)
		}
		return v.CPU.Usage.Total
	}
	metric := func(p n1Probe, group, key string) float64 { return p.Metrics[group].(map[string]any)[key].(float64) }
	countSync := func() int64 {
		var c int64
		for _, l := range clients {
			c += l.count.Load()
		}
		return c
	}
	results := []n1PhaseRow{}
	for _, phase := range []string{"idle", "active"} {
		pgBefore := pgCPU()
		topBefore, allBefore := pgStatements()
		before := sample()
		syncBefore := countSync()
		latencies := []float64{}
		if phase == "idle" {
			time.Sleep(4 * time.Second)
		} else {
			start := time.Now()
			for i := 0; i < 4; i++ {
				raw := []byte(`{"type":"message.send","request_id":"` + chat.UUID() + `","payload":{"kind":"global","body":"N1 controlled benchmark"}}`)
				if e := clients[i].conn.Write(f.ctx, websocket.MessageText, raw); e != nil {
					t.Fatal(e)
				}
				commit := receive("commit").At
				select {
				case code := <-clients[i].acks:
					if code != "" {
						t.Fatal("send ack", code)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("send ack missing")
				}
				deadline := time.Now().Add(4 * time.Second)
				for {
					ready := true
					for _, l := range clients {
						if l.last.Load() < commit {
							ready = false
							break
						}
					}
					if ready {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("notification missing")
					}
					time.Sleep(time.Millisecond)
				}
				for _, l := range clients {
					latencies = append(latencies, float64(l.last.Load()-commit)/1e6)
				}
				if wait := time.Until(start.Add(time.Duration(i+1) * 2500 * time.Millisecond)); wait > 0 {
					time.Sleep(wait)
				} else {
					t.Fatal("controlled workload overrun")
				}
			}
		}
		after := sample()
		topAfter, allAfter := pgStatements()
		pgAfter := pgCPU()
		seconds := float64(after.At-before.At) / 1e9
		if topAfter < topBefore || allAfter < allBefore {
			t.Fatal("pg_stat_statements counters reset/evicted")
		}
		row := n1PhaseRow{Mode: mode, Clients: n, Phase: phase, Seconds: seconds, SQLCallsPerSec: (metric(after, "process", "sql_calls") - metric(before, "process", "sql_calls")) / seconds, PGTopPerSec: (topAfter - topBefore) / seconds, PGAllPerSec: (allAfter - allBefore) / seconds, ChangesCalls: uint64(metric(after, "service", "changes_calls") - metric(before, "service", "changes_calls")), ScopeChecks: uint64(metric(after, "service", "scope_checks") - metric(before, "service", "scope_checks")), ChatCPU: 100 * float64(after.CPUNs-before.CPUNs) / float64(after.At-before.At), PGCPU: 100 * float64(pgAfter-pgBefore) / float64(after.At-before.At), RSS: after.RSSBytes, Heap: metric(after, "process", "heap_alloc_bytes"), Goroutines: metric(after, "process", "goroutines"), Samples: len(latencies)}
		for _, l := range clients {
			select {
			case <-l.done:
				row.Disconnects++
			default:
			}
		}
		expected := int64(0)
		if phase == "active" {
			expected = int64(4 * n)
		}
		actual := countSync() - syncBefore
		if actual > expected {
			row.Extra = actual - expected
		} else {
			row.Missing = expected - actual
		}
		if len(latencies) > 0 {
			sort.Float64s(latencies)
			percentile := func(p float64) float64 { return latencies[int(float64(len(latencies)-1)*p)] }
			row.P50 = percentile(.5)
			row.P95 = percentile(.95)
			row.P99 = percentile(.99)
		}
		if row.Missing != 0 || row.Disconnects != 0 {
			t.Fatal("missing sync or unexpected connection loss", row)
		}
		results = append(results, row)
		t.Logf("%s %d %s SQL=%.1f/s PG=%.1f/s Changes=%d p50/95/99=%.1f/%.1f/%.1fms ChatCPU=%.1f%% PGCPU=%.1f%% extra=%d", mode, n, phase, row.SQLCallsPerSec, row.PGAllPerSec, row.ChangesCalls, row.P50, row.P95, row.P99, row.ChatCPU, row.PGCPU, row.Extra)
	}
	if f.integer(`SELECT count(*) FROM chat_messages WHERE message_type='user'`) != 4 {
		t.Fatal("committed history mismatch")
	}
	return results
}
