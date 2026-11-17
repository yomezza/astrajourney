package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mixedProbe(ctx context.Context, client *http.Client, base, path string, cookie *http.Cookie, token string) map[string]any {
	request, e := http.NewRequestWithContext(ctx, "GET", base+path, nil)
	if e != nil {
		return map[string]any{"error": "request"}
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if token != "" {
		request.Header.Set("X-Astra-Service-Token", token)
	}
	response, e := client.Do(request)
	if e != nil {
		return map[string]any{"error": "transport"}
	}
	defer response.Body.Close()
	data := map[string]any{}
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&data) != nil {
		return map[string]any{"http": response.StatusCode, "error": "decode"}
	}
	return map[string]any{"http": response.StatusCode, "data": data}
}
func mixedSampleStart(t *testing.T, parent context.Context, f *fixture033, admin *mixedPlayer, urls map[string]string, natsURL, tokenFile string, pids map[string]int, metrics *mixedMetrics, logs, directory string) func() {
	t.Helper()
	file, e := os.OpenFile(filepath.Join(directory, "samples.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	tokenBytes, _ := os.ReadFile(tokenFile)
	token := strings.TrimSpace(string(tokenBytes))
	check, cancel := context.WithTimeout(parent, 4*time.Second)
	nats := mixedProbe(check, client, natsURL, "/varz", nil, "")
	cancel()
	if data, ok := nats["data"].(map[string]any); ok {
		if pid, ok := data["pid"].(float64); ok {
			pids["nats"] = int(pid)
		}
	}
	if pids["nats"] == 0 {
		for _, child := range mixedChildren(os.Getpid()) {
			if comm, e := os.ReadFile(filepath.Join("/proc", fmt.Sprint(child), "comm")); e == nil && strings.TrimSpace(string(comm)) == "nats-server" {
				pids["nats"] = child
			}
		}
	}
	run, stop := context.WithCancel(parent)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		defer file.Close()
		encoder := json.NewEncoder(file)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var chatOffset int64
		var chatDiagnostic map[string]any
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
			}
			metrics.mu.Lock()
			phase := metrics.phase
			metrics.mu.Unlock()
			process := map[string]any{}
			for name, pid := range pids {
				if pid > 0 {
					if name == "postgres_cluster" {
						process[name] = mixedProcessGroup(pid)
					} else {
						process[name] = processUsageLoad230(pid)
					}
				}
			}
			if pid := int(metrics.browserPID.Load()); pid > 0 {
				process["browser_probe_tree"] = mixedProcessGroup(pid)
			}
			if log, e := os.Open(filepath.Join(logs, "chat.jsonl")); e == nil {
				log.Seek(chatOffset, io.SeekStart)
				scanner := bufio.NewScanner(log)
				scanner.Buffer(make([]byte, 64<<10), 2<<20)
				for scanner.Scan() {
					line := scanner.Bytes()
					chatOffset += int64(len(line) + 1)
					if bytes.Contains(line, []byte("chat dispatcher diagnostics")) {
						var v map[string]any
						if json.Unmarshal(line, &v) == nil {
							chatDiagnostic = v
						}
					}
				}
				log.Close()
			}
			row := map[string]any{"at": time.Now().UTC(), "phase": phase, "processes": process, "cgroup": mixedCgroup(), "activity": metrics.activity(), "chat_diagnostics": chatDiagnostic, "logs_bytes": mixedLogSizes(logs)}
			var mu sync.Mutex
			var workers sync.WaitGroup
			for name, endpoint := range map[string]struct{ base, path, token string }{"cosmos": {urls["web"], "/api/admin/metrics", ""}, "planet": {urls["planet"], "/planet/api/v1/metrics", ""}, "cdn": {urls["cdn"], "/cdn/api/v1/metrics", token}, "web": {urls["web"], "/web/api/v1/health/ready", ""}, "chat_health": {urls["chat"], "/chat/api/v1/health", ""}, "nats": {natsURL, "/varz", ""}} {
				workers.Add(1)
				go func(name string, endpoint struct{ base, path, token string }) {
					defer workers.Done()
					ctx, done := context.WithTimeout(run, 4*time.Second)
					defer done()
					v := mixedProbe(ctx, client, endpoint.base, endpoint.path, admin.cookie, endpoint.token)
					mu.Lock()
					row[name] = v
					mu.Unlock()
				}(name, endpoint)
			}
			workers.Wait()
			ctx, cancel := context.WithTimeout(run, 2*time.Second)
			var connections, active, waiting int
			var oldest float64
			e = f.db.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE state='active'),count(*) FILTER(WHERE wait_event_type='Lock'),COALESCE(max(EXTRACT(epoch FROM clock_timestamp()-query_start)) FILTER(WHERE state='active'),0)::float8 FROM pg_stat_activity WHERE datname=current_database()`).Scan(&connections, &active, &waiting, &oldest)
			if e == nil {
				row["postgres_activity"] = map[string]any{"connections": connections, "active": active, "lock_waiting": waiting, "oldest_active_seconds": oldest}
			}
			var commits, rollbacks, reads, hits int64
			e = f.db.QueryRow(ctx, `SELECT xact_commit,xact_rollback,blks_read,blks_hit FROM pg_stat_database WHERE datname=current_database()`).Scan(&commits, &rollbacks, &reads, &hits)
			if e == nil {
				row["postgres_database"] = map[string]any{"xact_commit": commits, "xact_rollback": rollbacks, "blks_read": reads, "blks_hit": hits}
			}
			cancel()
			if e = encoder.Encode(row); e != nil {
				return
			}
		}
	}()
	return func() { once.Do(func() { stop(); <-done }) }
}

func mixedBrowserStart(t *testing.T, ctx context.Context, root, directory string, p *mixedPlayer) int {
	t.Helper()
	output, e := os.OpenFile(filepath.Join(directory, "logs", "browser.console.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	input, _ := json.Marshal(map[string]any{"base": p.base, "cookie": p.cookie.Value, "output": directory})
	cmd := exec.Command("node", filepath.Join(root, "tests", "load", "mixed100-browser.mjs"))
	cmd.Dir = root
	cmd.Env = os.Environ()
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = output
	cmd.Stderr = output
	if e = cmd.Start(); e != nil {
		output.Close()
		t.Fatal("browser observer start", e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(12 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			output.Close()
		})
	}
	t.Cleanup(stop)
	go func() { <-ctx.Done(); stop() }()
	return cmd.Process.Pid
}
