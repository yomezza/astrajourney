package commerce

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	tradecatalog "github.com/yomezza/cosmosimulator/internal/commerce/catalog"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/universe"
	wallets "github.com/yomezza/cosmosimulator/internal/wallet"
)

// The legacy prototype ore mine is separate. Bots trade only finite cargo against
// finite station wallets; neither this routine nor a trade creates currency.
func TradeBots(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE is_bot ORDER BY id`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i, id := range ids {
		var credits int64
		if err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&credits); err != nil {
			return err
		}
		ship, err := fleet.EnsureShip(ctx, tx, id)
		if err != nil {
			return err
		}
		star := universe.Star{ID: 1, Region: 0}
		stock, wallet, err := tradecatalog.MarketAt(ctx, tx, star, "station")
		if err != nil {
			return err
		}
		var raw []byte
		var goods []string
		if err = tx.QueryRow(ctx, `SELECT goods FROM stations WHERE star=1 AND object_id='station'`).Scan(&goods); err != nil {
			return err
		}
		available := tradecatalog.StationOffers(star, stock, goods)
		if len(available) == 0 {
			continue
		}
		res := available[i%len(available)].Resource

		var offer tradecatalog.Offer
		for _, o := range tradecatalog.Offers(star, stock) {
			if o.Resource == res {
				offer = o
			}
		}
		if ship.Cargo[res] > 0 && wallet >= int64(offer.Sell) && credits <= wallets.MaxCredits-int64(offer.Sell) {
			ship.Cargo[res]--
			stock[res]++
			credits += int64(offer.Sell)
			wallet -= int64(offer.Sell)
		} else if ship.Cargo[res] == 0 && stock[res] > 0 && credits >= int64(offer.Buy) && ship.CargoUsed() < ship.Capacity() && wallet <= wallets.MaxCredits-int64(offer.Buy) {
			ship.Cargo[res]++
			stock[res]--
			credits -= int64(offer.Buy)
			wallet += int64(offer.Buy)
		}
		cargo, _ := json.Marshal(ship.Cargo)
		raw, _ = json.Marshal(stock)
		for _, q := range []struct {
			sql  string
			args []any
		}{{`UPDATE ships SET cargo=$2 WHERE user_id=$1 AND active`, []any{id, cargo}}, {`UPDATE users SET credits=$2 WHERE id=$1`, []any{id, credits}}, {`UPDATE local_markets SET credits=$1,stock=$2 WHERE star=1 AND station='station'`, []any{wallet, raw}}} {
			if _, err = tx.Exec(ctx, q.sql, q.args...); err != nil {
				return fmt.Errorf("bot trade: %w", err)
			}
		}
	}
	return nil
}
