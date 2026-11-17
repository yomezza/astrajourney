package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/planet/building"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
)

type mixedChat struct {
	p         *mixedPlayer
	conn      *websocket.Conn
	writes    sync.Mutex
	mu        sync.Mutex
	pending   map[string]time.Time
	refresh   chan struct{}
	done      chan struct{}
	expected  atomic.Bool
	refreshes atomic.Int64
	connected atomic.Bool
}

func (p *mixedPlayer) chatKind() string {
	if p.planet && p.index%3 == 0 {
		return "sector"
	}
	if p.planet && p.index%3 == 1 {
		return "planet"
	}
	if p.index%3 == 2 {
		return "corporation"
	}
	return "global"
}
func mixedDialChat(ctx context.Context, p *mixedPlayer) (*mixedChat, error) {
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(check, "ws"+strings.TrimPrefix(p.base, "http")+"/chat/ws/v1", &websocket.DialOptions{HTTPClient: p.http, HTTPHeader: http.Header{"Cookie": {p.cookie.String()}, "Origin": {p.base}}})
	if err != nil {
		return nil, fmt.Errorf("chat dial unavailable")
	}
	c := &mixedChat{p: p, conn: conn, pending: map[string]time.Time{}, refresh: make(chan struct{}, 1), done: make(chan struct{})}
	conn.SetReadLimit(1 << 20)
	c.connected.Store(true)
	go func() {
		defer close(c.done)
		defer c.connected.Store(false)
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				if !c.expected.Load() && ctx.Err() == nil {
					p.metrics.count("chat.unexpected_disconnect", 1)
				}
				return
			}
			p.metrics.count("chat.ws_bytes", int64(len(raw)))
			var msg struct {
				Type    string `json:"type"`
				Request string `json:"request_id"`
				Error   string `json:"error"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				p.metrics.count("chat.invalid_json", 1)
				continue
			}
			switch msg.Type {
			case "sync":
				p.metrics.count("chat.sync", 1)
				select {
				case c.refresh <- struct{}{}:
				default:
					p.metrics.count("chat.client_coalesced", 1)
				}
			case "ack":
				c.mu.Lock()
				began, ok := c.pending[msg.Request]
				delete(c.pending, msg.Request)
				c.mu.Unlock()
				if ok {
					code := msg.Error
					if code == "" {
						code = "ok"
					}
					p.metrics.record("chat.message_ack", began, code, len(raw))
					p.metrics.ackMessage(msg.Request, msg.Error == "")
				}
			}
		}
	}()
	return c, nil
}
func (c *mixedChat) send(ctx context.Context, run string, n int) {
	id := chat.UUID()
	kind := c.p.chatKind()
	body := fmt.Sprintf("m100:%s:%s Пилот %d: сообщение %d", run, id, c.p.index, n)
	c.mu.Lock()
	c.pending[id] = time.Now()
	c.mu.Unlock()
	c.p.metrics.sentMessage(id, kind)
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c.writes.Lock()
	err := wsjson.Write(check, c.conn, map[string]any{"type": "message.send", "request_id": id, "payload": map[string]any{"kind": kind, "body": body}})
	c.writes.Unlock()
	if err != nil {
		c.p.metrics.count("chat.write_error", 1)
	}
}
func (c *mixedChat) run(ctx context.Context, run string) {
	rng := rand.New(rand.NewSource(410000 + int64(c.p.index)))
	writeTimer := time.NewTimer(time.Duration(1+rng.Intn(20)) * time.Second)
	defer writeTimer.Stop()
	periodic := time.NewTicker(30 * time.Second)
	defer periodic.Stop()
	var debounce *time.Timer
	var wake <-chan time.Time
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
		c.expected.Store(true)
		c.conn.CloseNow()
		<-c.done
	}()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-c.refresh:
			if debounce == nil {
				debounce = time.NewTimer(150 * time.Millisecond)
			} else {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(150 * time.Millisecond)
			}
			wake = debounce.C
		case <-wake:
			wake = nil
			c.refreshView(ctx, run)
		case <-periodic.C:
			c.mu.Lock()
			for id, began := range c.pending {
				if time.Since(began) > 20*time.Second {
					c.p.metrics.record("chat.message_ack", began, "timeout", 0)
					delete(c.pending, id)
				}
			}
			c.mu.Unlock()
		case <-writeTimer.C:
			if c.p.index%5 != 4 && !c.p.metrics.stopActions.Load() {
				n++
				c.send(ctx, run, n)
			}
			writeTimer.Reset(time.Duration(15+rng.Intn(11)) * time.Second)
		}
	}
}
func (c *mixedChat) refreshView(ctx context.Context, run string) {
	if ctx.Err() != nil {
		return
	}
	c.refreshes.Add(1)
	c.p.request(ctx, "GET", "/chat/api/v1/bootstrap", nil)
	c.p.request(ctx, "GET", "/chat/api/v1/notifications", nil)
	raw, status, _ := c.p.request(ctx, "GET", mixedPath("/chat/api/v1/history", url.Values{"kind": {c.p.chatKind()}}), nil)
	if status != 200 {
		return
	}
	var rows []struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	prefix := "m100:" + run + ":"
	for _, row := range rows {
		if strings.HasPrefix(row.Body, prefix) {
			rest := strings.TrimPrefix(row.Body, prefix)
			id, _, _ := strings.Cut(rest, " ")
			c.p.metrics.seeMessage(id)
		}
	}
}

type mixedScene struct {
	scene.Public
	Building struct {
		Cells    []byte            `json:"cells"`
		Terrain  []byte            `json:"terrain_cells"`
		Snapshot building.Snapshot `json:"snapshot"`
	} `json:"building"`
	Exits []struct {
		Neighbor int `json:"neighbor"`
		X        int `json:"x_q"`
		Y        int `json:"y_q"`
	} `json:"exits"`
}
type mixedPlanet struct {
	p            *mixedPlayer
	v            *loadClient230
	scene        mixedScene
	goals        map[int64]time.Time
	goalX, goalY int
	goalUntil    time.Time
	nextPortal   time.Time
	nextBuild    time.Time
	buildID      string
	buildAction  string
	buildBegan   time.Time
	connected    atomic.Bool
}

func mixedLand(ctx context.Context, p *mixedPlayer, site int64) (*mixedPlanet, error) {
	began := time.Now()
	v, status := p.get(ctx, "/api/planet/visit")
	_ = v
	_ = status
	raw, status, code := p.request(ctx, "POST", "/api/planet/handoff", map[string]any{"operation": chat.UUID(), "landing_site": fmt.Sprint(site), "ship": fmt.Sprint(p.shipID)})
	if status != 200 {
		return nil, fmt.Errorf("handoff HTTP %d %s", status, code)
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if json.Unmarshal(raw, &ticket) != nil || ticket.Ticket == "" {
		return nil, fmt.Errorf("ticket missing")
	}
	client, err := dialLoad230(p.base, p.cookie, p.uid, p.shipID, map[string]any{"ticket": ticket.Ticket}, &telemetry.Samples{})
	if err != nil {
		return nil, err
	}
	ground := &mixedPlanet{p: p, v: client, goals: map[int64]time.Time{}, nextPortal: time.Now().Add(time.Duration(20+p.index%8) * time.Second), nextBuild: time.Now().Add(time.Duration(3+p.index%10) * time.Second)}
	ground.connected.Store(true)
	if err = ground.loadScene(ctx); err != nil {
		client.close()
		return nil, err
	}
	p.metrics.record("planet.landing_ready", began, "ok", 0)
	return ground, nil
}
func (g *mixedPlanet) loadScene(ctx context.Context) error {
	w := g.v.welcome()
	raw, status, _ := g.p.request(ctx, "GET", fmt.Sprintf("/planet/api/v1/scene?visit=%s&epoch=%d", w.Visit, w.Epoch), nil)
	if status != 200 {
		return fmt.Errorf("scene HTTP %d", status)
	}
	return json.Unmarshal(raw, &g.scene)
}
func (g *mixedPlanet) move(x, y int) {
	if err := g.v.send("move", map[string]int{"x_q": x, "y_q": y}); err != nil {
		g.p.metrics.count("planet.write_error", 1)
	} else {
		g.goals[g.v.seq] = time.Now()
		g.p.metrics.count("planet.move_sent", 1)
	}
}
func (g *mixedPlanet) run(ctx context.Context) {
	rng := rand.New(rand.NewSource(510000 + int64(g.p.index)))
	tick := time.NewTicker(4 * time.Second)
	defer tick.Stop()
	defer func() { g.connected.Store(false); g.v.close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-g.v.done:
			g.connected.Store(false)
			g.p.metrics.count("planet.unexpected_disconnect", 1)
			return
		case event := <-g.v.events:
			switch event.Type {
			case "path.accepted":
				var state struct {
					Seq int64 `json:"command_seq"`
				}
				_ = json.Unmarshal(event.Payload, &state)
				if began, ok := g.goals[state.Seq]; ok {
					g.p.metrics.record("planet.move_ack", began, "ok", 0)
					delete(g.goals, state.Seq)
				}
			case "error":
				var v struct {
					Code string `json:"code"`
					Seq  int64  `json:"seq"`
				}
				_ = json.Unmarshal(event.Payload, &v)
				g.p.metrics.count("planet.error/"+v.Code, 1)
				if began, ok := g.goals[v.Seq]; ok {
					g.p.metrics.record("planet.move_ack", began, v.Code, 0)
					delete(g.goals, v.Seq)
				}
				g.goalUntil = time.Time{}
			case "transition.offer":
				var offer struct {
					ID    string `json:"offer"`
					Ready bool   `json:"ready"`
				}
				_ = json.Unmarshal(event.Payload, &offer)
				if offer.Ready {
					g.v.send("transition.confirm", map[string]any{"offer": offer.ID, "operation": chat.UUID()})
				}
			case "transition.complete":
				g.goalUntil = time.Time{}
				g.nextPortal = time.Now().Add(45 * time.Second)
				g.p.metrics.count("planet.transition_complete", 1)
				if err := g.loadScene(ctx); err != nil {
					g.p.metrics.count("planet.scene_reload_error", 1)
				}
			case "build.result":
				var reply struct {
					OK   bool   `json:"ok"`
					Code string `json:"code"`
				}
				_ = json.Unmarshal(event.Payload, &reply)
				code := reply.Code
				if reply.OK {
					code = "ok"
				}
				g.p.metrics.record("planet.build_"+g.buildAction, g.buildBegan, code, 0)
				if !reply.OK || g.buildAction == "remove" {
					g.buildID = ""
				}
				g.nextBuild = time.Now().Add(25 * time.Second)
			}
		case <-tick.C:
			if g.p.metrics.stopActions.Load() {
				continue
			}
			g.v.mu.Lock()
			state := g.v.state
			g.v.mu.Unlock()
			if !g.goalUntil.IsZero() && time.Now().Before(g.goalUntil) {
				if math.Hypot(float64(state.X-g.goalX), float64(state.Y-g.goalY)) < 2*1024 {
					continue
				}
				if state.Moving {
					continue
				}
				g.goalUntil = time.Time{}
			}
			if !state.Moving && time.Now().After(g.nextBuild) {
				g.build(state.X, state.Y, rng)
				g.nextBuild = time.Now().Add(25 * time.Second)
				continue
			}
			if !state.Moving && time.Now().After(g.nextPortal) && len(g.scene.Exits) > 0 {
				exit := g.scene.Exits[rng.Intn(len(g.scene.Exits))]
				g.goalX, g.goalY = exit.X, exit.Y
				g.goalUntil = time.Now().Add(110 * time.Second)
				g.nextPortal = time.Now().Add(120 * time.Second)
				g.move(exit.X, exit.Y)
				continue
			}
			if state.Moving {
				continue
			}
			for range 24 {
				x := state.X/1024 - 14 + rng.Intn(29)
				y := state.Y/1024 - 14 + rng.Intn(29)
				if x < 1 || y < 1 || x >= 255 || y >= 255 {
					continue
				}
				index := y*256 + x
				if len(g.scene.Building.Cells) == 256*256 && g.scene.Building.Cells[index] != 0 {
					g.move(x*1024+512, y*1024+512)
					break
				}
			}
		}
	}
}
func (g *mixedPlanet) build(x, y int, rng *rand.Rand) {
	if g.buildID != "" {
		g.buildAction = "remove"
		g.buildBegan = time.Now()
		g.v.send("build", building.Command{Action: "remove", Object: building.Object{ID: g.buildID}})
		return
	}
	for range 32 {
		bx := x/1024/4 - 5 + rng.Intn(11)
		by := y/1024/4 - 5 + rng.Intn(11)
		if bx < 1 || by < 1 || bx >= 63 || by >= 63 {
			continue
		}
		clear := true
		for yy := by*4 - 1; yy < (by+1)*4+1; yy++ {
			for xx := bx*4 - 1; xx < (bx+1)*4+1; xx++ {
				if len(g.scene.Building.Cells) != 256*256 || g.scene.Building.Cells[yy*256+xx] == 0 {
					clear = false
				}
			}
		}
		if !clear {
			continue
		}
		g.buildID = chat.UUID()
		g.buildAction = "place"
		g.buildBegan = time.Now()
		g.v.send("build", building.Command{Action: "place", Object: building.Object{ID: g.buildID, Kind: "floor", X: bx, Y: by, Rotation: rng.Intn(4)}})
		return
	}
	g.p.metrics.count("planet.no_local_build_site", 1)
}
