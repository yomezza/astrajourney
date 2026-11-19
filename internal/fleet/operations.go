package fleet

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

func Idle(ctx context.Context, tx pgx.Tx, s Ship) error {
	if err := Available(ctx, tx, s.ID); err != nil {
		return err
	}
	if s.Destination != nil || s.LocalTarget != nil {
		return errors.New("Корабль в пути")
	}
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=$1) OR EXISTS(SELECT 1 FROM research_jobs WHERE ship_id=$1 AND NOT paused) OR EXISTS(SELECT 1 FROM deliveries WHERE ship_id=$1 AND status='carrying')`, s.ID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return errors.New("Сначала завершите или приостановите текущее задание")
	}
	return nil
}

func IsShipOperation(action string) bool {
	switch action {
	case "module_buy", "module_install", "module_remove", "module_sell", "module_scoop", "queue_route", "cancel_route", "explore", "rescue", "extractor", "portal_travel", "store", "withdraw", "delivery_accept", "delivery_finish", "company_store", "company_cargo_withdraw":
		return true
	}
	return false
}

func AutomationBusy(ctx context.Context, q postgres.Querier, sid int64) (bool, error) {
	var yes bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_tasks WHERE ship_id=$1 AND status='running')`, sid).Scan(&yes)
	return yes, err
}
