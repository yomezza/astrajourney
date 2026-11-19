package mining

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

// Shared by manual mining and automation: visibility, extractor, cargo reservation
// and global deposit quotas are enforced in exactly the same transaction.
func BeginExtraction(ctx context.Context, tx pgx.Tx, uid int64, s fleet.Ship, star universe.Star, planet int, res string, limit int) (*MiningJob, error) {
	var known, busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2),EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=$3)`, uid, s.Star, s.ID).Scan(&known, &busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, errors.New("Добыча уже выполняется")
	}
	if !known {
		return nil, errors.New("Сначала исследуйте систему")
	}
	source := ""
	for _, site := range universe.SystemSites(star) {
		if site.Planet == planet {
			for _, id := range site.Resources {
				if id == res {
					source = site.ID
				}
			}
		}
	}
	if source == "" {
		return nil, errors.New("Ресурс отсутствует на планете")
	}
	if !fleet.NearObject(s, source) {
		return nil, errors.New("Приблизьтесь к объекту добычи")
	}
	count := s.ExtractionYieldFor(s.Star, planet, res)
	if count <= 0 {
		return nil, errors.New("Установите подходящий добытчик")
	}
	if limit > 0 {
		count = min(count, limit)
	}
	if s.Capacity()-s.CargoUsed() < count {
		return nil, errors.New("Трюм заполнен")
	}
	count, start, err := reserveExtraction(ctx, tx, s.Star, planet, res, count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("Лимит добычи исчерпан")
	}
	var tick int64
	if err = tx.QueryRow(ctx, `SELECT tick FROM world WHERE id=1`).Scan(&tick); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mining_jobs(user_id,star,planet,resource,quantity,finish_tick,quota_start,ship_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, uid, s.Star, planet, res, count, tick+1, start, s.ID)
	return &MiningJob{Resource: res, Quantity: count, FinishTick: tick + 1, StartedAt: time.Now(), Planet: planet}, err
}
