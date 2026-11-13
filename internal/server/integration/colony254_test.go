package integration

import (
	"testing"
)

func TestColonyNamingOwnerAndCooldown254(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	project := addColony160(f, uid, surface, sectors[0], nil, false)
	colony := f.integer(`SELECT id FROM planet_colonies WHERE project=$1`, project)
	other, otherID := f.signup("newowner254")
	body := map[string]any{"action": "rename_colony", "request": op180(t), "id": colony, "name": "База Рассвет", "expected_revision": 0}
	f.req(other, "POST", "/api/planet/action", body, 403)
	f.req(c, "POST", "/api/planet/action", body, 200)
	f.req(c, "POST", "/api/planet/action", body, 200)
	body["request"] = op180(t)
	body["name"] = "База Горизонт"
	body["expected_revision"] = 1
	f.req(c, "POST", "/api/planet/action", body, 409)
	f.exec(`UPDATE planet_colonies SET owner_user=$2 WHERE id=$1`, colony, otherID)
	f.req(c, "POST", "/api/planet/action", body, 403)
	f.exec(`UPDATE aj_colony_names SET last_player_rename_at=clock_timestamp()-interval '25 hours' WHERE colony_id=$1`, colony)
	f.req(other, "POST", "/api/planet/action", body, 200)
	if f.integer(`SELECT count(*) FROM aj_colony_name_history WHERE colony_id=$1`, colony) != 2 || f.integer(`SELECT owner_user FROM planet_projects WHERE id=$1`, project) != uid {
		t.Fatal("names altered project identity or lost history")
	}
	// Per-surface uniqueness is enforced by the database without consuming a
	// revision on a conflict. The original project owner has no stale naming ACL.
	project2 := addColony160(f, otherID, surface, sectors[1], nil, false)
	colony2 := f.integer(`SELECT id FROM planet_colonies WHERE project=$1`, project2)
	f.req(other, "POST", "/api/planet/action", map[string]any{"action": "rename_colony", "request": op180(t), "id": colony2, "name": "База Горизонт", "expected_revision": 0}, 409)
	if f.integer(`SELECT count(*) FROM aj_colony_names WHERE colony_id=$1`, colony2) != 0 {
		t.Fatal("failed rename consumed state")
	}
}
