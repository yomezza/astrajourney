package worldstore

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Commit commits a world-changing transaction while excluding cache readers.
// Failed commits leave the accepted cache untouched. The caller still owns rollback.
func (a *Service) Commit(ctx context.Context, tx pgx.Tx) error {
	a.universeMu.Lock()
	defer a.universeMu.Unlock()
	err := tx.Commit(ctx)
	if err == nil {
		a.universe = nil
		a.universeEpoch++
	}
	return err
}
