package chatintegration

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

func Installed(ctx context.Context, q postgres.DBTX) (bool, error) {
	var b bool
	e := q.QueryRow(ctx, `SELECT to_regclass('chat_schema_state') IS NOT NULL`).Scan(&b)
	return b, e
}
func CompanyWritable(ctx context.Context, q postgres.DBTX, id int64) error {
	b, e := Installed(ctx, q)
	if e != nil || !b {
		return e
	}
	var deleting bool
	if e = q.QueryRow(ctx, `SELECT chat_deleting FROM companies WHERE id=$1`, id).Scan(&deleting); e != nil {
		return e
	}
	if deleting {
		return errors.New("Корпорация удаляется: дождитесь завершения очистки чата")
	}
	return nil
}
func RequestCompany(ctx context.Context, tx pgx.Tx, id int64) error {
	if _, e := tx.Exec(ctx, `UPDATE companies SET chat_deleting=true WHERE id=$1 AND NOT starter AND NOT founder`, id); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `INSERT INTO chat_company_deletion_requests(company_id,operation_id) SELECT id,gen_random_uuid() FROM companies WHERE id=$1 AND chat_deleting ON CONFLICT DO NOTHING`, id)
	return e
}

// PrepareSurface persists a request before Cosmic can erase canonical geography.
func PrepareSurface(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	enabled, e := Installed(ctx, tx)
	if e != nil || !enabled {
		return !enabled, e
	}
	if _, e = tx.Exec(ctx, `UPDATE planet_runtime_controls SET state='retiring' WHERE surface=$1`, id); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chat_surface_cleanup_requests(surface_id,operation_id) VALUES($1,gen_random_uuid()) ON CONFLICT DO NOTHING`, id); e != nil {
		return false, e
	}
	var ready bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_surface_cleanup_requests r JOIN chat_surface_cleanup_receipts c USING(surface_id,operation_id) WHERE r.surface_id=$1)`, id).Scan(&ready)
	return ready, e
}
