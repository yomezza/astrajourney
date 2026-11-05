package corporations

import (
	"context"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/wallet"
	"time"
)

func (a *Service) RunChatCleanup(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			check, done := context.WithTimeout(ctx, 4*time.Second)
			_ = a.FinalizeChatDeletion(check)
			done()
		}
	}
}
func (a *Service) FinalizeChatDeletion(ctx context.Context) error {
	installed, e := chatintegration.Installed(ctx, a.DB)
	if e != nil || !installed {
		return e
	}
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `SELECT c.id,c.owner_id,c.credits FROM companies c JOIN chat_company_deletion_requests r ON r.company_id=c.id JOIN chat_company_cleanup_receipts receipt USING(company_id,operation_id) WHERE c.chat_deleting AND NOT c.starter AND NOT c.founder AND r.state='deleting' ORDER BY c.id FOR UPDATE OF c,r`)
	if e != nil {
		return e
	}
	type item struct{ id, owner, balance int64 }
	items := []item{}
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.id, &i.owner, &i.balance); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, i := range items {
		if e = wallet.Credit(ctx, tx, i.owner, i.balance); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE users SET company_id=NULL,company_role=NULL WHERE company_id=$1`, i.id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE stations SET company_id=NULL,founder=(SELECT id FROM companies WHERE founder_key='founder-01') WHERE company_id=$1`, i.id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM companies WHERE id=$1`, i.id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE chat_company_deletion_requests SET state='completed' WHERE company_id=$1`, i.id); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
