package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	"github.com/yomezza/cosmosimulator/internal/mining"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

var benchmarkControl = struct {
	sync.Mutex
	cancel map[int64]context.CancelFunc
}{cancel: map[int64]context.CancelFunc{}}

func (a *Service) Benchmark(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	if r.Method == "POST" {
		var in struct {
			Action   string `json:"action"`
			ID       int64  `json:"id"`
			Players  int    `json:"players"`
			Seconds  int    `json:"seconds"`
			Scenario string `json:"scenario"`
		}
		if !httpkit.Decode(w, r, &in) {
			return
		}
		if in.Action == "stop" {
			benchmarkControl.Lock()
			cancel := benchmarkControl.cancel[in.ID]
			benchmarkControl.Unlock()
			if cancel != nil {
				cancel()
			}
		} else if in.Action == "start" {
			if in.Players < 1 || in.Players > 500 || in.Seconds < 5 || in.Seconds > 300 || (in.Scenario != "mixed" && in.Scenario != "quota" && in.Scenario != "stock" && in.Scenario != "reservations") {
				httpkit.Fail(w, 400, "Игроки: 1–500, время: 5–300 секунд")
				return
			}
			var running bool
			if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM benchmark_runs WHERE status='running')`).Scan(&running); err != nil {
				httpkit.Internal(w, err)
				return
			}
			if running {
				httpkit.Fail(w, 409, "Бенчмарк уже выполняется")
				return
			}
			var id int64
			if err := a.DB.QueryRow(r.Context(), `INSERT INTO benchmark_runs(actor,players,seconds,scenario) VALUES($1,$2,$3,$4) RETURNING id`, u.ID, in.Players, in.Seconds, in.Scenario).Scan(&id); err != nil {
				var pgerr *pgconn.PgError
				if errors.As(err, &pgerr) && pgerr.Code == "23505" {
					httpkit.Fail(w, 409, "Бенчмарк уже выполняется")
				} else {
					httpkit.Internal(w, err)
				}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(in.Seconds+90)*time.Second)
			benchmarkControl.Lock()
			benchmarkControl.cancel[id] = cancel
			benchmarkControl.Unlock()
			go a.runBenchmark(ctx, cancel, id, in.Players, in.Seconds, in.Scenario)
		} else {
			httpkit.Fail(w, 400, "Неизвестное действие")
			return
		}
	}
	rows, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT * FROM benchmark_runs ORDER BY id DESC LIMIT 20`)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	httpkit.Reply(w, 200, rows)
}

func (a *Service) runBenchmark(ctx context.Context, cancel context.CancelFunc, id int64, players, seconds int, scenario string) {
	defer cancel()
	defer func() { benchmarkControl.Lock(); delete(benchmarkControl.cancel, id); benchmarkControl.Unlock() }()
	status := "failed"
	metrics := map[string]any{"isolation": "temporary PostgreSQL schemas", "version": "0.23.0", "scenario": scenario}
	var total, failed, conflicts atomic.Int64
	defer func() {
		if status == "failed" && ctx.Err() != nil {
			status = "stopped"
		}
		raw, _ := json.Marshal(metrics)
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, _ = a.DB.Exec(c, `UPDATE benchmark_runs SET status=$2,requests=$3,errors=$4,metrics=$5,finished_at=now() WHERE id=$1`, id, status, total.Load(), failed.Load(), raw)
	}()
	// Only the workload runs against disposable data in the same PostgreSQL DB.
	// No configurable world generator or production account is exposed to workers.
	schema := fmt.Sprintf("benchmark_%d_%d", id, time.Now().UnixNano())
	dsn := a.DB.Config().ConnString()
	db, err := a.OpenDatabase(ctx, dsn, schema)
	if err != nil {
		metrics["failure"] = err.Error()
		return
	}
	defer func() {
		db.Close()
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, _ = a.DB.Exec(c, "DROP SCHEMA IF EXISTS "+dblayout.DropList(schema)+" CASCADE")
	}()
	handler, e := a.Handler(ctx, db)
	if e != nil {
		metrics["failure"] = e.Error()
		return
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	cookies := make([]*http.Cookie, players)
	for i := 0; i < players; i++ {
		var uid int64
		token := fmt.Sprintf("%064x", i+1)
		if err = db.QueryRow(ctx, `INSERT INTO users(username,password_hash,credits,is_bot,virtual_run) VALUES($1,'',1000000,TRUE,$2) RETURNING id`, fmt.Sprintf("virtual_%d", i), id).Scan(&uid); err != nil {
			metrics["failure"] = err.Error()
			return
		}
		if _, err = db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, account.TokenHash(token), uid); err != nil {
			metrics["failure"] = err.Error()
			return
		}
		obj := "station"
		if scenario == "quota" {
			obj = "asteroids"
		}
		if _, err = db.Exec(ctx, `INSERT INTO ships(user_id,active,object_id,position_mode) VALUES($1,TRUE,$2,$3);`, uid, obj, func() string {
			if obj == "station" {
				return "docked"
			}
			return "approach"
		}()); err != nil {
			metrics["failure"] = err.Error()
			return
		}
		if _, err = db.Exec(ctx, `INSERT INTO surveys(user_id,star) VALUES($1,1)`, uid); err != nil {
			metrics["failure"] = err.Error()
			return
		}
		cookies[i] = &http.Cookie{Name: "session", Value: token}
	}
	_, _ = db.Exec(ctx, `INSERT INTO local_markets(star,stock) VALUES(1,'{"iron":1,"agni":10000}') ON CONFLICT DO NOTHING`)
	if scenario == "quota" {
		tx, e := db.Begin(ctx)
		if e != nil {
			metrics["failure"] = e.Error()
			return
		}
		settings, e := gameplay.ReadGameplay(ctx, tx)
		if e == nil {
			_, e = mining.EnsureQuota(ctx, tx, settings, 1, 101, "*", time.Now(), nil)
		}
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE resource_quotas SET used=capacity-1 WHERE star=1 AND object=101 AND resource='*'`)
		}
		if e == nil {
			e = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(context.Background())
		}
		if e != nil {
			metrics["failure"] = e.Error()
			return
		}
	}
	var mu sync.Mutex
	latencies := []float64{}
	var wg sync.WaitGroup
	start := time.Now()
	deadline := start.Add(time.Duration(seconds) * time.Second)
	send := func(cookie *http.Cookie, method, path, body string) []byte {
		if ctx.Err() != nil {
			return nil
		}
		t := time.Now()
		req, e := http.NewRequestWithContext(ctx, method, srv.URL+path, strings.NewReader(body))
		if e != nil {
			return nil
		}
		req.AddCookie(cookie)
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, e := client.Do(req)
		var data []byte
		if e != nil {
			if ctx.Err() == nil {
				failed.Add(1)
			}
		} else {
			data, _ = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			resp.Body.Close()
			if resp.StatusCode == 409 {
				conflicts.Add(1)
			} else if resp.StatusCode >= 400 {
				failed.Add(1)
			}
		}
		total.Add(1)
		elapsed := float64(time.Since(t).Microseconds()) / 1000
		mu.Lock()
		latencies = append(latencies, elapsed)
		mu.Unlock()
		return data
	}
	for i := 0; i < players; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cookie := cookies[i]
			step := 0
			for ctx.Err() == nil && time.Now().Before(deadline) {
				switch scenario {
				case "quota":
					send(cookie, "POST", "/api/expedition", `{"action":"mine","planet":101,"resource":"iron"}`)
				case "stock":
					send(cookie, "POST", "/api/expedition", `{"action":"buy","resource":"iron","quantity":1}`)
				case "reservations":
					send(cookie, "POST", "/api/market/global", `{"action":"reserve","star":1,"resource":"agni","quantity":1}`)
					raw := send(cookie, "GET", "/api/market/global", "")
					var data struct {
						Reservations []struct {
							ID     int64  `json:"id"`
							Status string `json:"status"`
						} `json:"reservations"`
					}
					if json.Unmarshal(raw, &data) == nil {
						for _, r := range data.Reservations {
							if r.Status == "reserved" {
								body := `{"action":"collect","id":` + strconv.FormatInt(r.ID, 10) + `}`
								send(cookie, "POST", "/api/market/global", body)
								send(cookie, "POST", "/api/market/global", body)
								break
							}
						}
					}
				default:
					paths := []string{"/api/state", "/api/fleet", "/api/expedition", "/api/workbench"}
					send(cookie, "GET", paths[step%len(paths)], "")
				}
				step++
				select {
				case <-ctx.Done():
					return
				case <-time.After(40 * time.Millisecond):
				}
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-ticker.C:
			_, _ = a.DB.Exec(context.Background(), `UPDATE benchmark_runs SET requests=$2,errors=$3 WHERE id=$1`, id, total.Load(), failed.Load())
		case <-ctx.Done():
			wg.Wait()
			break loop
		}
	}
	mu.Lock()
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		for key, p := range map[string]float64{"p50_ms": .50, "p95_ms": .95, "p99_ms": .99, "max_ms": 1} {
			metrics[key] = latencies[min(len(latencies)-1, int(float64(len(latencies)-1)*p))]
		}
	}
	mu.Unlock()
	metrics["duration_seconds"] = time.Since(start).Seconds()
	metrics["rps"] = float64(total.Load()) / time.Since(start).Seconds()
	metrics["expected_conflicts"] = conflicts.Load()
	metrics["pool_acquired"] = db.Stat().AcquiredConns()
	metrics["pool_max"] = db.Stat().MaxConns()
	metrics["db_acquire_ms"] = float64(db.Stat().AcquireDuration().Microseconds()) / 1000
	metrics["db_wait_count"] = db.Stat().EmptyAcquireCount()
	var invalid int
	check, doneCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer doneCheck()
	err = db.QueryRow(check, `SELECT (SELECT count(*) FROM users WHERE credits<0)+(SELECT count(*) FROM resource_quotas WHERE used>capacity OR used<0)+(SELECT count(*) FROM local_markets m,LATERAL jsonb_each_text(m.stock) e WHERE e.value::int<0)`).Scan(&invalid)

	if err == nil {
		var cash int64
		err = db.QueryRow(check, `SELECT (SELECT COALESCE(sum(credits),0) FROM users)+(SELECT COALESCE(sum(credits),0) FROM local_markets)+(SELECT COALESCE(sum(cost),0) FROM reservations WHERE status='reserved')`).Scan(&cash)
		if err == nil {
			delta := cash - (int64(players)*1000000 + 8000)
			metrics["credits_delta"] = delta
			if delta != 0 {
				invalid++
			}
		}
		var units int64
		if err == nil && scenario == "reservations" {
			err = db.QueryRow(check, `SELECT (SELECT COALESCE(sum(COALESCE((cargo->>'agni')::bigint,0)),0) FROM ships)+(SELECT COALESCE(sum(COALESCE((stock->>'agni')::bigint,0)),0) FROM local_markets)+(SELECT COALESCE(sum(quantity),0) FROM reservations WHERE status='reserved' AND resource='agni')`).Scan(&units)
			if err == nil {
				metrics["resource_delta"] = units - 10000
				if units != 10000 {
					invalid++
				}
			}
		}
		if err == nil && ctx.Err() == nil && (scenario == "quota" || scenario == "stock") {
			query := `SELECT COALESCE(sum(COALESCE((cargo->>'iron')::bigint,0)),0) FROM ships`
			if scenario == "quota" {
				query = `SELECT (SELECT COALESCE(sum(quantity),0) FROM mining_jobs)+(SELECT COALESCE(sum(COALESCE((cargo->>'iron')::bigint,0)),0) FROM ships)`
			}
			err = db.QueryRow(check, query).Scan(&units)
			if err == nil {
				metrics["allocated_units"] = units
				if units != 1 {
					invalid++
				}
			}
		}
	}
	metrics["integrity_violations"] = invalid
	if err != nil {
		metrics["integrity_error"] = err.Error()
		status = "failed"
	} else if invalid > 0 {
		status = "failed"
	} else if ctx.Err() != nil {
		status = "stopped"
	} else {
		status = "completed"
	}
}
