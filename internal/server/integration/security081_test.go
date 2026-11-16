package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/logistics"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func Test081NoRoundTripProfitOrReputation(t *testing.T) {
	f := newFixture033(t)
	c, uid := f.signup("secure_trader")
	f.exec(`UPDATE stations SET founder=(SELECT id FROM companies WHERE founder_key='founder-01') WHERE star=1 AND object_id='station'`)
	// Buy/store legitimate initial goods until the former pricing boundary.
	stock := f.integer(`SELECT (stock->>'iron')::int FROM local_markets WHERE star=1 AND station='station'`)
	for stock > 10 {
		q := min(stock-10, 50)
		f.req(c, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "iron", "quantity": q}, 200)
		f.cmd(c, map[string]any{"action": "store", "resource": "iron", "quantity": q}, 200)
		stock -= q
	}
	before := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	wallet := f.integer(`SELECT credits FROM local_markets WHERE star=1 AND station='station'`)
	for range 10 {
		f.req(c, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "iron", "quantity": 1}, 200)
		f.req(c, "POST", "/api/expedition", map[string]any{"action": "sell", "resource": "iron", "quantity": 1}, 200)
	}
	after := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	if after != before-20 || wallet+20 != f.integer(`SELECT credits FROM local_markets WHERE star=1 AND station='station'`) {
		t.Fatal("reversible trades created profit or lost currency")
	}
	if f.integer(`SELECT COALESCE(sum(points),0) FROM reputation WHERE user_id=$1`, uid) != 0 {
		t.Fatal("wash trades farmed reputation")
	}
	if f.integer(`SELECT (stock->>'iron')::int FROM local_markets WHERE star=1 AND station='station'`) != 10 {
		t.Fatal("round trip changed final stock")
	}
	// Negative and oversized quantities cannot change balances or resources.
	for _, q := range []int{-1, 0, 1000000} {
		f.req(c, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "iron", "quantity": q}, 409)
	}
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != after {
		t.Fatal("invalid quantity changed wallet")
	}
}

func Test081PublicDiscoveryAndIndependentWorlds(t *testing.T) {
	f := newFixture033(t)
	c, uid := f.signup("secure_scout")
	for _, path := range []string{"/api/galaxy", "/api/expedition", "/api/system?id=2"} {
		w := f.req(c, "GET", path, nil, 200)
		for _, key := range []string{`"seed":`, `"discovery_secret":`, `"discovery_seed":`, `"resource_rolls":`, `"resource_weights":`} {
			if bytes.Contains(w.Body.Bytes(), []byte(key)) {
				t.Fatalf("%s disclosed %s", path, key)
			}
		}
	}
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.DiscoverySecret) != 64 {
		t.Fatal("missing persistent secret")
	}
	different := 0
	for _, s := range g.Stars[1:100] {
		legacy := s
		legacy.DiscoverySeed = ""
		legacy.ResourceRolls = nil
		if !reflect.DeepEqual(universe.VariedStarResources(s, 101, "Пояс астероидов"), universe.VariedStarResources(legacy, 101, "Пояс астероидов")) {
			different++
		}
	}
	if different < 40 {
		t.Fatal("public ID still predicts hidden resources", different)
	}
	one := g.Stars[49]
	two := one
	one.DiscoverySeed = ""
	two.DiscoverySeed = ""
	universe.SealStar(&one, strings.Repeat("a", 64), false)
	universe.SealStar(&two, strings.Repeat("b", 64), false)
	if reflect.DeepEqual(one.Planets, two.Planets) {
		t.Fatal("identical public map seeds disclose private contents")
	}
	// Surveying still legitimately opens all objects, without leaking generator state.
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,2)`, uid)
	w := f.req(c, "GET", "/api/system?id=2", nil, 200)
	var system struct {
		Sites []universe.Site `json:"sites"`
	}
	json.Unmarshal(w.Body.Bytes(), &system)
	if len(system.Sites) == 0 || bytes.Contains(w.Body.Bytes(), []byte(g.Stars[1].DiscoverySeed)) {
		t.Fatal("survey did not safely reveal objects")
	}
}

func Test081DiscoveryUpgradePreservesObservedObjects(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("secure_migration")
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g)
	var legacy universe.Galaxy
	json.Unmarshal(raw, &legacy)
	legacy.DiscoverySecret = ""
	for i := range legacy.Stars {
		legacy.Stars[i].DiscoverySeed = ""
		legacy.Stars[i].ResourceRolls = nil
	}
	// Recreate legacy per-object resource memberships before migration.
	for i := range legacy.Stars {
		s := &legacy.Stars[i]
		for j := range s.Planets {
			s.Planets[j].Resources = universe.VariedStarResources(*s, s.Planets[j].ID, s.Planets[j].Kind)
		}
	}
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,42)`, uid)
	before := universe.SystemSites(legacy.Stars[41])
	unknown := legacy.Stars[42].Planets
	raw, _ = json.Marshal(legacy)
	f.stageLegacy(raw)
	var schema string
	f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema)
	db, err := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	raw = f.worldJSON()
	var secured universe.Galaxy
	json.Unmarshal(raw, &secured)
	if !reflect.DeepEqual(before, universe.SystemSites(secured.Stars[41])) {
		t.Fatal("observed bodies or deposits changed")
	}
	if reflect.DeepEqual(unknown, secured.Stars[42].Planets) {
		t.Fatal("never-observed planet generation remains public")
	}
	first := append([]byte(nil), raw...)
	db, err = openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	raw = f.worldJSON()
	if !bytes.Equal(first, raw) {
		t.Fatal("restart rerandomized private world")
	}
	// Existing protected deposits still obey future administrator weight changes.
	s := secured.Stars[41]
	s.ResourceWeights = map[string]int{}
	for i := range s.Planets {
		s.Planets[i].Resources = universe.VariedStarResources(s, s.Planets[i].ID, s.Planets[i].Kind)
	}
	for _, site := range universe.SystemSites(s) {
		if len(site.Resources) > 0 {
			t.Fatal("zero weights ignored for preserved deposits")
		}
	}
}

type securitySlowBody struct {
	entered, release chan struct{}
	once             sync.Once
}

func (b *securitySlowBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}

func (b *securitySlowBody) Close() error { return nil }

func Test081SlowBodyDoesNotLockGame(t *testing.T) {
	f := newFixture033(t)
	one, _ := f.signup("slow_body")
	two, _ := f.signup("other_pilot")
	body := &securitySlowBody{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(body.release) })
	r := httptest.NewRequest("POST", "/api/expedition", nil)
	r.Body = body
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(one)
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() { defer close(done); f.handler.ServeHTTP(w, r) }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("body was not read")
	}
	other := httptest.NewRequest("POST", "/api/expedition", strings.NewReader(`{"action":"refuel"}`))
	other.Header.Set("Content-Type", "application/json")
	other.AddCookie(two)
	complete := make(chan struct{})
	reply := httptest.NewRecorder()
	go func() { defer close(complete); f.handler.ServeHTTP(reply, other) }()
	select {
	case <-complete:
		if reply.Code != 409 {
			t.Fatal(reply.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow body blocks another pilot")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	if err := f.app.Simulation.Tick(ctx); err != nil {
		t.Fatal("slow body blocked simulation", err)
	}
	release.Do(func() { close(body.release) })
	<-done
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func Test081ProxyIdentityAndRequestValidation(t *testing.T) {
	trusted, err := httpkit.ParseTrustedProxies("127.0.0.1/32, ::1/128")
	if err != nil {
		t.Fatal(err)
	}
	a := server.New(nil, server.Config{TrustedProxies: trusted})
	req := func(peer, chain, path string) *http.Request {
		r := httptest.NewRequest("POST", path, nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", chain)
		return r
	}
	for i := 0; i < 20; i++ {
		if !a.Auth.Allow(req("127.0.0.1:18000", "198.51.100.10", "/api/login")) {
			t.Fatal("early limit")
		}
	}
	if a.Auth.Allow(req("127.0.0.1:18001", "198.51.100.10", "/api/login")) {
		t.Fatal("limit bypass")
	}
	if !a.Auth.Allow(req("127.0.0.1:18002", "203.0.113.22", "/api/login")) {
		t.Fatal("one proxied player blocked another")
	}
	if !a.Auth.Allow(req("127.0.0.1:18002", "198.51.100.10", "/api/register")) {
		t.Fatal("login blocked registration")
	}
	if actual := a.Auth.ClientAddress(req("203.0.113.22:5000", "1.2.3.4", "/api/login")); actual != "203.0.113.22" {
		t.Fatal("untrusted header accepted", actual)
	}
	if actual := a.Auth.ClientAddress(req("127.0.0.1:5000", "1.2.3.4, 198.51.100.10", "/api/login")); actual != "198.51.100.10" {
		t.Fatal("spoofed leftmost header accepted", actual)
	}
	if actual := a.Auth.ClientAddress(req("[::1]:5000", "2001:db8::11", "/api/login")); actual != "2001:db8::11" {
		t.Fatal("IPv6 client identity", actual)
	}
	if _, err := httpkit.ParseTrustedProxies("not-a-network"); err == nil {
		t.Fatal("invalid trust configuration accepted")
	}
	for _, body := range []string{"{", `{"username":"a"}` + strings.Repeat(" ", 4100)} {
		r := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.New(nil, server.Config{}).Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("malformed/oversized request reached database", w.Code)
		}
	}
}

func Test081ForgedCommandsAndAdminPermissions(t *testing.T) {
	f := newFixture033(t)
	one, uid := f.signup("forgery_one")
	_, other := f.signup("forgery_two")
	credits := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	for _, path := range []string{"/api/admin/tick", "/api/admin/user", "/api/admin/resources", "/api/admin/teleport", "/api/admin/gameplay", "/api/admin/galaxies/preview", "/api/admin/galaxies/apply", "/api/admin/galaxies/delete", "/api/admin/galaxies/delete-preview", "/api/admin/benchmark"} {
		f.req(one, "POST", path, map[string]any{}, 403)
	}
	f.req(one, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "iron", "quantity": 1, "credits": 500000000000}, 400)
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1`, other)
	f.cmd(one, map[string]any{"action": "select_ship", "ship": sid}, 404)
	f.req(one, "POST", "/api/logistics", map[string]any{"request": "forged-081-00000001", "mode": "direct", "from": logistics.CargoLocation{Kind: "ship", Ship: sid}, "to": logistics.CargoLocation{Kind: "personal", Star: 1, Station: "station"}, "resource": "iron", "quantity": 1}, 409)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != credits || f.integer(`SELECT count(*) FROM admin_log WHERE actor=$1`, uid) != 0 {
		t.Fatal("forged command changed finances or administrator state")
	}
}
