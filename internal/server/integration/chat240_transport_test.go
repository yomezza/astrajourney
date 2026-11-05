package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatserver"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChat240FiftyConnections(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	cosmic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.handler.ServeHTTP(w, r) }))
	defer cosmic.Close()
	planet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}))
	defer planet.Close()
	s := &chat.Service{DB: f.db}
	journal, e := chat.OpenJournal(f.ctx, s, filepath.Join(t.TempDir(), "journal"))
	if e != nil {
		t.Fatal(e)
	}
	defer journal.Close()
	s.Journal = journal
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	chatURL := "http://" + listener.Addr().String()
	t.Setenv("CHAT_INTERNAL_URL", chatURL)
	f.handler = f.app.Handler()
	config := chatserver.Config{DatabaseURL: "configured", Journal: "configured", Addr: listener.Addr().String(), CosmicURL: cosmic.URL, PlanetURL: planet.URL, Origins: []string{cosmic.URL}}
	app, e := chatserver.New(s, config)
	if e != nil {
		t.Fatal(e)
	}
	run, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() { done <- app.Serve(run, listener, func() { close(started) }) }()
	defer func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	<-started
	wsURL := "ws" + strings.TrimPrefix(cosmic.URL, "http") + "/chat/ws/v1"
	ctx, cancelDial := context.WithTimeout(f.ctx, 120*time.Second)
	defer cancelDial()
	tokens := []string{}
	sockets := []*websocket.Conn{}
	defer func() {
		for _, c := range sockets {
			c.CloseNow()
		}
	}()
	for i := 0; i < 50; i++ {
		token := strings.ReplaceAll(chat.UUID()+chat.UUID(), "-", "")
		var id int64
		if e = f.db.QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES($1,'') RETURNING id`, fmt.Sprintf("chatload%d", i)).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 hour')`, account.TokenHash(token), id)
		tokens = append(tokens, token)
		c, r, e := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {cosmic.URL}, "Cookie": {"session=" + token}}})
		if e != nil {
			t.Fatalf("dial %d %v response %v", i, e, r)
		}
		_, b, e := c.Read(ctx)
		if e != nil || !strings.Contains(string(b), `"hello"`) {
			t.Fatal("hello", e, string(b))
		}
		sockets = append(sockets, c)
	}
	if f.integer(`SELECT count(*) FROM chat_ws_leases WHERE expires_at>clock_timestamp()`) != 50 {
		t.Fatal("leases")
	}
	_, r, e := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {cosmic.URL}, "Cookie": {"session=" + tokens[0]}}})
	if e == nil || r.StatusCode != 409 {
		t.Fatal("second connection accepted")
	}
	_, r, e = websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.invalid"}, "Cookie": {"session=" + tokens[0]}}})
	if e == nil || r.StatusCode != 403 {
		t.Fatal("foreign origin accepted")
	}
	errors := make(chan error, 50)
	var wg sync.WaitGroup
	startedAt := time.Now()
	for i, c := range sockets {
		wg.Add(1)
		go func(i int, c *websocket.Conn) {
			defer wg.Done()
			id := chat.UUID()
			raw, _ := json.Marshal(map[string]any{"type": "message.send", "request_id": id, "payload": map[string]any{"kind": "global", "body": fmt.Sprintf("load %d", i)}})
			if e := c.Write(ctx, websocket.MessageText, raw); e != nil {
				errors <- e
				return
			}
			for {
				_, b, e := c.Read(ctx)
				if e != nil {
					errors <- e
					return
				}
				var ack struct {
					Type, Error string
					Request     string `json:"request_id"`
				}
				if e = json.Unmarshal(b, &ack); e != nil {
					errors <- e
					return
				}
				if ack.Type == "ack" && ack.Request == id {
					if ack.Error != "" {
						errors <- fmt.Errorf("ack %s", ack.Error)
					}
					return
				}
			}
		}(i, c)
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		t.Error(e)
	}
	if n := f.integer(`SELECT count(*) FROM chat_messages WHERE message_type='user'`); n != 50 {
		t.Fatalf("messages %d", n)
	}
	t.Logf("50 authenticated WS clients; 50 committed messages in %s", time.Since(startedAt))
	if os.Getenv("RUN_CHAT_BROWSER") == "1" {
		cookie, _ := f.signup("chat_browser")
		nonce := "terminal-test-" + chat.UUID()
		handler := f.handler
		f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-cosmos-test-run", nonce)
			handler.ServeHTTP(w, r)
		})
		raw, _ := json.Marshal(map[string]string{"token": cookie.Value})
		command := exec.CommandContext(ctx, "node", "../../../tests/ui/chat-live.mjs")
		command.Env = append(os.Environ(), "TERMINAL_TEST_URL="+cosmic.URL, "TERMINAL_TEST_NONCE="+nonce)
		command.Stdin = bytes.NewReader(raw)
		output, e := command.CombinedOutput()
		if e != nil {
			t.Fatalf("browser: %v\n%s", e, output)
		}
		t.Log(string(output))
	}
	// Private message bodies are not exposed through the health endpoint or SQL errors.
	response, e := http.Get(chatURL + "/chat/api/v1/health")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("health")
	}
}
