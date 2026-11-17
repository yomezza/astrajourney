package integration

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yomezza/cosmosimulator/internal/fleet"
)

type mixedPlayer struct {
	index       int
	uid, shipID int64
	cookie      *http.Cookie
	base        string
	http        *http.Client
	metrics     *mixedMetrics
	rng         *rand.Rand
	planet      bool
	mu          sync.Mutex
	refresh     sync.Mutex
	expedition  map[string]any
	ship        fleet.Ship
	chat        *mixedChat
	ground      *mixedPlanet
	planner     bool
	action      int
	started     time.Time
}

func newMixedPlayer(index int, base string, metrics *mixedMetrics) *mixedPlayer {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(fmt.Sprintf("127.0.1.%d", index+1))}}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	transport.MaxIdleConnsPerHost = 8
	transport.MaxConnsPerHost = 8
	transport.DisableCompression = true
	return &mixedPlayer{index: index, base: base, http: &http.Client{Transport: transport, Timeout: 25 * time.Second}, metrics: metrics, rng: rand.New(rand.NewSource(301000 + int64(index))), planet: index%10 < 3}
}
func (p *mixedPlayer) request(ctx context.Context, method, path string, input any) (json.RawMessage, int, string) {
	var reader io.Reader
	if input != nil {
		raw, _ := json.Marshal(input)
		reader = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	began := time.Now()
	label := strings.SplitN(path, "?", 2)[0]
	if e != nil {
		p.metrics.record("http "+method+" "+label, began, "transport", 0)
		return nil, 0, "request"
	}
	if p.cookie != nil {
		req.AddCookie(p.cookie)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	if method != "GET" {
		req.Header.Set("Origin", p.base)
	}
	response, e := p.http.Do(req)
	if e != nil {
		code := "transport"
		if ctx.Err() != nil {
			code = "cancelled"
		}
		p.metrics.record("http "+method+" "+label, began, code, 0)
		return nil, 0, "transport"
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	size := len(raw)
	if e != nil {
		p.metrics.record("http "+method+" "+label, began, "transport", size)
		return nil, 0, "body"
	}
	if response.Header.Get("Content-Encoding") == "gzip" {
		r, err := gzip.NewReader(bytes.NewReader(raw))
		if err == nil {
			raw, err = io.ReadAll(io.LimitReader(r, 32<<20))
			r.Close()
		}
		if err != nil {
			p.metrics.record("http "+method+" "+label, began, "transport", size)
			return nil, 0, "gzip"
		}
	}
	status := response.StatusCode
	p.metrics.record("http "+method+" "+label, began, strconv.Itoa(status), size)
	if p.cookie == nil {
		for _, cookie := range response.Cookies() {
			if cookie.Name == "session" {
				p.cookie = cookie
			}
		}
	}
	code := ""
	if status >= 400 {
		var v struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &v)
		code = v.Error
		if code == "" {
			code = strconv.Itoa(status)
		}
		p.metrics.count("http_rejection/"+label+"/"+code, 1)
	}
	return raw, status, code
}
func (p *mixedPlayer) get(ctx context.Context, path string) (map[string]any, int) {
	raw, status, _ := p.request(ctx, "GET", path, nil)
	v := map[string]any{}
	_ = json.Unmarshal(raw, &v)
	return v, status
}
func (p *mixedPlayer) command(ctx context.Context, path string, in any) bool {
	_, status, _ := p.request(ctx, "POST", path, in)
	if status == 200 {
		p.snapshot(ctx, true)
	}
	return status == 200
}
func mixedSleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (p *mixedPlayer) poll(ctx context.Context) {
	if !mixedSleep(ctx, time.Duration(p.index%5)*time.Second+time.Duration(p.index*73%997)*time.Millisecond) {
		return
	}
	for ctx.Err() == nil {
		began := time.Now()
		p.snapshot(ctx, false)
		if !mixedSleep(ctx, max(250*time.Millisecond, 5*time.Second-time.Since(began))) {
			return
		}
	}
}

func (p *mixedPlayer) snapshot(ctx context.Context, force bool) {
	if force {
		p.refresh.Lock()
	} else if !p.refresh.TryLock() {
		return
	}
	defer p.refresh.Unlock()
	began := time.Now()
	var workers sync.WaitGroup
	for _, endpoint := range []string{"state", "profile", "expedition", "fleet", "gameplay", "workbench", "notifications"} {
		workers.Add(1)
		go func(endpoint string) {
			defer workers.Done()
			raw, status, _ := p.request(ctx, "GET", "/api/"+endpoint, nil)
			if status != 200 {
				return
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if endpoint == "expedition" {
				_ = json.Unmarshal(raw, &p.expedition)
			}
			if endpoint == "fleet" {
				var v struct {
					Ships []fleet.Ship `json:"ships"`
				}
				if json.Unmarshal(raw, &v) == nil && len(v.Ships) > 0 {
					p.ship = v.Ships[0]
				}
			}
		}(endpoint)
	}
	workers.Wait()
	p.metrics.record("terminal.snapshot", began, "complete", 0)
}

func (p *mixedPlayer) space(ctx context.Context, neighbors map[int][]int) {
	if !mixedSleep(ctx, time.Duration(1+p.index%7)*time.Second) {
		return
	}
	for ctx.Err() == nil {
		if p.metrics.stopActions.Load() {
			return
		}
		p.mu.Lock()
		s := p.ship
		exp := p.expedition
		p.mu.Unlock()
		if s.ID != 0 && s.Destination == nil && s.LocalTarget == nil {
			p.action++
			mode := p.index % 4
			switch mode {
			case 0: // Stay at a real public port; alternate trade and warehouse operations.
				resource := "iron"
				qty := s.Cargo[resource]
				if s.PositionMode == "docked" {
					switch p.action % 4 {
					case 2:
						if qty > 0 {
							p.command(ctx, "/api/workbench", map[string]any{"action": "store", "resource": resource, "quantity": 1})
						}
					case 1:
						p.command(ctx, "/api/expedition", map[string]any{"action": "buy", "resource": resource, "quantity": 1})
					case 0:
						if qty > 0 {
							p.command(ctx, "/api/expedition", map[string]any{"action": "sell", "resource": resource, "quantity": 1})
						}
					case 3:
						p.command(ctx, "/api/workbench", map[string]any{"action": "withdraw", "resource": resource, "quantity": 1})
					}
				}
			case 1: // Mine, carry the ore to a warehouse, and return through real local flight.
				if exp["job"] == nil {
					if s.PositionMode == "docked" {
						if s.Cargo["iron"] > 0 {
							p.command(ctx, "/api/workbench", map[string]any{"action": "store", "resource": "iron", "quantity": s.Cargo["iron"]})
						} else {
							p.command(ctx, "/api/expedition", map[string]any{"action": "move", "target": "asteroids", "mode": "approach"})
						}
					} else if s.ObjectID == "asteroids" {
						if fleet.CargoSize(s.Cargo) >= s.Capacity()-5 {
							p.command(ctx, "/api/expedition", map[string]any{"action": "move", "target": "station", "mode": "docked"})
						} else {
							p.command(ctx, "/api/expedition", map[string]any{"action": "mine", "planet": 101, "resource": "iron", "quantity": 1})
						}
					}
				}
			case 2: // Travel between actual neighbours and scan previously unknown systems.
				var researching bool
				if jobs, ok := exp["research"].([]any); ok {
					researching = len(jobs) > 0
				}
				if !researching {
					if known, _ := exp["surveyed"].(bool); !known {
						p.command(ctx, "/api/expedition", map[string]any{"action": "scan"})
					} else if choices := neighbors[s.Star]; len(choices) > 0 {
						to := choices[p.rng.Intn(len(choices))]
						if s.Fuel < 8 && s.Cargo["agni"] > 0 {
							p.command(ctx, "/api/expedition", map[string]any{"action": "refuel"})
						} else if s.Fuel >= 2 {
							p.get(ctx, fmt.Sprintf("/api/estimate?to=%d", to))
							p.command(ctx, "/api/expedition", map[string]any{"action": "travel", "to": to})
						}
					}
				}
			case 3: // Let the authoritative planner run a real warehouse haul.
				if !p.planner {
					p.planner = p.command(ctx, "/api/planner", map[string]any{"action": "create", "kind": "haul", "ship": s.ID, "source_star": 1, "source_object": "station", "dest_star": 1, "dest_station": "station-2", "resource": "iron", "quantity": 20, "source_kind": "personal", "dest_kind": "personal"})
				}
				p.request(ctx, "GET", "/api/planner", nil)
			}
			if p.action%5 == 0 {
				p.request(ctx, "GET", fmt.Sprintf("/api/system?id=%d", s.Star), nil)
				p.request(ctx, "GET", "/api/market/global", nil)
			}
		}
		if !mixedSleep(ctx, time.Duration(4+p.rng.Intn(5))*time.Second) {
			return
		}
	}
}

func mixedPath(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}
