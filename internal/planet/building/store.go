package building

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

func Allowed(ctx context.Context, q postgres.Querier, uid int64, k scene.Key) (bool, error) {
	var allowed bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_colonies cl JOIN planet_projects p ON p.id=cl.project JOIN users u ON u.id=$1 LEFT JOIN companies co ON co.id=cl.owner_company LEFT JOIN company_roles r ON r.id=u.company_role AND r.company_id=u.company_id WHERE cl.surface=$2 AND cl.sector=$3 AND p.status='complete' AND (cl.owner_user=$1 OR (cl.owner_company=u.company_id AND (co.owner_id=$1 OR COALESCE(r.permissions,'[]'::jsonb)?'colonize'))))`, uid, k.Surface, k.Sector).Scan(&allowed)
	return allowed, err
}
func Load(ctx context.Context, q postgres.Querier, k scene.Key) (Snapshot, error) {
	s := Snapshot{Objects: []Object{}, Removed: []string{}}
	var raw, removed []byte
	err := q.QueryRow(ctx, `SELECT version,objects,removed_objects FROM planet_sector_buildings WHERE surface=$1 AND sector=$2 AND revision=$3`, k.Surface, k.Sector, k.Revision).Scan(&s.Version, &raw, &removed)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s.Objects); err != nil {
		return s, err
	}
	err = json.Unmarshal(removed, &s.Removed)
	return s, err
}

type Command struct {
	Action string `json:"action"`
	Object Object `json:"object"`
}

// Apply uses the caller's checked visit transaction. An object UUID is also the
// idempotency key, so a repeated placement can never duplicate an object.
func Apply(ctx context.Context, tx pgx.Tx, uid int64, base *scene.Scene, c Command, people []pathing.Point, builder pathing.Point) (Snapshot, error) {
	var empty Snapshot
	k := base.Key()
	allowed, err := Allowed(ctx, tx, uid, k)
	if err != nil {
		return empty, err
	}
	if !allowed {
		return empty, ErrDenied
	}
	if (c.Action != "clear" && !access.ValidUUID(c.Object.ID)) || (c.Action != "place" && c.Action != "remove" && c.Action != "clear") {
		return empty, ErrPlacement
	}
	_, err = tx.Exec(ctx, `INSERT INTO planet_sector_buildings(surface,sector,revision) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, k.Surface, k.Sector, k.Revision)
	if err != nil {
		return empty, err
	}
	var locked int64
	if err = tx.QueryRow(ctx, `SELECT version FROM planet_sector_buildings WHERE surface=$1 AND sector=$2 AND revision=$3 FOR UPDATE`, k.Surface, k.Sector, k.Revision).Scan(&locked); err != nil {
		return empty, err
	}
	s, err := Load(ctx, tx, k)
	if err != nil {
		return empty, err
	}
	found := -1
	for i, o := range s.Objects {
		if o.ID == c.Object.ID {
			found = i
			break
		}
	}
	if c.Action == "clear" {
		var target *scene.Object
		for _, o := range base.Metadata().Objects {
			if o.ID == c.Object.ID {
				copy := o
				target = &copy
				break
			}
		}
		if target == nil {
			return empty, ErrPlacement
		}
		p := pathing.FromQ(target.Position.X, target.Position.Y)
		if (p.X-builder.X)*(p.X-builder.X)+(p.Y-builder.Y)*(p.Y-builder.Y) > Reach*Reach {
			return empty, errors.New("build_too_far")
		}
		for _, id := range s.Removed {
			if id == target.ID {
				return s, nil
			}
		}
		s.Removed = append(s.Removed, target.ID)
	} else if c.Action == "remove" {
		if found < 0 {
			return s, nil
		}
		o := s.Objects[found]
		if o.Owner != uid {
			return empty, ErrDenied
		}
		if p := Position(o); (p.X-builder.X)*(p.X-builder.X)+(p.Y-builder.Y)*(p.Y-builder.Y) > Reach*Reach {
			return empty, errors.New("build_too_far")
		}
		s.Objects = append(s.Objects[:found], s.Objects[found+1:]...)
	} else {
		o := c.Object
		o.Owner = uid
		o.Mine = false
		if found >= 0 {
			old := s.Objects[found]
			if old.ID == o.ID && old.Kind == o.Kind && old.X == o.X && old.Y == o.Y && old.Rotation == o.Rotation && old.Owner == uid {
				return s, nil
			}
			return empty, ErrPlacement
		}
		if len(s.Objects) >= Limit {
			return empty, ErrLimit
		}
		// Disconnected characters keep their saved spot. Protect them from enclosure
		// at reconnect, as well as the live positions captured by the sector editor.
		rows, e := tx.Query(ctx, `SELECT x_q,y_q FROM planet_characters WHERE last_surface=$1 AND last_sector=$2 AND last_revision=$3`, k.Surface, k.Sector, k.Revision)
		if e != nil {
			return empty, e
		}
		for rows.Next() {
			var x, y int
			if e = rows.Scan(&x, &y); e != nil {
				rows.Close()
				return empty, e
			}
			people = append(people, pathing.FromQ(x, y))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return empty, e
		}
		if err = Validate(base.WithoutObjects(s.Removed), s.Objects, o, people, builder); err != nil {
			return empty, err
		}
		s.Objects = append(s.Objects, o)
	}
	raw, err := json.Marshal(s.Objects)
	if err != nil {
		return empty, err
	}
	removedRaw, err := json.Marshal(s.Removed)
	if err != nil {
		return empty, err
	}
	s.Version++
	_, err = tx.Exec(ctx, `UPDATE planet_sector_buildings SET version=$4,objects=$5,removed_objects=$6 WHERE surface=$1 AND sector=$2 AND revision=$3`, k.Surface, k.Sector, k.Revision, s.Version, raw, removedRaw)
	return s, err
}
