package webserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
)

func TestGatewayRoutesAndUntrustedHeaders(t *testing.T) {
	var maps atomic.Int64
	upstream := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/cdn/api/v1/asset-map" {
				maps.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"schema_version":1,"client_contract":"astra-assets-v1","urls":{}}`))
				return
			}
			if r.Header.Get(mediastore.Header) != "" || strings.Contains(r.Header.Get("X-Forwarded-For"), "evil") {
				t.Error("browser forged internal/forwarding headers")
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("X-Upstream", id)
			w.Header().Set("X-Origin", r.Header.Get("Origin"))
			w.Header().Set("X-Cookie", r.Header.Get("Cookie"))
			w.WriteHeader(200)
		}))
	}
	cosmos, cdn, planet, chat := upstream("cosmos"), upstream("cdn"), upstream("planet"), upstream("chat")
	defer cosmos.Close()
	defer cdn.Close()
	defer planet.Close()
	defer chat.Close()
	s, e := New(Config{StorageRoot: t.TempDir(), CosmicURL: cosmos.URL, CDNURL: cdn.URL, PlanetURL: planet.URL, ChatURL: chat.URL})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := s.Handler()
	for p, target := range map[string]string{"/api/profile": "cosmos", "/api/guide/media/1": "cdn", "/assets/sha256/test/foo.png": "cdn", "/profile.svg": "cdn", "/planet/api/v1/scene": "planet", "/chat/api/v1/channels": "chat"} {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("Origin", "https://game.example")
		r.Header.Set("Cookie", "session=test")
		r.Header.Set(mediastore.Header, "evil")
		r.Header.Set("X-Forwarded-For", "evil")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if len(w.Header().Values("Cache-Control")) != 1 || strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("gateway overrode upstream cache policy", w.Header().Values("Cache-Control"))
		}
		if w.Header().Get("X-Upstream") != target || w.Header().Get("X-Origin") != "https://game.example" || w.Header().Get("X-Cookie") != "session=test" {
			t.Fatalf("routing/auth headers for %s: %v", p, w.Header())
		}
	}
	for _, p := range []string{"/internal/cdn/v1/media/1/authorize", "/cdn/internal/v1/media/1", "/planet/api/v1/metrics", "/cdn/api/v1/metrics"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("internal route exposed: %s %d", p, w.Code)
		}
	}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/cdn/api/v1/asset-map", nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if maps.Load() != 1 {
		t.Fatal("public map cache missed")
	}
	s.generation.Add(1)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/cdn/api/v1/asset-map", nil))
	if maps.Load() != 2 {
		t.Fatal("hint did not invalidate cache")
	}
}
func TestWebSocketGatewayPreservesOriginAndSession(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=test" {
			t.Error("missing session cookie")
		}
		c, e := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"game.example"}})
		if e != nil {
			return
		}
		defer c.CloseNow()
		kind, b, e := c.Read(r.Context())
		if e == nil {
			c.Write(r.Context(), kind, b)
		}
	}))
	defer downstream.Close()
	s, e := New(Config{StorageRoot: t.TempDir(), CosmicURL: downstream.URL, CDNURL: downstream.URL, PlanetURL: downstream.URL, ChatURL: downstream.URL})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	gateway := httptest.NewServer(s.Handler())
	defer gateway.Close()
	for _, p := range []string{"/planet/ws/v1", "/chat/ws/v1"} {
		c, _, e := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(gateway.URL, "http")+p, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://game.example"}, "Cookie": []string{"session=test"}}})
		if e != nil {
			t.Fatal(e)
		}
		if e = c.Write(context.Background(), websocket.MessageText, []byte("ping")); e != nil {
			t.Fatal(e)
		}
		_, b, e := c.Read(context.Background())
		c.CloseNow()
		if e != nil || string(b) != "ping" {
			t.Fatal(string(b), e)
		}
	}
}
func TestMissingBundleIsLiveButNotReady(t *testing.T) {
	s, e := New(Config{StorageRoot: t.TempDir(), CosmicURL: "http://127.0.0.1:1", CDNURL: "http://127.0.0.1:1", PlanetURL: "http://127.0.0.1:1", ChatURL: "http://127.0.0.1:1"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for p, status := range map[string]int{"/web/api/v1/health/live": 200, "/web/api/v1/health/ready": 503, "/": 503} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != status {
			t.Fatal(p, w.Code)
		}
		if strings.Contains(p, "health") {
			var body map[string]any
			if json.Unmarshal(w.Body.Bytes(), &body) != nil {
				t.Fatal("invalid health")
			}
		}
	}
}
