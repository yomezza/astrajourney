package chat

import (
	"bufio"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"os"
	"strings"
	"sync"
)

// ErasureJournal lives outside PostgreSQL backups, contains IDs only, and is
// fsynced before deletion. Startup verifies installation and replays every ID.
// A missing, malformed, mismatched or truncated journal fails closed.
type ErasureJournal struct {
	mu   sync.Mutex
	file *os.File
	ids  map[string]bool
}

func OpenJournal(ctx context.Context, s *Service, path string) (*ErasureJournal, error) {
	if path == "" {
		return nil, errors.New("CHAT_ERASURE_JOURNAL is required")
	}
	var populated bool
	var installation string
	var watermark int64
	if e := s.DB.QueryRow(ctx, `SELECT installation_id::text,erasure_count,EXISTS(SELECT 1 FROM chat_message_locator) OR EXISTS(SELECT 1 FROM chat_operations) OR EXISTS(SELECT 1 FROM chat_audit) FROM chat_recovery_state WHERE id=1`).Scan(&installation, &watermark, &populated); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0600)
	created := false
	if os.IsNotExist(e) && !populated && watermark == 0 {
		f, e = os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_EXCL, 0600)
		created = e == nil
	}
	if e != nil {
		return nil, errors.New("erasure journal unavailable: restore it or perform verified PITR before starting Chat")
	}
	j := &ErasureJournal{file: f, ids: map[string]bool{}}
	fail := func() (*ErasureJournal, error) {
		f.Close()
		return nil, errors.New("erasure journal invalid or mismatched; Chat is fail-closed")
	}
	header := "COSMOS-CHAT-ERASURE/1 " + installation
	if created {
		if _, e = f.WriteString(header + "\n"); e != nil {
			return fail()
		}
		if e = f.Sync(); e != nil {
			return fail()
		}
		if _, e = f.Seek(0, 0); e != nil {
			return fail()
		}
	}
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() || scanner.Text() != header {
		return fail()
	}
	for scanner.Scan() {
		id := scanner.Text()
		if !identifiers.ValidUUID(id) {
			return fail()
		}
		j.ids[id] = true
	}
	if scanner.Err() != nil || int64(len(j.ids)) < watermark {
		return fail()
	}
	if e = j.Replay(ctx, s); e != nil {
		f.Close()
		return nil, e
	}
	return j, nil
}
func (j *ErasureJournal) Close() error { return j.file.Close() }
func (j *ErasureJournal) Record(ids ...string) error {
	if j == nil {
		return errors.New("erasure journal unavailable")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, id := range ids {
		if !identifiers.ValidUUID(id) {
			return failure("invalid_argument")
		}
		if !j.ids[id] {
			if _, e := j.file.WriteString(id + "\n"); e != nil {
				return e
			}
			if e := j.file.Sync(); e != nil {
				return e
			}
			j.ids[id] = true
		}
	}
	return nil
}
func (j *ErasureJournal) Watermark(ctx context.Context, tx pgx.Tx) error {
	j.mu.Lock()
	count := len(j.ids)
	j.mu.Unlock()
	_, e := tx.Exec(ctx, `UPDATE chat_recovery_state SET erasure_count=GREATEST(erasure_count,$1) WHERE id=1`, count)
	return e
}
func (j *ErasureJournal) Replay(ctx context.Context, s *Service) error {
	j.mu.Lock()
	ids := make([]string, 0, len(j.ids))
	for id := range j.ids {
		ids = append(ids, id)
	}
	j.mu.Unlock()
	for i := 0; i < len(ids); i += 500 {
		batch := ids[i:min(i+500, len(ids))]
		tx, e := s.DB.Begin(ctx)
		if e != nil {
			return e
		}
		for _, q := range []string{`DELETE FROM chat_saved_items WHERE source_message_id=ANY($1::uuid[])`, `DELETE FROM chat_messages WHERE id=ANY($1::uuid[])`, `DELETE FROM chat_message_locator WHERE id=ANY($1::uuid[])`} {
			if _, e = tx.Exec(ctx, q, batch); e != nil {
				break
			}
		}
		if e == nil {
			e = j.Watermark(ctx, tx)
		}
		if e == nil {
			e = tx.Commit(ctx)
		} else {
			tx.Rollback(context.Background())
		}
		if e != nil {
			return e
		}
		s.committed("replay")
	}
	return nil
}
func validJournalHeader(s string) bool { return strings.HasPrefix(s, "COSMOS-CHAT-ERASURE/1 ") }
