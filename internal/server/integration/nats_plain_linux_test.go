package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func TestPlainNATSNativeSharedConfig(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f, c, uid, _, site, ship := prepare180(t)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	b := natsfixture.New(t, natsfixture.Options{N6: true, Plain: true})
	_, role, _ := n1RuntimePool(t, f)
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("role", role)
	u.RawQuery = q.Encode()
	cosmosAddr, planetAddr, chatAddr := n5Port(t), n5Port(t), n5Port(t)
	cosmos, planet, chat := "http://"+cosmosAddr, "http://"+planetAddr, "http://"+chatAddr
	profile := func(name, password string) map[string]any {
		return map[string]any{"username": name, "password": password}
	}
	doc := map[string]any{
		"database": map[string]any{"url": dsn, "schema": schema},
		"cosmos":   map[string]any{"http_addr": cosmosAddr, "public_origin": cosmos, "planet_enabled": true, "chat_enabled": true, "planet_internal_url": planet, "chat_internal_url": chat},
		"planet":   map[string]any{"http_addr": planetAddr, "allowed_origins": []string{cosmos, "http://127.0.0.1:8080"}, "db_timeout": "200ms", "lease_ttl": "3s", "lease_renew": "1s", "shutdown_timeout": "2s"},
		"chat":     map[string]any{"database_url": u.String(), "http_addr": chatAddr, "cosmic_internal_url": cosmos, "planet_internal_url": planet, "allowed_origins": []string{cosmos}, "erasure_journal": filepath.Join(t.TempDir(), "erasure.log"), "dispatcher_mode": "b1", "fallback_poll_interval": "10s", "degraded_poll_interval": "500ms"},
		"nats":     map[string]any{"enabled": true, "servers": []string{b.URL}, "connect_timeout": "500ms", "reconnect_wait": "250ms", "cosmos": profile("cosmos", b.CosmosPassword), "planet": profile("planet", b.PlanetPassword), "chat": profile("chat", b.Password)},
	}
	config := n5Config(t, doc)
	n5Process(t, n5Build(t, "./cmd/astra-journey", "cosmos", false), config, cosmos+"/api/health")
	n5Process(t, n5Build(t, "./cmd/astra-journey-planet", "planet", false), config, planet+"/planet/api/v1/health/ready")
	n5Process(t, n5Build(t, "./cmd/astra-journey-chat", "chat", false), config, chat+"/chat/api/v1/health")
	ready := func() {
		eventuallyN1(t, func() bool {
			res, e := (&http.Client{Timeout: time.Second}).Get(b.MonitorURL + "/connz")
			if e != nil {
				return false
			}
			defer res.Body.Close()
			var v struct {
				Connections []struct {
					Name string `json:"name"`
				} `json:"connections"`
			}
			if json.NewDecoder(res.Body).Decode(&v) != nil {
				return false
			}
			seen := map[string]bool{}
			for _, connection := range v.Connections {
				seen[connection.Name] = true
			}
			return seen["cosmos-cosmos-core"] && seen["cosmos-planet-core"] && seen["cosmos-chat-core"]
		})
	}
	ready()
	checkStatus := func(connected bool) bool {
		response := n6API(t, cosmos, "/api/admin/services", c, nil, 200)
		cards := response["services"].([]any)
		if len(cards) != 3 {
			return false
		}
		for _, entry := range cards {
			state := entry.(map[string]any)["nats"].(map[string]any)
			if state["connected"] != connected {
				return false
			}
			if connected && state["state"] != "connected" {
				return false
			}
		}
		return true
	}
	eventuallyN1(t, func() bool { return checkStatus(true) })
	wire := n5Wire(t, b)
	events := make(chan gamesignal.Message, 16)
	watch := func(nc *nats.Conn) {
		_, e := nc.Subscribe("cosmos.v1.signal.>", func(m *nats.Msg) {
			select {
			case events <- gamesignal.Message{Subject: m.Subject, Data: m.Data}:
			default:
			}
		})
		if e != nil {
			t.Fatal(e)
		}
		if e = nc.FlushTimeout(time.Second); e != nil {
			t.Fatal(e)
		}
	}
	watch(wire)
	pilot := n1Dial(t, chat, cosmos, c.Value)
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > 0 })
	before := pilot.syncs.Load()
	n6API(t, cosmos, "/api/companies", c, map[string]any{"action": "leave"}, 200)
	n6Event(t, events, gamesignal.MembershipSubject)
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > before })
	ticket := n6API(t, cosmos, "/api/planet/handoff", c, map[string]any{"operation": op180(t), "landing_site": fmt.Sprint(site), "ship": fmt.Sprint(ship)}, 200)
	conn, _ := ws180(t, planet, c, map[string]any{"ticket": ticket["ticket"]})
	n6Event(t, events, gamesignal.VisitStartedSubject)
	if n5Presence(t, chat, cosmos, c) != 1 {
		t.Fatal("plain transport spatial scope")
	}
	conn.CloseNow()
	n6Event(t, events, gamesignal.VisitDisconnectedSubject)
	b.Stop()
	eventuallyN1(t, func() bool { return checkStatus(false) })
	starter := f.integer(`SELECT id FROM companies WHERE starter LIMIT 1`)
	n6API(t, cosmos, "/api/companies", c, map[string]any{"action": "join", "id": starter}, 200)
	ack := n6Command(t, pilot, "favorite.note", map[string]any{"body": "plain NATS outage"})
	if ack["error"] != nil {
		t.Fatal("outage changed SQL operation", ack)
	}
	if f.integer(`SELECT company_id FROM users WHERE id=$1`, uid) != starter {
		t.Fatal("commit lost while broker offline")
	}
	b.Start()
	ready()
	wire = n5Wire(t, b)
	watch(wire)
	n6API(t, cosmos, "/api/companies", c, map[string]any{"action": "leave"}, 200)
	n6Event(t, events, gamesignal.MembershipSubject)
	t.Log("three native -race services, one TOML, plaintext authenticated NATS, Cosmic/Planet hints and broker restart PASS")
}
