package terminal

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
)

type expeditionInput struct {
	RequestID      string `json:"request_id,omitempty"`
	Ship           int64  `json:"ship"`
	Action         string `json:"action"`
	To             int    `json:"to"`
	Resource       string `json:"resource"`
	Quantity       int    `json:"quantity"`
	Planet         int    `json:"planet"`
	Target         string `json:"target"`
	Mode           string `json:"mode"`
	ExpectedPrice  *int   `json:"expected_price"`
	ExpectedStar   int    `json:"expected_star"`
	ExpectedObject string `json:"expected_object"`
}

type expeditionReceipt struct {
	RequestID string `json:"request_id"`
	Ship      int64  `json:"ship"`
	Action    string `json:"action"`
}

type receiptReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readExpeditionReceipt(ctx context.Context, db receiptReader, userID int64, requestID string) (expeditionReceipt, string, error) {
	receipt := expeditionReceipt{RequestID: requestID}
	var digest string
	err := db.QueryRow(ctx, `SELECT digest,ship_id,action FROM expedition_command_receipts
		WHERE user_id=$1 AND request_id=$2`, userID, requestID).Scan(&digest, &receipt.Ship, &receipt.Action)
	return receipt, digest, err
}

func (a *Service) ExpeditionCommand(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	if !identifiers.ValidUUID(id) {
		httpkit.FailCode(w, 400, "INVALID_ACTION_ID", "Некорректный идентификатор сделки")
		return
	}
	receipt, _, err := readExpeditionReceipt(r.Context(), a.DB, u.ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.FailCode(w, 404, "ACTION_UNCONFIRMED", "Подтверждение сделки пока не найдено")
		return
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, receipt)
}
