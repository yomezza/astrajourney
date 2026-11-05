// Package access shares read-only entry/session rules between the two services.
package access

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

var ErrDenied = errors.New("access_denied")
var ErrAuth = errors.New("auth_required")

type Identity struct {
	Nickname string
	User     int64
	Session  string
}

func Authenticate(ctx context.Context, q postgres.Querier, r *http.Request) (Identity, error) {
	c, err := r.Cookie("session")
	if err != nil || len(c.Value) != 64 {
		return Identity{}, ErrAuth
	}
	return Session(ctx, q, account.TokenHash(c.Value))
}
func Session(ctx context.Context, q postgres.Querier, hash string) (Identity, error) {
	var id Identity
	id.Session = hash
	err := q.QueryRow(ctx, `SELECT s.user_id,s.nickname FROM planet_session_lookup s WHERE s.token_hash=$1 AND s.expires_at>clock_timestamp() AND NOT s.is_bot`, hash).Scan(&id.User, &id.Nickname)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrAuth
	}
	return id, err
}

type Site struct {
	Name                                string
	ID, Colony, Surface, Ship, Revision int64
	Sector, Star, Planet                int
	X, Y                                int
}

func Landing(ctx context.Context, q postgres.Querier, uid, site, ship int64) (Site, error) {
	var s Site
	s.Ship = ship
	err := q.QueryRow(ctx, `SELECT l.id,l.colony,l.surface,l.sector,l.geography_revision,s.star,s.planet,s.name,l.spawn_x_q,l.spawn_y_q
 FROM planet_landing_sites l JOIN planet_colonies c ON c.id=l.colony
 JOIN planet_projects p ON p.id=c.project JOIN planet_surfaces s ON s.id=l.surface
 JOIN planet_runtime_controls rc ON rc.surface=s.id
 LEFT JOIN planet_surface_controls sc ON sc.star=s.star AND sc.planet=s.planet
 JOIN ships sh ON sh.id=$3 AND sh.user_id=$1
 JOIN users u ON u.id=$1 LEFT JOIN companies co ON co.id=c.owner_company
 LEFT JOIN company_roles r ON r.id=u.company_role AND r.company_id=u.company_id
 WHERE l.id=$2 AND l.enabled AND l.layout_version=1 AND p.status='complete'
 AND rc.state='enabled' AND rc.geography_revision=l.geography_revision
 AND COALESCE(sc.revision,0)=l.geography_revision AND NOT COALESCE(sc.deleted,false)
 AND sh.star=s.star AND sh.object_id='planet-'||s.planet AND sh.destination IS NULL AND sh.local_target IS NULL
 AND jsonb_array_length(sh.waypoints)=0
 AND EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=s.star)
 AND (c.owner_user=$1 OR (c.owner_company=u.company_id AND (co.owner_id=$1 OR COALESCE(r.permissions,'[]'::jsonb)?'planetary.enter')))`, uid, site, ship).Scan(&s.ID, &s.Colony, &s.Surface, &s.Sector, &s.Revision, &s.Star, &s.Planet, &s.Name, &s.X, &s.Y)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrDenied
	}
	return s, err
}
func UUID() (string, error)   { return identifiers.UUID() }
func ValidUUID(v string) bool { return identifiers.ValidUUID(v) }
