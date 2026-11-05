package integration

import (
	"bytes"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Executes pg_dump/pg_restore inside the selected disposable PostgreSQL test
// container. Never infers or targets a production database/container.
func TestChat240BackupRestore(t *testing.T) {
	container := os.Getenv("CHAT_BACKUP_DOCKER_CONTAINER")
	if container == "" {
		t.Skip("CHAT_BACKUP_DOCKER_CONTAINER selects the disposable PostgreSQL test container")
	}
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	cookie, uid := f.signup("chat_backup")
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	config := f.db.Config().ConnConfig
	run := func(program string, input []byte, args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(f.ctx, "docker", append([]string{"exec", "-i", container, program, "-U", config.User, "-d", config.Database}, args...)...)
		command.Stdin = bytes.NewReader(input)
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		if e := command.Run(); e != nil {
			t.Fatalf("%s failed: %v %s", program, e, stderr.String())
		}
		return stdout.Bytes()
	}
	s := &chat.Service{DB: f.db}
	path := filepath.Join(t.TempDir(), "erasure.log")
	j, e := chat.OpenJournal(f.ctx, s, path)
	if e != nil {
		t.Fatal(e)
	}
	s.Journal = j
	u, e := s.Authenticate(f.ctx, account.TokenHash(cookie.Value))
	if e != nil {
		t.Fatal(e)
	}
	lease, e := s.Claim(f.ctx, u, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	command := func(typ string, body any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r, e := s.Execute(f.ctx, u, lease, chat.Command{Type: typ, Request: chat.UUID(), Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		var result map[string]any
		json.Unmarshal(r, &result)
		return result
	}
	message := command("message.send", map[string]any{"kind": "global", "body": "Text erased after this backup"})["message_id"].(string)
	command("favorite.copy", map[string]any{"id": message})
	dump := run("pg_dump", nil, "-Fc", "--schema", schema)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	var other int64
	if e = f.db.QueryRow(f.ctx, `INSERT INTO users(username,password_hash) VALUES('original_author','') RETURNING id`).Scan(&other); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE chat_message_locator SET author_id=$2 WHERE id=$1`, message, other)
	f.exec(`UPDATE chat_messages SET author_id=$2 WHERE id=$1`, message, other)
	command("message.delete", map[string]any{"id": message, "reason": "Policy violation"})
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	// Restore into an empty owned schema: pg_restore --clean cannot drop
	// inherited partition constraints independently on PostgreSQL 17.
	f.exec("DROP SCHEMA " + pgx.Identifier{schema}.Sanitize() + " CASCADE")
	run("pg_restore", dump, "--no-owner", "--exit-on-error")
	if f.integer(`SELECT count(*) FROM chat_messages WHERE id=$1`, message) != 1 {
		t.Fatal("backup did not restore earlier text")
	}
	if _, e = chat.OpenJournal(f.ctx, s, filepath.Join(t.TempDir(), "lost-journal")); e == nil {
		t.Fatal("missing journal accepted a restored DB")
	}
	recovered, e := chat.OpenJournal(f.ctx, s, path)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	for _, query := range []string{`SELECT count(*) FROM chat_messages WHERE id=$1`, `SELECT count(*) FROM chat_message_locator WHERE id=$1`, `SELECT count(*) FROM chat_saved_items WHERE source_message_id=$1`} {
		if f.integer(query, message) != 0 {
			t.Fatal("restored erasure resurrected text")
		}
	}
	t.Logf("Real pg_dump/pg_restore drill: %d-byte pre-erasure backup replayed; original, locator and copied text purged before readiness", len(dump))
}
