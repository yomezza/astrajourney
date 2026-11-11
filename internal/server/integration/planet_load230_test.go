package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
)

// This is an opt-in integration experiment, not an admin action against a world.
// newFixture033 rejects non-_test databases and creates its own random schema.
func TestPlanetLoad230(t *testing.T) {
	if os.Getenv("RUN_PLANET_LOAD") != "1" {
		t.Skip("RUN_PLANET_LOAD=1 with an isolated TEST_DATABASE_URL required")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the process experiment and /proc sampling currently require Linux")
	}
	f, owner, uid, surface, site, _ := prepare180(t)
	peer, peerID, peerShip := peer200(t, f, uid, surface)
	company := f.integer(`SELECT company_id FROM users WHERE id=$1`, uid)
	role := f.integer(`SELECT company_role FROM users WHERE id=$1`, uid)
	for _, sector := range []int{2, 4, 6, 8, 10} {
		addColony160(f, uid, surface, sector, &company, false)
	}
	reconcile160(f)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.exec(`UPDATE game_settings SET tick_seconds=2; UPDATE world SET last_tick=clock_timestamp()`)
	type account struct {
		Cookie    *http.Cookie
		UID, Ship int64
	}
	accounts := []account{{owner, uid, f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)}, {peer, peerID, peerShip}}
	for i := 2; i < 50; i++ {
		// Distinct fixture addresses exercise registration policy without weakening
		// the production rate limiter or exposing a public setup endpoint.
		raw, _ := json.Marshal(map[string]string{"username": fmt.Sprintf("load230_%02d", i), "password": "test-only"})
		r := httptest.NewRequest("POST", "/api/register", bytes.NewReader(raw))
		r.RemoteAddr = fmt.Sprintf("198.18.0.%d:1234", i)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("fixture registration HTTP %d", w.Code)
		}
		c := w.Result().Cookies()[0]
		id := f.integer(`SELECT id FROM users WHERE username=$1`, fmt.Sprintf("load230_%02d", i))
		f.req(c, "GET", "/api/expedition", nil, 200)
		f.exec(`UPDATE users SET company_id=$2,company_role=$3 WHERE id=$1`, id, company, role)
		f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1) ON CONFLICT DO NOTHING`, id)
		ship := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, id)
		planet := f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface)
		f.exec(`UPDATE ships SET star=1,object_id=$2,position_mode='orbit',destination=NULL,local_target=NULL,waypoints='[]' WHERE id=$1`, ship, fmt.Sprintf("planet-%d", planet))
		accounts = append(accounts, account{c, id, ship})
	}
	var schema string
	if err := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	var geographyHash string
	if err := f.db.QueryRow(f.ctx, `SELECT md5(data::text) FROM planet_surfaces WHERE id=$1`, surface).Scan(&geographyHash); err != nil {
		t.Fatal(err)
	}
	credits := f.integer(`SELECT sum(credits) FROM users WHERE NOT is_bot`)
	ships := f.integer(`SELECT count(*) FROM ships s JOIN users u ON u.id=s.user_id WHERE NOT u.is_bot`)
	colonies := f.integer(`SELECT count(*) FROM planet_colonies`)
	modules := f.integer(`SELECT count(*) FROM planet_landing_sites WHERE enabled`)
	users := f.integer(`SELECT count(*) FROM users`)
	spaceBinary := buildLoad230(t, "astra-journey")
	planetBinary := buildLoad230(t, "astra-journey-planet")
	spaceURL, planetURL := loadPort230(t), loadPort230(t)
	dsn, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	q := dsn.Query()
	q.Set("pool_max_conns", "8")
	dsn.RawQuery = q.Encode()
	common := map[string]string{"DATABASE_URL": dsn.String(), "DB_SCHEMA": schema, "ADMIN_USERNAME": "", "ADMIN_PASSWORD": "", "PUBLIC_ORIGIN": spaceURL, "COOKIE_SECURE": "false", "TRUSTED_PROXIES": "", "PLANET_ENABLED": "true", "PLANET_INTERNAL_URL": planetURL}
	spaceEnv := copyEnv230(common)
	spaceEnv["HTTP_ADDR"] = strings.TrimPrefix(spaceURL, "http://")
	planetEnv := copyEnv230(common)
	for k, v := range map[string]string{"PLANET_HTTP_ADDR": strings.TrimPrefix(planetURL, "http://"), "PLANET_ALLOWED_ORIGINS": spaceURL, "PLANET_DB_TIMEOUT": "2s", "PLANET_LEASE_TTL": "9s", "PLANET_LEASE_RENEW": "3s", "PLANET_SHUTDOWN_TIMEOUT": "8s", "PLANET_SCENE_IDLE_TTL": "2s", "PLANET_SCENE_CAPACITY": "32", "PLANET_LOG_FILE": "", "ADMIN_USERNAME": "planet_must_not_bootstrap", "ADMIN_PASSWORD": "test-not-used-by-planet"} {
		planetEnv[k] = v
	}
	space := startLoadProcess230(t, spaceBinary, spaceEnv)
	waitLoadReady230(t, spaceURL, "/api/health")
	planet := startLoadProcess230(t, planetBinary, planetEnv)
	var planetPID atomic.Int64
	planetPID.Store(int64(planet.cmd.Process.Pid))
	waitLoadReady230(t, planetURL, "/planet/api/v1/health/ready")
	if f.integer(`SELECT count(*) FROM users`) != users {
		t.Fatal("a process unexpectedly bootstrapped an account")
	}
	requireLoad230(t, spaceURL, nil, "GET", "/api/admin/metrics", nil, 401)
	requireLoad230(t, spaceURL, peer, "GET", "/api/admin/metrics", nil, 403)
	requireLoad230(t, spaceURL, nil, "GET", "/planet/api/v1/metrics", nil, 401)
	requireLoad230(t, spaceURL, peer, "GET", "/planet/api/v1/metrics", nil, 403)
	report := map[string]any{"clients": 50, "version": "0.23.0", "started_at": time.Now().UTC(), "go": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "pool_max_per_process": 8, "planet_idle_ttl_seconds": 2, "cosmic_tick_seconds": 2, "phase_result": "incomplete", "browser_rendering_tested": false}
	for _, name := range []string{"cpu.max", "memory.max"} {
		if raw, e := os.ReadFile("/sys/fs/cgroup/" + name); e == nil {
			report["cgroup_"+strings.ReplaceAll(name, ".", "_")] = strings.TrimSpace(string(raw))
		}
	}
	if raw, e := os.ReadFile("/proc/cpuinfo"); e == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "model name") {
				_, model, _ := strings.Cut(line, ":")
				report["cpu_model"] = strings.TrimSpace(model)
				break
			}
		}
	}
	var reportMu sync.Mutex
	var samples []map[string]any
	var phases []map[string]any
	var phaseName string
	var phaseMu sync.RWMutex
	defer func() {
		report["samples"] = samples
		report["phases"] = phases
		report["finished_at"] = time.Now().UTC()
		writeLoadReport230(t, report)
	}()
	sampleCtx, stopSamples := context.WithCancel(context.Background())
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
				phaseMu.RLock()
				name := phaseName
				phaseMu.RUnlock()
				a, sa, _ := loadRequest230(spaceURL, owner, "GET", "/api/admin/metrics", nil)
				b, sb, _ := loadRequest230(spaceURL, owner, "GET", "/planet/api/v1/metrics", nil)
				v := map[string]any{"at": time.Now().UTC(), "phase": name, "cosmic": a, "planet": b, "cosmic_http": sa, "planet_http": sb, "cosmic_process": processUsageLoad230(space.cmd.Process.Pid), "planet_process": processUsageLoad230(int(planetPID.Load()))}
				reportMu.Lock()
				samples = append(samples, v)
				reportMu.Unlock()
			}
		}
	}()
	defer func() { stopSamples(); <-sampled }()
	phase := func(name string, action func()) {
		phaseMu.Lock()
		phaseName = name
		phaseMu.Unlock()
		begin := time.Now()
		tick := f.integer(`SELECT tick FROM world`)
		a := requireLoad230(t, spaceURL, owner, "GET", "/api/admin/metrics", nil, 200)
		b := requireLoad230(t, spaceURL, owner, "GET", "/planet/api/v1/metrics", nil, 200)
		pid := planet.cmd.Process.Pid
		cpuA, cpuB := processUsageLoad230(space.cmd.Process.Pid), processUsageLoad230(pid)
		action()
		elapsed := time.Since(begin).Seconds()
		endA := requireLoad230(t, spaceURL, owner, "GET", "/api/admin/metrics", nil, 200)
		endB := requireLoad230(t, spaceURL, owner, "GET", "/planet/api/v1/metrics", nil, 200)
		uA, uB := processUsageLoad230(space.cmd.Process.Pid), processUsageLoad230(planet.cmd.Process.Pid)
		p := map[string]any{"name": name, "seconds": elapsed, "cosmic_ticks": f.integer(`SELECT tick FROM world`) - tick, "cosmic_before": a, "planet_before": b, "cosmic_after": endA, "planet_after": endB, "cosmic_average_cpu_cores": math.Max(0, uA.CPUSeconds-cpuA.CPUSeconds) / elapsed, "planet_average_cpu_cores": math.Max(0, uB.CPUSeconds-cpuB.CPUSeconds) / elapsed}
		if pid != planet.cmd.Process.Pid {
			p["runtime_restarted"] = true
			p["planet_average_cpu_cores"] = nil
		}
		phases = append(phases, p)
		reportMu.Lock()
		snapshot := append([]map[string]any(nil), samples...)
		reportMu.Unlock()
		progress := map[string]any{"phase_result": "running", "phases": phases, "samples": snapshot}
		writeLoadReport230(t, progress)
		t.Logf("phase %s: %.2fs", name, elapsed)
	}
	latency := &telemetry.Samples{}
	var clients []*loadClient230
	var allClients []*loadClient230
	var clientsMu sync.Mutex
	defer func() {
		for _, c := range allClients {
			c.expected.Store(true)
			c.c.CloseNow()
		}
	}()
	connect := func(resume bool, sites []int64) {
		next := make([]*loadClient230, 50)
		loadParallel230(t, 50, func(i int) error {
			a := accounts[i]
			var hello any
			if resume {
				hello = map[string]string{"resume_visit": clients[i].welcome().Visit}
			} else {
				op, e := accessUUIDLoad230()
				if e != nil {
					return e
				}
				v, status, e := loadRequest230(spaceURL, a.Cookie, "POST", "/api/planet/handoff", map[string]any{"operation": op, "landing_site": fmt.Sprint(sites[i%len(sites)]), "ship": fmt.Sprint(a.Ship)})
				if e != nil || status != 200 {
					return fmt.Errorf("handoff: HTTP %d", status)
				}
				hello = map[string]any{"ticket": v["ticket"]}
			}
			v, e := dialLoad230(spaceURL, a.Cookie, a.UID, a.Ship, hello, latency)
			if e != nil {
				return e
			}
			next[i] = v
			clientsMu.Lock()
			allClients = append(allClients, v)
			clientsMu.Unlock()
			if _, e = v.wait("sector.snapshot", 10*time.Second); e != nil {
				return e
			}
			w := v.welcome()
			_, status, e := loadRequest230(spaceURL, a.Cookie, "GET", fmt.Sprintf("/planet/api/v1/scene?visit=%s&epoch=%d", w.Visit, w.Epoch), nil)
			if e != nil || status != 200 {
				return fmt.Errorf("scene: HTTP %d", status)
			}
			return nil
		})
		clients = next
	}
	closeClients := func() {
		loadParallel230(t, len(clients), func(i int) error { clients[i].close(); return nil })
		waitConditionLoad230(t, 8*time.Second, func() bool { return f.integer(`SELECT count(*) FROM planet_visits WHERE state='active'`) == 0 })
	}
	returnClients := func() {
		loadParallel230(t, len(clients), func(i int) error {
			c := clients[i]
			if e := c.send("leave", map[string]string{"operation": mustUUIDLoad230()}); e != nil {
				return e
			}
			if _, e := c.wait("visit.ended", 10*time.Second); e != nil {
				return e
			}
			c.close()
			return nil
		})
		waitConditionLoad230(t, 8*time.Second, func() bool {
			return f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship IS NOT NULL`) == 0
		})
	}
	checkPresence := func(want int) {
		loadParallel230(t, len(clients), func(i int) error {
			c := clients[i]
			if e := c.send("resync", map[string]int{"last_server_seq": 0}); e != nil {
				return e
			}
			if _, e := c.wait("sector.snapshot", 10*time.Second); e != nil {
				return e
			}
			return nil
		})
		for _, c := range clients {
			n, errors := c.counts()
			if n != want || errors != 0 || c.faults.Load() != 0 {
				t.Fatalf("presence/transport: actors=%d want=%d errors=%d faults=%d", n, want, errors, c.faults.Load())
			}
		}
	}
	phase("baseline", func() { time.Sleep(4 * time.Second) })
	phase("cold_simultaneous_landing", func() { connect(false, []int64{site}); checkPresence(50) })
	decode := func(sector int) *scene.Scene {
		var raw, hash []byte
		if e := f.db.QueryRow(f.ctx, `SELECT geometry,geometry_checksum FROM planet_local_locations WHERE surface=$1 AND sector=$2`, surface, sector).Scan(&raw, &hash); e != nil {
			t.Fatal(e)
		}
		if len(hash) != sha256.Size {
			t.Fatal("invalid persisted scene hash")
		}
		s, e := scene.Decode(raw, [32]byte(hash))
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	local := map[int]*scene.Scene{1: decode(1)}
	points := func(s *scene.Scene) []pathing.Point {
		p := s.Metadata().Landing
		if p == nil {
			t.Fatal("fixture candidate missing")
		}
		center := pathing.FromQ(p.SpawnX, p.SpawnY)
		v := []pathing.Point{center}
		for r := 2; r <= 8 && len(v) < 3; r += 2 {
			for _, d := range [][2]int{{r, 0}, {-r, 0}, {0, r}, {r, r}, {-r, r}} {
				p := pathing.Point{X: math.Floor(center.X) + .5 + float64(d[0]), Y: math.Floor(center.Y) + .5 + float64(d[1])}
				if pathing.Clear(s, p) && pathing.Segment(s, center, p) {
					v = append(v, p)
					if len(v) == 3 {
						break
					}
				}
			}
		}
		if len(v) < 3 {
			t.Fatal("fixture lacks short safe walking targets")
		}
		return v
	}
	walk := func(duration time.Duration, period time.Duration) {
		end := time.Now().Add(duration)
		step := 0
		for time.Now().Before(end) {
			begin := time.Now()
			loadParallel230(t, len(clients), func(i int) error {
				c := clients[i]
				p := points(local[c.welcome().Sector])
				x, y := p[(step+i)%len(p)].Quantized()
				return c.send("move", map[string]int{"x_q": x, "y_q": y})
			})
			step++
			if delay := period - time.Since(begin); delay > 0 {
				time.Sleep(delay)
			}
		}
		checkPresence(50)
	}
	phase("one_sector_steady_50_moves_per_second", func() { walk(12*time.Second, time.Second) })
	phase("one_sector_burst_250_moves_per_second", func() { walk(5*time.Second, 200*time.Millisecond) })
	phase("disconnect_idle_eviction", func() {
		closeClients()
		waitConditionLoad230(t, 8*time.Second, func() bool {
			m := requireLoad230(t, spaceURL, owner, "GET", "/planet/api/v1/metrics", nil, 200)
			return numberMap230(m, "movement", "actors") == 0 && numberMap230(m, "scenes", "cached") == 0
		})
	})
	phase("reconnect_storm", func() { connect(true, nil); checkPresence(50) })
	phase("return_and_five_sector_landing", func() {
		returnClients()
		var sites []int64
		rows, e := f.db.Query(f.ctx, `SELECT id FROM planet_landing_sites WHERE surface=$1 AND enabled ORDER BY sector`, surface)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			sites = append(sites, id)
		}
		rows.Close()
		if len(sites) != 5 {
			t.Fatalf("fixture has %d sites", len(sites))
		}
		connect(false, sites)
		for _, sector := range []int{1, 4, 6, 8, 10} {
			local[sector] = decode(sector)
		}
		checkPresence(10)
	})
	phase("five_sectors_steady_50_moves_per_second", func() {
		end := time.Now().Add(12 * time.Second)
		step := 0
		for time.Now().Before(end) {
			loadParallel230(t, 50, func(i int) error {
				c := clients[i]
				ps := points(local[c.welcome().Sector])
				x, y := ps[(step+i)%len(ps)].Quantized()
				return c.send("move", map[string]int{"x_q": x, "y_q": y})
			})
			step++
			time.Sleep(time.Second)
		}
		checkPresence(10)
	})
	phase("return_to_common_sector", func() { returnClients(); connect(false, []int64{site}); checkPresence(50) })
	var portal scene.Position
	for _, p := range local[1].Portals() {
		if p.Neighbor == 2 {
			portal = p.Ingress
		}
	}
	if portal.X == 0 {
		t.Fatal("fixture portal missing")
	}
	phase("walk_and_50_confirmed_transitions", func() {
		loadParallel230(t, 50, func(i int) error {
			c := clients[i]
			if e := c.send("move", map[string]int{"x_q": portal.X, "y_q": portal.Y}); e != nil {
				return e
			}
			var offer map[string]any
			for {
				p, e := c.wait("transition.offer", 60*time.Second)
				if e != nil {
					return e
				}
				if p["ready"] == true {
					offer = p
					break
				}
			}
			op := mustUUIDLoad230()
			payload := map[string]any{"offer": offer["offer"], "operation": op}
			if e := c.send("transition.confirm", payload); e != nil {
				return e
			}
			if _, e := c.wait("transition.complete", 10*time.Second); e != nil {
				return e
			}
			if e := c.send("transition.confirm", payload); e != nil {
				return e
			}
			_, e := c.wait("transition.complete", 10*time.Second)
			return e
		})
		checkPresence(50)
		if f.integer(`SELECT count(*) FROM planet_transition_receipts`) != 50 || f.integer(`SELECT count(*) FROM planet_visits WHERE state='active' AND current_sector=2`) != 50 {
			t.Fatal("transition duplicated or lost actors")
		}
	})
	local[2] = decode(2)
	walkInward := func(c *loadClient230, steps int) error {
		c.mu.Lock()
		from := pathing.FromQ(c.state.X, c.state.Y)
		c.mu.Unlock()
		p := local[2].Metadata().Landing
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		path, e := pathing.Find(ctx, local[2], from, pathing.FromQ(p.SpawnX, p.SpawnY), 24000)
		if e != nil {
			return e
		}
		x, y := path[min(steps, len(path)-1)].Quantized()
		if e = c.send("move", map[string]int{"x_q": x, "y_q": y}); e != nil {
			return e
		}
		_, e = c.wait("path.accepted", 10*time.Second)
		return e
	}
	// Move inward from the edge. Each target must be reachable via normal A*.
	phase("checkpoint_before_crash", func() {
		loadParallel230(t, 50, func(i int) error {
			return walkInward(clients[i], 4)
		})
		time.Sleep(7 * time.Second)
		checkPresence(50)
	})
	oldFence := f.integer(`SELECT fence FROM planet_runtime_leases`)
	phase("hard_crash_and_50_resume", func() {
		loadParallel230(t, 50, func(i int) error {
			return walkInward(clients[i], 12)
		})
		time.Sleep(250 * time.Millisecond)
		for _, c := range clients {
			c.expected.Store(true)
		}
		planet.stop(t, true)
		var saved = map[int64][2]int{}
		for _, a := range accounts {
			var x, y int
			if e := f.db.QueryRow(f.ctx, `SELECT x_q,y_q FROM planet_characters WHERE user_id=$1`, a.UID).Scan(&x, &y); e != nil {
				t.Fatal(e)
			}
			saved[a.UID] = [2]int{x, y}
		}
		before := f.integer(`SELECT tick FROM world`)
		requireLoad230(t, spaceURL, owner, "GET", "/api/fleet", nil, 200)
		time.Sleep(3 * time.Second)
		if f.integer(`SELECT tick FROM world`) <= before {
			t.Fatal("cosmic tick stopped with planet")
		}
		planet = startLoadProcess230(t, planetBinary, planetEnv)
		planetPID.Store(int64(planet.cmd.Process.Pid))
		waitLoadReady230(t, planetURL, "/planet/api/v1/health/ready")
		previous := clients
		connect(true, nil)
		for i, c := range clients {
			w, old := c.welcome(), previous[i].welcome()
			if w.Actor != old.Actor || w.Visit != old.Visit || w.Epoch <= old.Epoch || w.Sector != 2 || [2]int{w.X, w.Y} != saved[c.uid] {
				t.Fatal("crash recovery changed stable identity, location or checkpoint")
			}
		}
		if f.integer(`SELECT fence FROM planet_runtime_leases`) <= oldFence {
			t.Fatal("replacement runtime did not fence old process")
		}
		checkPresence(50)
	})
	phase("final_return_and_eviction", func() {
		returnClients()
		waitConditionLoad230(t, 8*time.Second, func() bool {
			m := requireLoad230(t, spaceURL, owner, "GET", "/planet/api/v1/metrics", nil, 200)
			return numberMap230(m, "movement", "actors") == 0 && numberMap230(m, "scenes", "cached") == 0
		})
	})
	var afterHash string
	_ = f.db.QueryRow(f.ctx, `SELECT md5(data::text) FROM planet_surfaces WHERE id=$1`, surface).Scan(&afterHash)
	invariants := map[string][2]int64{
		"player_credits": {credits, f.integer(`SELECT sum(credits) FROM users WHERE NOT is_bot`)},
		"player_ships":   {ships, f.integer(`SELECT count(*) FROM ships s JOIN users u ON u.id=s.user_id WHERE NOT u.is_bot`)},
		"colonies":       {colonies, f.integer(`SELECT count(*) FROM planet_colonies`)},
		"landing_sites":  {modules, f.integer(`SELECT count(*) FROM planet_landing_sites WHERE enabled`)},
		"characters":     {50, f.integer(`SELECT count(*) FROM planet_characters`)},
		"live_visits":    {0, f.integer(`SELECT count(*) FROM planet_visits WHERE state<>'ended'`)},
	}
	report["invariants_expected_actual"] = invariants
	if afterHash != geographyHash {
		t.Error("load test changed canonical geography")
	}
	for name, pair := range invariants {
		if pair[0] != pair[1] {
			t.Errorf("invariant %s: expected %d, actual %d", name, pair[0], pair[1])
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	for _, a := range accounts {
		var sector, x, y int
		if e := f.db.QueryRow(f.ctx, `SELECT last_sector,x_q,y_q FROM planet_characters WHERE user_id=$1`, a.UID).Scan(&sector, &x, &y); e != nil {
			t.Fatal(e)
		}
		if !pathing.Clear(local[sector], pathing.FromQ(x, y)) {
			t.Fatal("checkpoint is not physically walkable")
		}
	}
	for index, c := range allClients {
		if c.faults.Load() != 0 {
			t.Fatalf("connection %d: unexpected_close=%d sequence=%d speed=%d event_overflow=%d total=%d", index, c.closeFaults.Load(), c.sequenceFaults.Load(), c.speedFaults.Load(), c.overflowFaults.Load(), c.faults.Load())
		}
	}
	report["command_ack_latency"] = latency.Snapshot()
	report["characters_after"] = 50
	report["live_visits_after"] = 0
	report["transition_receipts_after"] = 50
	report["geography_unchanged"] = true
	report["player_credits_unchanged"] = true
	stopSamples()
	<-sampled
	// Stop the owned cosmic executable: the planetary listener remains independent.
	space.stop(t, false)
	waitLoadReady230(t, planetURL, "/planet/api/v1/health/ready")
	planet.stop(t, false)
	report["phase_result"] = "passed"
}

func copyEnv230(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func numberMap230(in map[string]any, group, key string) float64 {
	m, _ := in[group].(map[string]any)
	n, _ := m[key].(float64)
	return n
}
func accessUUIDLoad230() (string, error) { return access.UUID() }
func mustUUIDLoad230() string {
	v, e := access.UUID()
	if e != nil {
		panic(e)
	}
	return v
}
