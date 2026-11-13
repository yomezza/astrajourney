package planetserver

import (
	"context"
	"testing"
	"time"
)

type socketRequestKey struct{}

func TestPlanetWebSocketLifetimeIgnoresHTTPRequestCancellation(t *testing.T) {
	request, cancelRequest := context.WithCancel(context.WithValue(context.Background(), socketRequestKey{}, "request-tag"))
	service, cancelService := context.WithCancel(context.Background())
	defer cancelService()
	conn, closeConn := planetWebSocketContext(request, service)
	defer closeConn()
	cancelRequest()
	if conn.Err() != nil {
		t.Fatal("HTTP request cancellation prematurely closed the upgraded WebSocket")
	}
	if got := conn.Value(socketRequestKey{}); got != "request-tag" {
		t.Fatalf("request metadata lost: %v", got)
	}
	cancelService()
	select {
	case <-conn.Done():
	case <-time.After(time.Second):
		t.Fatal("planet service shutdown did not close WebSocket")
	}
}

func TestPlanetWebSocketLifetimeReleaseCloses(t *testing.T) {
	service, cancelService := context.WithCancel(context.Background())
	defer cancelService()
	ctx, stop := planetWebSocketContext(context.Background(), service)
	stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("WebSocket cleanup did not cancel its context")
	}
}
