package corporations

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/commands"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
)

// SignalTarget captures only operation targets BEFORE a command can remove its
// request row. It never publishes inside the caller-owned transaction.
func (a *Service) SignalTarget(ctx context.Context, tx pgx.Tx, actor int64, in commands.Command) (int64, error) {
	if a.Signals == nil {
		return 0, nil
	}
	switch in.Action {
	case "request_accept", "invitation_accept":
		var id int64
		e := tx.QueryRow(ctx, `SELECT user_id FROM company_requests WHERE id=$1`, in.ID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return 0, nil
		}
		return id, e
	case "role_assign":
		return in.ID, nil
	case "role_save", "role_delete", "company_transfer", "company_dissolve":
		return actor, nil
	}
	return 0, nil
}
func number(id int64) string { return strconv.FormatInt(id, 10) }

// StageCommand reads confirmed-in-transaction metadata. The caller emits ONLY
// after its COMMIT; rollback discards these messages along with the mutation.
func (a *Service) StageCommand(ctx context.Context, tx pgx.Tx, target int64, action string) ([]gamesignal.Message, error) {
	if a.Signals == nil || target == 0 {
		return nil, nil
	}
	subject := gamesignal.MembershipSubject
	d := gamesignal.CompanyChange{UserID: number(target)}
	switch action {
	case "role_assign", "role_save", "role_delete", "company_transfer", "company_dissolve":
		installed, e := chatintegration.Installed(ctx, tx)
		if e != nil || !installed {
			return nil, e
		}
		var company, fence int64
		if e = tx.QueryRow(ctx, `SELECT u.company_id,f.revision FROM users u JOIN chat_company_access_fences f ON f.company_id=u.company_id WHERE u.id=$1`, target).Scan(&company, &fence); e != nil {
			return nil, e
		}
		d = gamesignal.CompanyChange{CompanyID: number(company), Fence: number(fence)}
		subject = gamesignal.PermissionsSubject
		if action == "role_assign" {
			subject = gamesignal.RoleSubject
			d.UserID = number(target)
		}
		if action == "company_dissolve" {
			var op string
			if e = tx.QueryRow(ctx, `SELECT operation_id::text FROM chat_company_deletion_requests WHERE company_id=$1 AND state='deleting'`, company).Scan(&op); e != nil {
				return nil, e
			}
			subject = gamesignal.DeletionSubject
			d.Fence = ""
			d.Operation = op
		}
	}
	m, ok := gamesignal.EncodeCompany(subject, d)
	if !ok {
		return nil, nil
	}
	return []gamesignal.Message{m}, nil
}
func (a *Service) EmitSignals(messages []gamesignal.Message) {
	if a.Signals != nil {
		for _, m := range messages {
			a.Signals(m)
		}
	}
}
func (a *Service) MembershipCommitted(user int64) {
	if a.Signals == nil {
		return
	}
	m, ok := gamesignal.EncodeCompany(gamesignal.MembershipSubject, gamesignal.CompanyChange{UserID: number(user)})
	if ok {
		a.EmitSignals([]gamesignal.Message{m})
	}
}

// WakeChatCleanup coalesces receipt hints; the existing SQL join remains the
// only authority for deletion/refund. A dropped wake is covered by its timer.
func (a *Service) WakeChatCleanup() {
	if a.CleanupWake != nil {
		select {
		case a.CleanupWake <- struct{}{}:
		default:
		}
	}
}
