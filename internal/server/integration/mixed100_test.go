package integration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pelletier/go-toml/v2"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func mixedDuration(t *testing.T, key string, fallback time.Duration) time.Duration {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	d, e := time.ParseDuration(value)
	if e != nil || d < 5*time.Second || d > 45*time.Minute {
		t.Fatal("invalid duration for", key)
	}
	return d
}

// Optional comparison artifacts must stay in the owned build directory. The
// published downloads remain untouched, and no remote target is configurable.
func mixedOwnedArtifact(root, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || !strings.HasPrefix(absolute, filepath.Join(root, "dist")+string(os.PathSeparator)) {
		return "", fmt.Errorf("comparison artifact must be inside this repository's dist directory")
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("comparison artifact must be a regular file")
	}
	return absolute, nil
}
func mixedDatabase(t *testing.T) (string, int) {
	t.Helper()
	u, e := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if e != nil || u.Hostname() != "127.0.0.1" || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("a loopback disposable TEST_DATABASE_URL ending _test is required")
	}
	admin, e := pgx.Connect(context.Background(), u.String())
	if e != nil {
		t.Fatal("local test PostgreSQL unavailable")
	}
	name := fmt.Sprintf("mixed100_%d_test", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(context.Background(), "CREATE DATABASE "+quoted); e != nil {
		admin.Close(context.Background())
		t.Fatal("cannot create owned test database", e)
	}
	t.Cleanup(func() {
		_, e := admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)")
		if e != nil {
			t.Error("owned test database cleanup", e)
		}
		admin.Close(context.Background())
	})
	var directory string
	var pgPID int
	if admin.QueryRow(context.Background(), "SHOW data_directory").Scan(&directory) == nil {
		if raw, e := os.ReadFile(filepath.Join(directory, "postmaster.pid")); e == nil {
			fmt.Sscan(strings.SplitN(string(raw), "\n", 2)[0], &pgPID)
		}
	}
	u.Path = "/" + name
	t.Setenv("TEST_DATABASE_URL", u.String())
	return name, pgPID
}
func mixedEnv(config string) map[string]string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		drop := key == "CONFIG_FILE" || key == "DATABASE_URL" || key == "DB_SCHEMA" || key == "HTTP_ADDR" || key == "PUBLIC_ORIGIN" || key == "COOKIE_SECURE" || key == "TRUSTED_PROXIES"
		for _, prefix := range []string{"COSMOS_", "PLANET_", "CHAT_", "CDN_", "WEB_", "NATS_", "ADMIN_", "DIAGNOSTICS_"} {
			drop = drop || strings.HasPrefix(key, prefix)
		}
		if !drop {
			env[key] = value
		}
	}
	env["CONFIG_FILE"] = config
	return env
}
func mixedStart(t *testing.T, binary, config, root, logs, name string, args ...string) *loadProcess230 {
	t.Helper()
	file, e := os.OpenFile(filepath.Join(logs, name+".console.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	for key, value := range mixedEnv(config) {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout = file
	cmd.Stderr = file
	if e = cmd.Start(); e != nil {
		file.Close()
		t.Fatal("start", name, e)
	}
	p := &loadProcess230{cmd: cmd, log: file, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { p.stop(t, false); file.Close() })
	return p
}
func mixedInstall(t *testing.T, binary, config, root, archive, logName string) {
	t.Helper()
	cmd := exec.Command(binary, "--install-pack", archive)
	cmd.Dir = root
	for key, value := range mixedEnv(config) {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	raw, e := cmd.CombinedOutput()
	os.WriteFile(logName, raw, 0600)
	if e != nil {
		t.Fatal("local pack install failed", filepath.Base(binary))
	}
}
func mixedSectors(t *testing.T, f *fixture033, surface, owner, company int64) []int64 {
	t.Helper()
	var raw []byte
	if e := f.db.QueryRow(f.ctx, `SELECT data FROM planet_surfaces WHERE id=$1`, surface).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var world geography.PlanetSurface
	if json.Unmarshal(raw, &world) != nil {
		t.Fatal("surface geometry")
	}
	sectors := append([]geography.PlanetSector(nil), world.Sectors...)
	sort.Slice(sectors, func(i, j int) bool { return sectors[i].Land > sectors[j].Land })
	used := map[int]bool{}
	chosen := []int{}
	for _, s := range sectors {
		if s.Land < 30 || used[s.Number] {
			continue
		}
		for _, neighbor := range s.Neighbors {
			if neighbor < 1 || neighbor > len(world.Sectors) || world.Sectors[neighbor-1].Land < 30 || used[neighbor] {
				continue
			}
			allowed := true
			for _, candidate := range []int{s.Number, neighbor} {
				for _, adjacent := range world.Sectors[candidate-1].Neighbors {
					if used[adjacent] {
						allowed = false
					}
				}
			}
			if !allowed {
				continue
			}
			chosen = append(chosen, s.Number, neighbor)
			used[s.Number] = true
			used[neighbor] = true
			break
		}
		if len(chosen) >= 6 {
			break
		}
	}
	if len(chosen) < 2 {
		t.Fatal("generated planet has insufficient valid adjacent sectors")
	}
	for _, sector := range chosen {
		addColony160(f, owner, surface, sector, &company, false)
	}
	reconcile160(f)
	sites := []int64{}
	rows, e := f.db.Query(f.ctx, `SELECT id FROM planet_landing_sites WHERE surface=$1 AND enabled ORDER BY sector`, surface)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) != nil {
			t.Fatal("landing site decode")
		}
		sites = append(sites, id)
	}
	rows.Close()
	if len(sites) == 0 {
		t.Fatal("generated planet has no ready landing sites")
	}
	return sites
}

func TestMixedPlayers100(t *testing.T) {
	if os.Getenv("RUN_MIXED_100") != "1" {
		t.Skip("opt in with RUN_MIXED_100=1; creates a loopback-only disposable database and child processes")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux process sampling required")
	}
	n := 100
	if value := os.Getenv("MIXED100_USERS"); value != "" {
		var e error
		n, e = strconv.Atoi(value)
		if e != nil || n < 10 || n > 100 {
			t.Fatal("MIXED100_USERS must be 10-100")
		}
	}
	ramp := mixedDuration(t, "MIXED100_RAMP", 60*time.Second)
	hold := mixedDuration(t, "MIXED100_HOLD", 30*time.Minute)
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	runID := time.Now().UTC().Format("20060102T150405.000000000Z")
	directory := filepath.Join(root, "dist", "mixed100", runID)
	logs := filepath.Join(directory, "logs")
	if e = os.MkdirAll(logs, 0700); e != nil {
		t.Fatal(e)
	}
	t.Log("owned output:", directory)
	name, pgPID := mixedDatabase(t)
	t.Setenv("CHAT_ENABLED", "true")
	f, owner, ownerID, surface, _ := setupPlanet120(t)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, ownerID)
	var company, role int64
	if e = f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,color,flag,owner_id) VALUES('Mixed100 Expedition','MX100','#88aaaa','orbit',$1) RETURNING id`, ownerID).Scan(&company); e != nil {
		t.Fatal(e)
	}
	if e = f.db.QueryRow(f.ctx, `INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Expedition member','["planetary.enter","colonize"]') RETURNING id`, company).Scan(&role); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE users SET company_id=$2,company_role=$3 WHERE id=$1`, ownerID, company, role)
	sites := mixedSectors(t, f, surface, ownerID, company)
	schema := f.db.Config().ConnConfig.RuntimeParams["search_path"]
	_ = schema
	if e = f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	_, chatRole, _ := n1RuntimePool(t, f)
	u, _ := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	q := u.Query()
	q.Set("pool_max_conns", "8")
	u.RawQuery = q.Encode()
	dsn := u.String()
	q.Set("role", chatRole)
	u.RawQuery = q.Encode()
	chatDSN := u.String()
	broker := natsfixture.New(t, natsfixture.Options{N6: true})
	defer func() {
		if raw, e := os.ReadFile(broker.LogFile); e == nil {
			os.WriteFile(filepath.Join(logs, "nats.log"), raw, 0600)
		}
	}()
	urls := map[string]string{}
	for _, service := range []string{"cosmos", "planet", "chat", "cdn", "web"} {
		urls[service] = loadPort230(t)
	}
	storage := t.TempDir()
	token := filepath.Join(storage, "cdn", "state", "service.token")
	natsConfig := map[string]any{"enabled": true, "servers": []string{broker.URL}, "ca_file": broker.CAFile, "connect_timeout": "2s", "reconnect_wait": "1s"}
	for service, password := range map[string]string{"cosmos": broker.CosmosPassword, "planet": broker.PlanetPassword, "chat": broker.Password, "cdn": broker.CDNPassword, "web": broker.WebPassword} {
		natsConfig[service] = map[string]any{"username": service, "password": password}
	}
	config := map[string]any{
		"diagnostics": map[string]any{"directory": logs, "enabled": true}, "database": map[string]any{"url": dsn, "schema": schema},
		"cosmos": map[string]any{"http_addr": strings.TrimPrefix(urls["cosmos"], "http://"), "public_origin": urls["web"], "cookie_secure": false, "trusted_proxies": []string{"127.0.0.1/32"}, "planet_enabled": true, "chat_enabled": true, "planet_internal_url": urls["planet"], "chat_internal_url": urls["chat"], "cdn_internal_url": urls["cdn"], "web_internal_url": urls["web"], "cdn_service_token_file": token},
		"planet": map[string]any{"http_addr": strings.TrimPrefix(urls["planet"], "http://"), "allowed_origins": []string{urls["web"]}, "log_file": filepath.Join(logs, "planet.legacy.log"), "db_timeout": "2s", "lease_ttl": "15s", "lease_renew": "5s", "shutdown_timeout": "10s", "scene_capacity": 32, "scene_idle_ttl": "2m", "scene_load_timeout": "20s", "move_speed": 4.0, "path_budget": 24000},
		"chat":   map[string]any{"http_addr": strings.TrimPrefix(urls["chat"], "http://"), "cosmic_internal_url": urls["cosmos"], "planet_internal_url": urls["planet"], "allowed_origins": []string{urls["web"]}, "database_url": chatDSN, "erasure_journal": filepath.Join(storage, "chat-erasure.log"), "dispatcher_mode": "b1", "fallback_poll_interval": "10s", "degraded_poll_interval": "2s", "outbox_overlap": "60s"},
		"cdn":    map[string]any{"http_addr": strings.TrimPrefix(urls["cdn"], "http://"), "storage_root": filepath.Join(storage, "cdn"), "cosmic_internal_url": urls["cosmos"], "service_token_file": token, "sync_enabled": false, "repository": "yomezza/astrajourney", "sync_interval": "30m"},
		"web":    map[string]any{"http_addr": strings.TrimPrefix(urls["web"], "http://"), "storage_root": filepath.Join(storage, "web"), "cosmic_internal_url": urls["cosmos"], "cdn_internal_url": urls["cdn"], "planet_internal_url": urls["planet"], "chat_internal_url": urls["chat"], "sync_enabled": false, "release_tag": "v0.30.0"}, "nats": natsConfig,
	}
	configBytes, e := toml.Marshal(config)
	if e != nil {
		t.Fatal(e)
	}
	configFile := filepath.Join(storage, "shared.toml")
	if e = os.WriteFile(configFile, configBytes, 0600); e != nil {
		t.Fatal(e)
	}
	binaries := map[string]string{}
	hashes := map[string]string{}
	for _, service := range []string{"cosmos", "planet", "chat", "cdn", "web"} {
		suffix := "-" + service
		if service == "cosmos" {
			suffix = ""
		}
		path := filepath.Join(root, "downloads", "astra-journey"+suffix+"-v0.30.0-linux-amd64")
		if service == "chat" && os.Getenv("MIXED100_CHAT_BINARY") != "" {
			path, e = mixedOwnedArtifact(root, os.Getenv("MIXED100_CHAT_BINARY"))
			if e != nil {
				t.Fatal(e)
			}
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		hashes[service] = fmt.Sprintf("%x", sha256.Sum256(raw))
		binaries[service] = path
	}
	var assetIndex, webIndex struct {
		Archive string `json:"archive"`
	}
	raw, _ := os.ReadFile(filepath.Join(root, "downloads", "astra-journey-assets.json"))
	if json.Unmarshal(raw, &assetIndex) != nil {
		t.Fatal("asset index")
	}
	raw, _ = os.ReadFile(filepath.Join(root, "downloads", "astra-journey-web.json"))
	if json.Unmarshal(raw, &webIndex) != nil {
		t.Fatal("web index")
	}
	mixedInstall(t, binaries["cdn"], configFile, root, filepath.Join(root, "downloads", assetIndex.Archive), filepath.Join(logs, "cdn.install.log"))
	webArchive := filepath.Join(root, "downloads", webIndex.Archive)
	if os.Getenv("MIXED100_WEB_PACK") != "" {
		webArchive, e = mixedOwnedArtifact(root, os.Getenv("MIXED100_WEB_PACK"))
		if e != nil {
			t.Fatal(e)
		}
	}
	mixedInstall(t, binaries["web"], configFile, root, webArchive, filepath.Join(logs, "web.install.log"))
	processes := map[string]*loadProcess230{}
	pids := map[string]int{"generator": os.Getpid(), "postgres_cluster": pgPID}
	defer func() {
		for _, service := range []string{"web", "chat", "planet", "cdn", "cosmos"} {
			if p := processes[service]; p != nil {
				p.stop(t, false)
			}
		}
	}()
	for _, service := range []string{"cosmos", "cdn", "planet", "chat", "web"} {
		p := mixedStart(t, binaries[service], configFile, root, logs, service)
		processes[service] = p
		pids[service] = p.cmd.Process.Pid
		path := map[string]string{"cosmos": "/api/health", "cdn": "/cdn/api/v1/health/ready", "planet": "/planet/api/v1/health/ready", "chat": "/chat/api/v1/health", "web": "/web/api/v1/health/ready"}[service]
		waitLoadReady230(t, urls[service], path)
		t.Log("ready:", service)
	}
	metrics := newMixedMetrics()
	players := make([]*mixedPlayer, n)
	for i := range n {
		p := newMixedPlayer(i, urls["web"], metrics)
		players[i] = p
		if i == 0 {
			p.cookie = owner
			p.uid = ownerID
		} else {
			v, status := p.get(f.ctx, "/api/server-status")
			_ = v
			_ = status
			raw, status, code := p.request(f.ctx, "POST", "/api/register", map[string]any{"username": fmt.Sprintf("mixed100_%03d", i), "password": "local-load-test-only"})
			_ = raw
			if status != 200 || p.cookie == nil {
				t.Fatalf("owned registration %d: HTTP %d %s", i, status, code)
			}
			if e = f.db.QueryRow(f.ctx, `SELECT id FROM users WHERE username=$1`, fmt.Sprintf("mixed100_%03d", i)).Scan(&p.uid); e != nil {
				t.Fatal(e)
			}
		}
		_, status := p.get(f.ctx, "/api/expedition")
		if status != 200 {
			t.Fatal("initial ship setup HTTP", status)
		}
		p.shipID = f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, p.uid)
		f.exec(`UPDATE users SET company_id=$2,company_role=$3,credits=1000000,level=30 WHERE id=$1`, p.uid, company, role)
		f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1) ON CONFLICT DO NOTHING`, p.uid)
		f.exec(`UPDATE ships SET cargo='{"agni":10,"iron":2}' WHERE id=$1`, p.shipID)
		f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station','{"iron":500}') ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=excluded.cargo`, p.uid)
		if p.planet {
			planet := f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface)
			f.exec(`UPDATE ships SET star=1,object_id=$2,position_mode='orbit',destination=NULL,local_target=NULL,waypoints='[]' WHERE id=$1`, p.shipID, fmt.Sprintf("planet-%d", planet))
		}
	}
	var public struct{ Lanes []struct{ From, To int } }
	raw, _, _ = players[0].request(f.ctx, "GET", "/api/galaxy", nil)
	if json.Unmarshal(raw, &public) != nil {
		t.Fatal("public world decode")
	}
	neighbors := map[int][]int{}
	for _, edge := range public.Lanes {
		neighbors[edge.From] = append(neighbors[edge.From], edge.To)
		neighbors[edge.To] = append(neighbors[edge.To], edge.From)
	}
	baseReport := map[string]any{"run": runID, "version": "0.30.0", "started_at": time.Now().UTC(), "planned_users": n, "chat_writers_percent": 80, "planet_percent": 30, "dispatcher": "b1", "logging_all_services": true, "nats_tls": true, "pool_max_per_service": 8, "go": runtime.Version(), "visible_cpus": runtime.NumCPU(), "cgroup": mixedCgroup(), "binary_sha256": hashes, "database": name, "landing_sites": len(sites), "surface": "generated garden planet, prepared corporate colonies", "setup": "accounts, knowledge, starting resources and colony access prepared before measurement; actions through actual HTTP/WS via Web", "graph_systems": f.integer(`SELECT count(*) FROM star_systems`), "phase_result": "running"}
	baseReport["variant"] = os.Getenv("MIXED100_LABEL")
	var wg sync.WaitGroup
	run, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); wg.Wait() }()
	active := 0
	phases := []map[string]any{}
	browserPID := 0
	stopSample := mixedSampleStart(t, run, f, players[0], urls, broker.MonitorURL, token, pids, metrics, logs, directory)
	defer stopSample()
	publish := func() {
		out := map[string]any{}
		for key, value := range baseReport {
			out[key] = value
		}
		out["phases"] = phases
		out["result"] = metrics.snapshot()
		out["logs"] = mixedLogSizes(logs)
		out["finished_at"] = time.Now().UTC()
		if e := mixedWriteJSON(filepath.Join(directory, "report.json"), out); e != nil {
			t.Error(e)
		}
	}
	defer publish()
	for phaseIndex, target := range []int{max(1, n/4), max(2, n/2), n} {
		phase := fmt.Sprintf("players_%d", target)
		metrics.setPhase(phase)
		t.Log("begin phase", phase)
		for active < target {
			p := players[active]
			p.started = time.Now()
			p.request(run, "GET", "/api/galaxy", nil)
			if active == n-1 {
				browserPID = mixedBrowserStart(t, run, root, directory, p)
				metrics.browserPID.Store(int64(browserPID))
			} else {
				wg.Add(1)
				go func() { defer wg.Done(); p.poll(run) }()
				if p.planet {
					site := sites[(p.index/10*3+p.index%10)%len(sites)]
					g, e := mixedLand(run, p, site)
					if e != nil {
						t.Fatalf("planet participant %d: %v", p.index, e)
					}
					p.ground = g
					wg.Add(1)
					go func() { defer wg.Done(); g.run(run) }()
				} else {
					wg.Add(1)
					go func() { defer wg.Done(); p.space(run, neighbors) }()
				}
				c, e := mixedDialChat(run, p)
				if e != nil {
					t.Fatalf("chat participant %d: %v", p.index, e)
				}
				p.chat = c
				wg.Add(1)
				go func() { defer wg.Done(); c.run(run, runID) }()
			}
			active++
		}
		duration := ramp
		if phaseIndex == 2 {
			duration = hold
		}
		began := time.Now()
		tickStart := f.integer(`SELECT tick FROM world`)
		timer := time.NewTimer(duration)
		progress := time.NewTicker(10 * time.Second)
		aborted := ""
	phaseLoop:
		for {
			select {
			case <-timer.C:
				break phaseLoop
			case <-progress.C:
				all, failed := metrics.failures()
				alivePlanet, aliveChat := 0, 0
				for _, p := range players[:active] {
					if p.ground != nil && p.ground.connected.Load() {
						alivePlanet++
					}
					if p.chat != nil && p.chat.connected.Load() {
						aliveChat++
					}
				}
				status := map[string]any{"phase": phase, "elapsed_seconds": int(time.Since(began).Seconds()), "target_seconds": int(duration.Seconds()), "active_users": active, "planet_connections": alivePlanet, "programmatic_chat_connections": aliveChat, "browser_pid": browserPID, "http_requests": all, "http_5xx_transport": failed, "logs_bytes": mixedLogSizes(logs), "at": time.Now().UTC()}
				mixedWriteJSON(filepath.Join(directory, "progress.json"), status)
				if time.Since(began) > 45*time.Second && all > 200 && float64(failed)/float64(all) > .25 {
					aborted = "more than 25% HTTP 5xx/transport failures after warmup"
					break phaseLoop
				}
				for name, p := range processes {
					if _, e := os.Stat(fmt.Sprintf("/proc/%d/stat", p.cmd.Process.Pid)); e != nil {
						aborted = name + " process exited"
						break phaseLoop
					}
				}
			}
		}
		timer.Stop()
		progress.Stop()
		phases = append(phases, map[string]any{"name": phase, "users": active, "seconds": time.Since(began).Seconds(), "ticks_advanced": f.integer(`SELECT tick FROM world`) - tickStart, "aborted": aborted})
		publish()
		t.Log("end phase", phase, "seconds", time.Since(began).Seconds())
		if aborted != "" {
			baseReport["phase_result"] = "stopped_on_overload"
			baseReport["stop_reason"] = aborted
			break
		}
	}
	metrics.stopActions.Store(true)
	metrics.setPhase("drain")
	mixedSleep(run, 10*time.Second)
	cancel()
	wg.Wait()
	stopSample()
	if baseReport["phase_result"] == "running" {
		baseReport["phase_result"] = "completed"
	}
	baseReport["integrity"] = mixedIntegrity(t, f, runID, players)
	baseReport["cgroup_end"] = mixedCgroup()
	publish()
	t.Log("mixed load finished:", directory, "status", baseReport["phase_result"])
}

func mixedIntegrity(t *testing.T, f *fixture033, run string, players []*mixedPlayer) map[string]any {
	t.Helper()
	ids := []int64{}
	for _, p := range players {
		ids = append(ids, p.uid)
	}
	out := map[string]any{}
	for key, sql := range map[string]string{"users": "SELECT count(*) FROM users WHERE id=ANY($1)", "ships": "SELECT count(*) FROM ships WHERE user_id=ANY($1)", "negative_user_balance": "SELECT count(*) FROM users WHERE id=ANY($1) AND credits<0", "negative_ship_cargo": "SELECT count(*) FROM ships s CROSS JOIN LATERAL jsonb_each_text(s.cargo) c WHERE s.user_id=ANY($1) AND c.value::bigint<0", "planner_tasks": "SELECT count(*) FROM automation_tasks WHERE user_id=ANY($1)", "completed_planner_tasks": "SELECT count(*) FROM automation_tasks WHERE user_id=ANY($1) AND status='completed'", "saved_buildings": "SELECT count(*) FROM planet_sector_buildings"} {
		var value int64
		var e error
		if strings.Contains(sql, "$1") {
			e = f.db.QueryRow(f.ctx, sql, ids).Scan(&value)
		} else {
			e = f.db.QueryRow(f.ctx, sql).Scan(&value)
		}
		if e != nil {
			out[key] = "unavailable"
		} else {
			out[key] = value
		}
	}
	var stored, unique int64
	e := f.db.QueryRow(f.ctx, `SELECT count(*),count(DISTINCT id) FROM chat_messages WHERE body LIKE $1`, "m100:"+run+":%").Scan(&stored, &unique)
	if e != nil {
		out["chat_messages"] = "unavailable"
	} else {
		out["chat_messages"] = stored
		out["chat_unique_ids"] = unique
	}
	return out
}
