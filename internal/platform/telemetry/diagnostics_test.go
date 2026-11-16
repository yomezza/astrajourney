package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDiagnosticRedactionAndHTTPPropagation(t *testing.T) {
	dir := t.TempDir()
	stop, err := Start("planet", dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	const secret = "must-never-appear-password-token"
	const id = "156a0243-8629-4908-912b-4795ae47497a"
	upstream := TraceHTTP("planet", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") != id {
			t.Fatal("gateway lost request ID")
		}
		ctx := WithVisit(Phase(r.Context(), "checkpoint"), 7, "test-visit", 89)
		Event(ctx, "db failed", &pgconn.PgError{Code: "55P03", Message: secret, Detail: secret, Where: secret})
		Event(ctx, "connection failed", errors.New("postgres://user:"+secret+"@localhost/db"))
		w.WriteHeader(204)
	}))
	gateway := TraceHTTP("cosmos", upstream)
	r := httptest.NewRequest("GET", "/planet/api/v1/scene?request_id="+id+"&ticket="+secret, nil)
	r.Header.Set("Cookie", "session="+secret)
	w := httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("X-Request-ID") != id {
		t.Fatal("request/response changed")
	}
	stop()
	data, err := os.ReadFile(filepath.Join(dir, "planet.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, secret) {
		t.Fatal("diagnostics leaked sensitive data")
	}
	for _, want := range []string{id, `"visit_id":"test-visit"`, `"connection_epoch":89`, `"sqlstate":"55P03"`, `"phase":"checkpoint"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestDiagnosticRejectsArbitraryRequestID(t *testing.T) {
	r := httptest.NewRequest("GET", "/planet/ws/v1?request_id=password", nil)
	r.Header.Set("X-Request-ID", "untrusted-password")
	w := httptest.NewRecorder()
	TraceHTTP("planet", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") == "untrusted-password" {
			t.Fatal("unvalidated request ID")
		}
		if len(Fields(r.Context())) == 0 {
			t.Fatal("missing context")
		}
	})).ServeHTTP(w, r)
	if len(w.Header().Get("X-Request-ID")) != 36 {
		t.Fatal("missing generated ID")
	}
	if ErrorFields(context.DeadlineExceeded)[1] != "deadline" {
		t.Fatal("timeout classification")
	}
}
