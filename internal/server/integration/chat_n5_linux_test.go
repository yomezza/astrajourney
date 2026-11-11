package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/pelletier/go-toml/v2"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func n5Wire(t *testing.T, b *natsfixture.Broker) *nats.Conn {
	t.Helper()
	roots := x509.NewCertPool()
	raw, e := os.ReadFile(b.CAFile)
	if e != nil || !roots.AppendCertsFromPEM(raw) {
		t.Fatal("CA fixture")
	}
	opts := []nats.Option{nats.UserInfo("n3_test_operator", b.OperatorPassword), nats.NoReconnect(), nats.Timeout(time.Second)}
	if strings.HasPrefix(b.URL, "tls://") {
		opts = append(opts, nats.Secure(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}))
	}
	nc, e := nats.Connect(b.URL, opts...)
	if e != nil {
		t.Fatal("owned broker unavailable")
	}
	t.Cleanup(nc.Close)
	return nc
}
func n5Build(t *testing.T, pkg, name string, testBinary bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	args := []string{"build", "-race", "-o", path, pkg}
	if testBinary {
		args = []string{"test", "-c", "-race", "-o", path, pkg}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("..", "..", "..")
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("native test build failed: %s", output)
	}
	return path
}
func n5Port(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().String()
}
func n5Config(t *testing.T, document map[string]any) string {
	t.Helper()
	raw, e := toml.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "service.toml")
	if e = os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func n5Process(t *testing.T, binary, config, healthURL string, observe ...func(*os.Process)) func() {
	t.Helper()
	log, e := os.OpenFile(filepath.Join(t.TempDir(), "service.log"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { log.Close() })
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "CONFIG_FILE="+config)
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		t.Fatal("native service start failed")
	}
	for _, watch := range observe {
		watch(cmd.Process)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case e := <-done:
				if e != nil {
					t.Error("native service failed on exit")
				}
			case <-time.After(10 * time.Second):
				cmd.Process.Kill()
				<-done
				t.Error("native service shutdown timeout")
			}
		})
	}
	t.Cleanup(stop)
	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(12 * time.Second)
	for {
		r, e := client.Get(healthURL)
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return stop
			}
		}
		select {
		case <-done:
			once.Do(func() {})
			t.Fatal("native service exited before readiness; log retained in test temp directory")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("native service readiness timeout")
		}
		time.Sleep(30 * time.Millisecond)
	}
}
func n5HTTP(t *testing.T, base, route, origin string, c *http.Cookie) (int, []byte) {
	t.Helper()
	r, e := http.NewRequest("GET", base+"/chat/api/v1/"+route, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.AddCookie(c)
	r.Header.Set("Origin", origin)
	response, e := (&http.Client{Timeout: 4 * time.Second}).Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body
}
func n5Presence(t *testing.T, base, origin string, c *http.Cookie) int {
	t.Helper()
	code, raw := n5HTTP(t, base, "bootstrap", origin, c)
	if code != 200 {
		t.Fatal("native bootstrap failed", code)
	}
	var v struct {
		Presence []struct {
			Sector int `json:"sector"`
		} `json:"presence"`
	}
	if json.Unmarshal(raw, &v) != nil {
		t.Fatal("bootstrap JSON")
	}
	if len(v.Presence) == 0 {
		return 0
	}
	return v.Presence[0].Sector
}
func n5AtPortal(t *testing.T, f *fixture033, base string, c *http.Cookie, uid int64, w map[string]any, old *websocket.Conn, to int) (*websocket.Conn, map[string]any) {
	t.Helper()
	old.CloseNow()
	// An independently committed transition has a newer epoch than this old
	// socket. Its teardown must not rewrite that authoritative visit state.
	eventuallyN1(t, func() bool {
		return f.integer(`SELECT count(*) FROM planet_visits WHERE user_id=$1 AND (state='disconnected' OR connection_epoch<>$2)`, uid, int64(w["connection_epoch"].(float64))) == 1
	})
	var surface, revision int64
	var sector int
	var fence scenestore.Fence
	if e := f.db.QueryRow(f.ctx, `SELECT current_surface,current_sector,revision FROM planet_visits WHERE user_id=$1`, uid).Scan(&surface, &sector, &revision); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(f.ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); e != nil {
		t.Fatal(e)
	}
	source, e := scenestore.New(f.db).Load(f.ctx, sceneKeyN5(surface, revision, sector), fence)
	if e != nil {
		t.Fatal(e)
	}
	x, y := 0, 0
	for _, p := range source.Portals() {
		if p.Neighbor == to {
			x, y = p.Ingress.X, p.Ingress.Y
		}
	}
	if x == 0 {
		t.Fatal("fixture has no requested portal")
	}
	f.exec(`UPDATE planet_characters SET x_q=$2,y_q=$3,position_version=position_version+1 WHERE user_id=$1`, uid, x, y)
	return ws180(t, base, c, map[string]any{"resume_visit": w["visit"]})
}
func n5Receive(t *testing.T, events <-chan []byte, operation string) gamesignal.PlanetSignal {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-events:
			s, ok := gamesignal.DecodePlanet(gamesignal.PlanetSectorSubject, raw)
			if !ok {
				continue
			}
			if s.SignalID == operation {
				return s
			}
		case <-deadline:
			t.Fatal("missing actual Planet Core signal")
		}
	}
}
func n5NoSignal(t *testing.T, events <-chan []byte) {
	t.Helper()
	select {
	case <-events:
		t.Fatal("unconfirmed transition published")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestN5NativePlanetNATSChat(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	broker := natsfixture.New(t, natsfixture.Options{PlanetSignals: true})
	planetBinary := n5Build(t, "./cmd/cosmosimulator-planet", "planet", false)
	chatBinary := n5Build(t, "./cmd/cosmosimulator-chat", "chat", false)
	probeBinary := n5Build(t, "./internal/planetserver", "planet-probe.test", true)
	f, c, uid, surface, site, ship := prepare180(t)
	for _, sector := range []int{2, 3, 4, 5, 6, 7} {
		addColony160(f, uid, surface, sector, nil, false)
	}
	reconcile160(f)
	peer, peerID, peerShip := peer200(t, f, uid, surface)
	_, role, _ := n1RuntimePool(t, f)
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	u, e := url.Parse(dsn)
	if e != nil || u.Scheme != "postgres" {
		t.Fatal("native fixture expects a postgres:// TCP test DSN")
	}
	q := u.Query()
	q.Set("role", role)
	u.RawQuery = q.Encode()
	chatDSN := u.String()
	planetAddr, chatAddr := n5Port(t), n5Port(t)
	planetURL, chatURL := "http://"+planetAddr, "http://"+chatAddr
	cosmic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}))
	defer cosmic.Close()
	natsDoc := func(identity, passwordFile string) map[string]any {
		return map[string]any{"enabled": true, "service_identity": identity, "username": identity, "servers": []string{broker.URL}, "password_file": passwordFile, "ca_file": broker.CAFile, "connect_timeout": "500ms", "reconnect_wait": "250ms"}
	}
	planetDoc := map[string]any{"database": map[string]any{"url": dsn, "schema": schema}, "planet": map[string]any{"http_addr": planetAddr, "allowed_origins": []string{"http://127.0.0.1:8080"}, "db_timeout": "200ms", "lease_ttl": "3s", "lease_renew": "1s", "shutdown_timeout": "2s"}, "nats": natsDoc("planet", broker.PlanetPasswordFile)}
	planetConfig := n5Config(t, planetDoc)
	// Start Planet with the broker already offline: game readiness must survive.
	broker.Stop()
	var planetProcess *os.Process
	stopPlanet := n5Process(t, planetBinary, planetConfig, planetURL+"/planet/api/v1/health/ready", func(p *os.Process) { planetProcess = p })
	conn, w, _, _ := edge210(t, f, c, uid, surface, site, ship, planetURL)
	peerConn, _ := ws180(t, planetURL, peer, map[string]any{"ticket": ticket180(t, f, peer, op180(t), site, peerShip)["ticket"]})
	defer peerConn.CloseNow()
	broker.Start()
	wire := n5Wire(t, broker)
	events := make(chan []byte, 16)
	sub, e := wire.Subscribe(gamesignal.PlanetSectorSubject, func(m *nats.Msg) {
		select {
		case events <- append([]byte(nil), m.Data...):
		default:
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer sub.Unsubscribe()
	wire.FlushTimeout(time.Second)
	chatDoc := map[string]any{"database": map[string]any{"url": chatDSN, "schema": schema}, "chat": map[string]any{"http_addr": chatAddr, "cosmic_internal_url": cosmic.URL, "planet_internal_url": planetURL, "allowed_origins": []string{cosmic.URL}, "erasure_journal": filepath.Join(t.TempDir(), "preserved-erasure.log"), "dispatcher_mode": "b1", "fallback_poll_interval": "1s", "degraded_poll_interval": "500ms"}, "nats": natsDoc("chat", broker.PasswordFile)}
	chatConfig := n5Config(t, chatDoc)
	stopChat := n5Process(t, chatBinary, chatConfig, chatURL+"/chat/api/v1/health")
	n5CoreReady(t, broker)
	pilot, other := n1Dial(t, chatURL, cosmic.URL, c.Value), n1Dial(t, chatURL, cosmic.URL, peer.Value)
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > 0 && other.syncs.Load() > 0 })
	if n5Presence(t, chatURL, cosmic.URL, c) != 1 || n5Presence(t, chatURL, cosmic.URL, peer) != 1 {
		t.Fatal("initial native scope")
	}
	offer := readyOffer210(t, conn)
	command190(t, conn, w, 2, "transition.cancel", map[string]any{"offer": offer["offer"]})
	read190(t, conn, "transition.cancelled")
	n5NoSignal(t, events)
	conn, w = n5AtPortal(t, f, planetURL, c, uid, w, conn, 2)
	offer = readyOffer210(t, conn)
	op12 := op180(t)
	before, peerBefore := pilot.syncs.Load(), other.syncs.Load()
	command190(t, conn, w, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": op12})
	complete := read190(t, conn, "transition.complete")
	w = complete["welcome"].(map[string]any)
	ev12 := n5Receive(t, events, op12)
	if ev12.Data.User() != uid || ev12.Data.From != 1 || ev12.Data.To != 2 || ev12.Data.Epoch != fmt.Sprint(w["connection_epoch"]) || f.integer(`SELECT count(*) FROM planet_transition_receipts WHERE operation=$1`, op12) != 1 {
		t.Fatal("signal before matching SQL receipt")
	}
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > before })
	if other.syncs.Load() != peerBefore {
		t.Fatal("Planet event refreshed another user's Chat")
	}
	if n5Presence(t, chatURL, cosmic.URL, c) != 2 {
		t.Fatal("native Chat did not resolve committed sector")
	}
	route := fmt.Sprintf("history?kind=sector&target=%d/1", surface)
	if status, _ := n5HTTP(t, chatURL, route, cosmic.URL, c); status == 200 {
		t.Fatal("old sector history allowed")
	}
	n5OldSectorSendDenied(t, pilot, surface)
	if status, _ := n5HTTP(t, chatURL, fmt.Sprintf("history?kind=sector&target=%d/2", surface), cosmic.URL, peer); status == 200 {
		t.Fatal("other user's sector leaked")
	}
	// Cached duplicate confirm must not create a second receipt or publication.
	command190(t, conn, w, 3, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": op12})
	read190(t, conn, "transition.complete")
	n5NoSignal(t, events)
	// Real rollback after visit/character UPDATE: an uncommitted conflicting
	// receipt blocks INSERT until the service's SQL deadline, then everything rolls back.
	conn, w = n5AtPortal(t, f, planetURL, c, uid, w, conn, 3)
	offer = readyOffer210(t, conn)
	blockedOp := op180(t)
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	if _, e = tx.Exec(f.ctx, `INSERT INTO planet_transition_receipts(user_id,operation,offer,visit_id,from_sector,to_sector,surface,revision,source_x_q,source_y_q,arrival_x_q,arrival_y_q,connection_epoch) SELECT user_id,$1,$2,visit_id,from_sector,to_sector,surface,revision,source_x_q,source_y_q,arrival_x_q,arrival_y_q,connection_epoch FROM planet_transition_receipts WHERE operation=$3`, blockedOp, offer["offer"], op12); e != nil {
		t.Fatal(e)
	}
	command190(t, conn, w, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": blockedOp})
	read190(t, conn, "transition.cancelled")
	tx.Rollback(f.ctx)
	if f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 2 || f.integer(`SELECT count(*) FROM planet_transition_receipts WHERE operation=$1`, blockedOp) != 0 {
		t.Fatal("partial rollback")
	}
	n5NoSignal(t, events)
	conn, w = n5AtPortal(t, f, planetURL, c, uid, w, conn, 3)
	offer = readyOffer210(t, conn)
	op23 := op180(t)
	command190(t, conn, w, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": op23})
	w = read190(t, conn, "transition.complete")["welcome"].(map[string]any)
	ev23 := n5Receive(t, events, op23)
	// Duplicate, out-of-order and invalid events are hints, never state replay.
	raw12, _ := json.Marshal(ev12)
	raw23, _ := json.Marshal(ev23)
	wire.Publish(gamesignal.PlanetSectorSubject, raw23)
	wire.Publish(gamesignal.PlanetSectorSubject, raw12)
	wire.Publish(gamesignal.PlanetSectorSubject, raw12)
	wire.Publish(gamesignal.PlanetSectorSubject, []byte(`{"schema_version":9}`))
	wire.Publish(gamesignal.PlanetSectorSubject, []byte(strings.Repeat("x", 2049)))
	wire.FlushTimeout(time.Second)
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 3 })
	if f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 3 {
		t.Fatal("old event rewound state")
	}
	// Simulate a crash after SQL COMMIT before the composition hook. This
	// invokes the same unmodified SQL transition service without a publisher.
	conn, w = n5AtPortal(t, f, planetURL, c, uid, w, conn, 4)
	_ = readyOffer210(t, conn)
	lostOp := op180(t)
	n5TransitionWithoutPublish(t, f, c, uid, 4, lostOp)
	n5NoOperation(t, events, lostOp)
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 4 })
	conn, w = n5AtPortal(t, f, planetURL, c, uid, w, conn, 5)
	// P02-09 runs the real commitCrossing against a deliberately closed private
	// RAM hub in a TEST binary, while authoritative SQL/service stay alive.
	_ = readyOffer210(t, conn)
	ramOperation := op180(t)
	probeInput := map[string]any{"DSN": dsn, "Schema": schema, "Operation": ramOperation, "User": uid, "Neighbor": 5, "NATS": natscore.Config{Enabled: true, ServiceIdentity: "planet", Username: "planet", Servers: []string{broker.URL}, PasswordFile: broker.PlanetPasswordFile, CAFile: broker.CAFile, ConnectTimeout: 500 * time.Millisecond, ReconnectWait: 250 * time.Millisecond}}
	raw, _ := json.Marshal(probeInput)
	path := filepath.Join(t.TempDir(), "probe.json")
	os.WriteFile(path, raw, 0600)
	probe := exec.Command(probeBinary, "-test.run=^TestN5PostcommitJoinFailureProbe$")
	probe.Env = append(os.Environ(), "N5_JOIN_PROBE="+path)
	if output, e := probe.CombinedOutput(); e != nil {
		t.Fatalf("postcommit RAM failure probe failed: %s", output)
	}
	ramSignal := n5Receive(t, events, ramOperation)
	if ramSignal.Data.To != 5 || f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 5 {
		t.Fatal("RAM failure lost authoritative transition")
	}
	// Chat restart after a missed Core signal uses current SQL bootstrap.
	conn.CloseNow()
	stopChat()
	stopChat = n5Process(t, chatBinary, chatConfig, chatURL+"/chat/api/v1/health")
	fresh := n1Dial(t, chatURL, cosmic.URL, c.Value)
	eventuallyN1(t, func() bool { return fresh.syncs.Load() > 0 })
	// The existing Planet sweep keeps an authorised committed active visit fresh,
	// even when a previous RAM join failed. Bootstrap must follow that SQL state.
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 5 })
	// Resume at the committed sector with a newer epoch, then lose both services.
	resumed, wr := ws180(t, planetURL, c, map[string]any{"resume_visit": w["visit"]})
	defer resumed.CloseNow()
	if wr["sector"] != float64(5) {
		t.Fatal("resume ignored committed target")
	}
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 5 })
	// A fresh Chat has no previous dedupe cache: an old epoch is still only a hint.
	wire.Publish(gamesignal.PlanetSectorSubject, raw12)
	wire.FlushTimeout(time.Second)
	if n5Presence(t, chatURL, cosmic.URL, c) != 5 {
		t.Fatal("old epoch after Chat restart granted old scope")
	}
	// Pause ONLY the owned test process while testing checked_at expiry. No
	// protocol/debug setter exists in the product; resume before lease expiry.
	func() {
		if e := syscall.Kill(planetProcess.Pid, syscall.SIGSTOP); e != nil {
			t.Fatal(e)
		}
		defer syscall.Kill(planetProcess.Pid, syscall.SIGCONT)
		f.exec(`UPDATE planet_visits SET checked_at=clock_timestamp()-interval '3 seconds' WHERE user_id=$1`, uid)
		if n5Presence(t, chatURL, cosmic.URL, c) != 0 {
			t.Fatal("stale checked_at accepted")
		}
		wire.Publish(gamesignal.PlanetSectorSubject, raw23)
		wire.FlushTimeout(time.Second)
		if n5Presence(t, chatURL, cosmic.URL, c) != 0 {
			t.Fatal("NATS resurrected stale presence")
		}
	}()
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 5 })
	resumed, wr = n5AtPortal(t, f, planetURL, c, uid, wr, resumed, 6)
	offer = readyOffer210(t, resumed)
	broker.Stop()
	offlineOp := op180(t)
	command190(t, resumed, wr, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": offlineOp})
	wr = read190(t, resumed, "transition.complete")["welcome"].(map[string]any)
	if wr["sector"] != float64(6) {
		t.Fatal("NATS outage cancelled successful transition")
	}
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 6 })
	broker.Start()
	n5CoreReady(t, broker)
	wire2 := n5Wire(t, broker)
	events2 := make(chan []byte, 8)
	sub2, e := wire2.Subscribe(gamesignal.PlanetSectorSubject, func(m *nats.Msg) {
		select {
		case events2 <- m.Data:
		default:
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer sub2.Unsubscribe()
	wire2.FlushTimeout(time.Second)
	resumed, wr = n5AtPortal(t, f, planetURL, c, uid, wr, resumed, 7)
	offer = readyOffer210(t, resumed)
	restoredOp := op180(t)
	command190(t, resumed, wr, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": restoredOp})
	wr = read190(t, resumed, "transition.complete")["welcome"].(map[string]any)
	if n5Receive(t, events2, restoredOp).Data.To != 7 {
		t.Fatal("publisher not restored")
	}
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 7 })
	broker.Stop()
	stopPlanet()
	eventuallyN1(t, func() bool { return n5Presence(t, chatURL, cosmic.URL, c) == 0 })
	n2Command(t, fresh, "message.send", map[string]any{"kind": "global", "body": "Global survives Planet and NATS outage"})
	n2Command(t, fresh, "private.create", map[string]any{"kind": "dm", "user": strconv.FormatInt(peerID, 10)})
	if status, _ := n5HTTP(t, chatURL, "history?kind=corporation", cosmic.URL, c); status != 200 {
		t.Fatal("corporation blocked by Planet/NATS outage")
	}
	stopChat()
}

func sceneKeyN5(surface, revision int64, sector int) scene.Key {
	return scene.Key{Surface: surface, Revision: revision, Sector: sector}
}

func n5CoreReady(t *testing.T, b *natsfixture.Broker) {
	t.Helper()
	eventuallyN1(t, func() bool {
		response, e := (&http.Client{Timeout: time.Second}).Get(b.MonitorURL + "/connz")
		if e != nil {
			return false
		}
		defer response.Body.Close()
		var v struct {
			Connections []struct {
				Name string `json:"name"`
			} `json:"connections"`
		}
		if json.NewDecoder(response.Body).Decode(&v) != nil {
			return false
		}
		seen := map[string]bool{}
		for _, c := range v.Connections {
			seen[c.Name] = true
		}
		return seen["cosmos-chat-core"] && seen["cosmos-planet-core"]
	})
}
func n5NoOperation(t *testing.T, events <-chan []byte, operation string) {
	t.Helper()
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case raw := <-events:
			s, ok := gamesignal.DecodePlanet(gamesignal.PlanetSectorSubject, raw)
			if ok && s.SignalID == operation {
				t.Fatal("missing-publisher simulation unexpectedly published")
			}
		case <-timer.C:
			return
		}
	}
}
func n5TransitionWithoutPublish(t *testing.T, f *fixture033, c *http.Cookie, uid int64, target int, operation string) {
	t.Helper()
	var w visits.Welcome
	var key scene.Key
	var fence scenestore.Fence
	if e := f.db.QueryRow(f.ctx, `SELECT visit_id::text,connection_epoch,generation,current_surface,current_sector,revision FROM planet_visits WHERE user_id=$1`, uid).Scan(&w.Visit, &w.Epoch, &w.Generation, &key.Surface, &key.Sector, &key.Revision); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(f.ctx, `SELECT x_q,y_q,position_version FROM planet_characters WHERE user_id=$1`, uid).Scan(&w.X, &w.Y, &w.PositionVersion); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(f.ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); e != nil {
		t.Fatal(e)
	}
	id, e := access.Authenticate(f.ctx, f.db, &http.Request{Header: http.Header{"Cookie": []string{c.String()}}})
	if e != nil {
		t.Fatal(e)
	}
	from, e := scenestore.New(f.db).Load(f.ctx, key, fence)
	if e != nil {
		t.Fatal(e)
	}
	key.Sector = target
	to, e := scenestore.New(f.db).Load(f.ctx, key, fence)
	if e != nil {
		t.Fatal(e)
	}
	offer, _ := access.UUID()
	if _, e = (&visits.Service{DB: f.db}).Transition(f.ctx, id, w, fence, from, to, w.X, w.Y, offer, operation); e != nil {
		t.Fatal("crash-before-publish simulation", e)
	}
}

func n5OldSectorSendDenied(t *testing.T, c *n1Client, surface int64) {
	t.Helper()
	request := op180(t)
	raw, _ := json.Marshal(map[string]any{"type": "message.send", "request_id": request, "payload": map[string]any{"kind": "sector", "target": fmt.Sprintf("%d/1", surface), "body": "Old scope must be rejected"}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := c.conn.Write(ctx, websocket.MessageText, raw); e != nil {
		t.Fatal(e)
	}
	select {
	case ack := <-c.acks:
		if ack["request_id"] != request || ack["error"] != "scope_changed" {
			t.Fatal("old-sector send was not rejected by current SQL scope", ack["error"])
		}
	case <-ctx.Done():
		t.Fatal("old-sector rejection ACK missing")
	}
}
