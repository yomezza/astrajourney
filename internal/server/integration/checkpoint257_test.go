package integration

import (
	"errors"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"testing"
)

func TestCheckpointCommitFailureIsNotRetryable257(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket180(t, f, c, op180(t), site, ship)["ticket"]})
	defer conn.CloseNow()
	var fence scenestore.Fence
	if err := f.db.QueryRow(f.ctx, `SELECT owner_id,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); err != nil {
		t.Fatal(err)
	}
	local, err := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
	if err != nil {
		t.Fatal(err)
	}
	version := f.integer(`SELECT position_version FROM planet_characters WHERE user_id=$1`, uid)
	stamp := visits.Welcome{Visit: w["visit"].(string), Epoch: int64(w["connection_epoch"].(float64)), Generation: f.integer(`SELECT generation FROM planet_visits WHERE user_id=$1`, uid), PositionVersion: version}
	f.exec(`CREATE FUNCTION reject_checkpoint_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture deferred commit failure'; END $$;
 CREATE CONSTRAINT TRIGGER reject_checkpoint_commit AFTER UPDATE ON planet_characters DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_checkpoint_commit()`)
	service := visits.Service{DB: f.db}
	_, err = service.Checkpoint(f.ctx, access.Identity{User: uid, Session: account.TokenHash(c.Value)}, stamp, fence, local, int(w["x_q"].(float64)), int(w["y_q"].(float64)), false)
	if !errors.Is(err, visits.ErrCheckpointCommit) {
		t.Fatalf("COMMIT failure lost its non-retryable marker: %v", err)
	}
	if f.integer(`SELECT position_version FROM planet_characters WHERE user_id=$1`, uid) != version {
		t.Fatal("failed COMMIT changed character version")
	}
	f.exec(`DROP TRIGGER reject_checkpoint_commit ON planet_characters`)
}
