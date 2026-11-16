package telemetry

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
)

var diagnostics atomic.Bool

// Set by diagnostic builds so binaries with the same release version and base
// commit can be distinguished in an incident log.
var BuildLabel = "development"

// Start configures one service process, never a request or an App in a test.
// A shared TOML directory produces separate files, also mirrored to stderr.
func Start(service, directory string, enabled bool, extra ...io.Writer) (func(), error) {
	previous, previousWriter, previousFlags, previousDiagnostics := slog.Default(), log.Writer(), log.Flags(), diagnostics.Load()
	var output io.Writer = os.Stderr
	closeOutput := func() {}
	if directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, errors.New("cannot create diagnostics directory")
		}
		file, err := os.OpenFile(filepath.Join(directory, service+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, errors.New("cannot open diagnostics log")
		}
		output = io.MultiWriter(os.Stderr, file)
		closeOutput = func() { _ = file.Close() }
	}
	outputs := []io.Writer{output}
	for _, writer := range extra {
		if writer != nil {
			outputs = append(outputs, writer)
		}
	}
	output = io.MultiWriter(outputs...)
	slog.SetDefault(slog.New(slog.NewJSONHandler(output, nil)).With("service", service, "pid", os.Getpid()))
	// Route legacy lifecycle messages into the same service sink.
	log.SetFlags(0)
	diagnostics.Store(enabled)
	slog.Info("diagnostics started", "enabled", enabled, "build", BuildLabel)
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" || setting.Key == "vcs.modified" {
				slog.Info("diagnostic build", "setting", setting.Key, "value", setting.Value)
			}
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			diagnostics.Store(previousDiagnostics)
			slog.SetDefault(previous)
			log.SetOutput(previousWriter)
			log.SetFlags(previousFlags)
			closeOutput()
		})
	}, nil
}

type diagnosticKey struct{}
type traceFields struct {
	Request, Visit, Phase string
	User, Epoch           int64
}

func WithVisit(ctx context.Context, user int64, visit string, epoch int64) context.Context {
	f, _ := ctx.Value(diagnosticKey{}).(traceFields)
	f.User, f.Visit, f.Epoch = user, visit, epoch
	return context.WithValue(ctx, diagnosticKey{}, f)
}
func Phase(ctx context.Context, phase string) context.Context {
	f, _ := ctx.Value(diagnosticKey{}).(traceFields)
	f.Phase = phase
	return context.WithValue(ctx, diagnosticKey{}, f)
}
func Fields(ctx context.Context) []any {
	f, _ := ctx.Value(diagnosticKey{}).(traceFields)
	return []any{"request_id", f.Request, "visit_id", f.Visit, "user_id", f.User, "connection_epoch", f.Epoch, "phase", f.Phase}
}

// Never log Error(), SQL, arguments, headers, URL queries or PgError detail:
// PostgreSQL errors and connection errors can contain credentials/user content.
func ErrorFields(err error) []any {
	kind := "none"
	if err != nil {
		kind = "operation_failed"
	}
	if errors.Is(err, context.Canceled) {
		kind = "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		kind = "deadline"
	}
	code := ""
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		kind, code = "postgres", pg.Code
	}
	return []any{"error_kind", kind, "sqlstate", code}
}
func Event(ctx context.Context, name string, err error, fields ...any) {
	if !diagnostics.Load() {
		return
	}
	attrs := append(Fields(ctx), fields...)
	attrs = append(attrs, ErrorFields(err)...)
	if err != nil {
		slog.Warn(name, attrs...)
	} else {
		slog.Info(name, attrs...)
	}
}

// TraceHTTP does not wrap the writer: Hijack/Flush and WebSocket deadlines stay
// unchanged. The gateway forwards the sanitized request ID to Planet/Chat.
func TraceHTTP(service string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = r.URL.Query().Get("request_id")
		}
		if !identifiers.ValidUUID(id) {
			id, _ = identifiers.UUID()
		}
		r = r.Clone(context.WithValue(r.Context(), diagnosticKey{}, traceFields{Request: id}))
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		start := time.Now()
		// Hash the path to avoid dynamic IDs/content in arbitrary client URLs.
		pathID := fmt.Sprintf("%x", sha256.Sum256([]byte(r.URL.Path)))[:16]
		Event(r.Context(), "http started", nil, "component", service, "method", r.Method, "route", routeName(r.URL.Path), "path_id", pathID)
		defer func() {
			Event(r.Context(), "http ended", r.Context().Err(), "component", service, "path_id", pathID, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(w, r)
	})
}

func routeName(path string) string {
	switch path {
	case "/api/planet/visit", "/api/planet/handoff", "/api/planet/return", "/api/planet/recover", "/planet/ws/v1", "/planet/api/v1/scene", "/chat/ws/v1", "/api/login", "/api/register", "/api/logout", "/api/state", "/api/expedition":
		return path
	default:
		return "other"
	}
}
