package visits

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
)

var ErrLandingDistance = errors.New("return_near_landing_required")
var ErrReturnOperation = errors.New("return_operation_conflict")

type ReturnProof struct {
	Operation string    `json:"operation"`
	Expires   time.Time `json:"expires_at"`
}

// PrepareReturn reads the checkpointed authoritative character, never browser
// coordinates. The caller freezes movement and owns the existing user gate.
func (a *Service) PrepareReturn(ctx context.Context, id access.Identity, w Welcome, f scenestore.Fence, s *scene.Scene, operation string) (ReturnProof, error) {
	proof := ReturnProof{Operation: operation}
	if !access.ValidUUID(operation) {
		return proof, ErrReturnOperation
	}
	tx, err := a.locked(ctx, id.User, f)
	if err != nil {
		return proof, err
	}
	defer tx.Rollback(context.Background())
	if err = check(ctx, tx, id, w, f); err != nil {
		return proof, err
	}
	if err = scenestore.CheckSource(ctx, tx, s); err != nil {
		return proof, err
	}
	var site, ship int64
	var x, y int
	var version int64
	err = tx.QueryRow(ctx, `SELECT v.landing_site,v.ship,c.x_q,c.y_q,c.position_version FROM planet_visits v JOIN planet_characters c ON c.user_id=v.user_id WHERE v.user_id=$1 AND v.visit_id=$2 AND v.generation=$3 AND v.connection_epoch=$4 AND v.current_surface=$5 AND v.current_sector=$6 AND v.revision=$7 AND c.last_surface=v.current_surface AND c.last_sector=v.current_sector AND c.last_revision=v.revision`, id.User, w.Visit, w.Generation, w.Epoch, s.Key().Surface, s.Key().Sector, s.Key().Revision).Scan(&site, &ship, &x, &y, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, ErrVisit
	}
	if err != nil {
		return proof, err
	}
	landing, err := access.Landing(ctx, tx, id.User, site, ship)
	if err != nil {
		return proof, err
	}
	placement := s.Metadata().Landing
	distance := int(math.Ceil(math.Hypot(float64(x-landing.X), float64(y-landing.Y))))
	if placement == nil || landing.Surface != s.Key().Surface || landing.Sector != s.Key().Sector || placement.SpawnX != landing.X || placement.SpawnY != landing.Y || distance > access.ReturnRadiusQ || version != w.PositionVersion || !s.Walkable(x/1024, y/1024) {
		return proof, ErrLandingDistance
	}
	if _, err = tx.Exec(ctx, `UPDATE aj_planet_return_authorizations SET state='expired' WHERE user_id=$1 AND state='authorized' AND (expires_at<=clock_timestamp() OR visit_id<>$2 OR connection_epoch<>$3 OR generation<>$4 OR runtime_fence<>$5)`, id.User, w.Visit, w.Epoch, w.Generation, f.Number); err != nil {
		return proof, err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT visit_id=$3 AND connection_epoch=$4 AND generation=$5 AND runtime_fence=$6 AND state='authorized' AND position_version=$7 AND expires_at>clock_timestamp(),expires_at FROM aj_planet_return_authorizations WHERE user_id=$1 AND operation=$2 FOR UPDATE`, id.User, operation, w.Visit, w.Epoch, w.Generation, f.Number, version).Scan(&valid, &proof.Expires)
	if err == nil {
		if !valid {
			return proof, ErrReturnOperation
		}
		if err = tx.Commit(ctx); err != nil {
			return proof, err
		}
		return proof, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return proof, err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aj_planet_return_authorizations WHERE user_id=$1 AND state='authorized')`, id.User).Scan(&pending); err != nil {
		return proof, err
	}
	if pending {
		return proof, ErrReturnOperation
	}
	err = tx.QueryRow(ctx, `INSERT INTO aj_planet_return_authorizations(user_id,operation,visit_id,generation,connection_epoch,runtime_fence,surface_id,sector_number,landing_site_id,position_x_q,position_y_q,distance_q,position_version,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,clock_timestamp()+interval '30 seconds') RETURNING expires_at`, id.User, operation, w.Visit, w.Generation, w.Epoch, f.Number, s.Key().Surface, s.Key().Sector, site, x, y, distance, version).Scan(&proof.Expires)
	if err != nil {
		return proof, err
	}
	if err = tx.Commit(ctx); err != nil {
		return proof, err
	}
	return proof, nil
}
