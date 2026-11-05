// Package chatintegration contains the narrow Cosmic-owned integration contract.
package chatintegration

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"strings"
	"time"
)

func Partitions(ctx context.Context, tx postgres.DBTX, now time.Time) error {
	for _, kind := range []string{"global", "planet", "sector", "corporation", "private"} {
		weekly := kind == "private" || kind == "corporation"
		start := now.UTC().Truncate(24 * time.Hour)
		step, count := 24*time.Hour, 15
		if weekly {
			start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
			step = 7 * 24 * time.Hour
			count = 9
		}
		for i := -1; i < count; i++ {
			from := start.Add(time.Duration(i) * step)
			to := from.Add(step)
			name := "chat_messages_" + kind + "_" + from.Format("20060102")
			if weekly {
				name += "_week"
			}
			sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')", pgx.Identifier{name}.Sanitize(), pgx.Identifier{"chat_messages_" + kind}.Sanitize(), from.Format(time.RFC3339), to.Format(time.RFC3339))
			if _, e := tx.Exec(ctx, sql); e != nil {
				return e
			}
		}
	}
	return SearchIndex(ctx, tx)
}
func SearchIndex(ctx context.Context, tx postgres.DBTX) error {
	var namespace string
	if e := tx.QueryRow(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='pg_trgm'`).Scan(&namespace); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `CREATE INDEX IF NOT EXISTS chat_messages_search ON chat_messages USING gin(lower(body COLLATE chat_unicode) `+pgx.Identifier{namespace, "gin_trgm_ops"}.Sanitize()+`)`)
	return e
}
func Functions(ctx context.Context, tx pgx.Tx, schema string) error {
	s := pgx.Identifier{schema}.Sanitize()
	sql := strings.ReplaceAll(functions, "@REG@", strings.ReplaceAll(s, "'", "''"))
	sql = strings.ReplaceAll(sql, "@S@", s)
	_, e := tx.Exec(ctx, sql)
	return e
}

const functions = `
CREATE OR REPLACE FUNCTION @S@.chat_validate_session(token text) RETURNS TABLE(id bigint,name text,role text,game_moderator boolean) LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=@S@,pg_catalog AS $f$
 SELECT u.id,u.username,u.role,EXISTS(SELECT 1 FROM @S@.chat_game_moderator_grants g WHERE g.user_id=u.id AND g.revoked_at IS NULL) FROM @S@.sessions s JOIN @S@.users u ON u.id=s.user_id WHERE s.token_hash=token AND s.expires_at>pg_catalog.clock_timestamp() AND NOT u.is_bot
$f$;
REVOKE ALL ON FUNCTION @S@.chat_validate_session(text) FROM PUBLIC;
CREATE OR REPLACE FUNCTION @S@.chat_lock_company_access(actor bigint,company bigint) RETURNS TABLE(owner_id bigint,permissions jsonb,revision bigint) LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=@S@,pg_catalog AS $f$
BEGIN
 PERFORM 1 FROM @S@.chat_company_access_fences f WHERE f.company_id=company FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'access_denied' USING ERRCODE='42501';END IF;
 RETURN QUERY SELECT COALESCE(c.owner_id,0),COALESCE(r.permissions,'[]'::jsonb),f.revision FROM @S@.users u JOIN @S@.companies c ON c.id=u.company_id JOIN @S@.chat_company_access_fences f ON f.company_id=c.id LEFT JOIN @S@.company_roles r ON r.id=u.company_role AND r.company_id=c.id WHERE u.id=actor AND c.id=company AND NOT c.chat_deleting;
END $f$;
REVOKE ALL ON FUNCTION @S@.chat_lock_company_access(bigint,bigint) FROM PUBLIC;
CREATE OR REPLACE FUNCTION @S@.chat_lock_planet_presence(actor bigint) RETURNS TABLE(surface bigint,sector integer,epoch bigint) LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=@S@,pg_catalog AS $f$
BEGIN
 PERFORM 1 FROM @S@.planet_visits v WHERE v.user_id=actor FOR SHARE;
 RETURN QUERY SELECT v.current_surface,v.current_sector,v.connection_epoch FROM @S@.planet_visits v JOIN @S@.planet_runtime_leases l ON l.id=1 AND l.fence=v.runtime_fence JOIN @S@.planet_runtime_controls r ON r.surface=v.current_surface WHERE v.user_id=actor AND v.state='active' AND v.checked_at>pg_catalog.clock_timestamp()-interval '2 seconds' AND l.owner_id IS NOT NULL AND l.expires_at>pg_catalog.clock_timestamp() AND r.state='enabled' AND r.geography_revision=v.revision;
END $f$;
REVOKE ALL ON FUNCTION @S@.chat_lock_planet_presence(bigint) FROM PUBLIC;
CREATE OR REPLACE FUNCTION @S@.chat_lock_service_scope(actor bigint,channel uuid) RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=@S@,pg_catalog AS $f$
DECLARE scope record;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM @S@.users u WHERE u.id=actor AND (u.role='admin' OR EXISTS(SELECT 1 FROM @S@.chat_game_moderator_grants g WHERE g.user_id=u.id AND g.revoked_at IS NULL))) THEN RETURN false;END IF;
 SELECT * INTO scope FROM @S@.chat_channels WHERE id=channel AND kind<>'private';IF NOT FOUND THEN RETURN false;END IF;
 IF scope.kind='corporation' THEN
  PERFORM 1 FROM @S@.chat_company_access_fences WHERE company_id=scope.company_id FOR SHARE;
  IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM @S@.companies WHERE id=scope.company_id AND NOT chat_deleting) THEN RETURN false;END IF;
 ELSIF scope.kind IN('planet','sector') THEN
  PERFORM 1 FROM @S@.planet_runtime_controls WHERE surface=scope.surface_id AND state='enabled' FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END IF;RETURN true;
END $f$;
REVOKE ALL ON FUNCTION @S@.chat_lock_service_scope(bigint,uuid) FROM PUBLIC;
CREATE OR REPLACE FUNCTION @S@.chat_scope_stamp(actor bigint) RETURNS text LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=@S@,pg_catalog AS $f$
 SELECT concat(u.id,'/',u.role,'/',EXISTS(SELECT 1 FROM @S@.chat_game_moderator_grants g WHERE g.user_id=u.id AND g.revoked_at IS NULL),'/',u.company_id,'/',f.revision,'/',p.surface,'/',p.sector,'/',p.epoch) FROM @S@.users u LEFT JOIN @S@.chat_company_access_fences f ON f.company_id=u.company_id LEFT JOIN LATERAL @S@.chat_lock_planet_presence(u.id) p ON true WHERE u.id=actor AND NOT u.is_bot
$f$;
REVOKE ALL ON FUNCTION @S@.chat_scope_stamp(bigint) FROM PUBLIC;
-- Revision maintenance is an invariant, not a policy/auth trigger. Only Cosmic
-- can mutate these source tables; Chat cannot alter memberships or revisions.
CREATE OR REPLACE FUNCTION @S@.chat_touch_company_fence() RETURNS trigger LANGUAGE plpgsql SET search_path=@S@,pg_catalog AS $f$
DECLARE before_id bigint;after_id bigint;cid bigint;
BEGIN
 IF TG_TABLE_NAME='users' THEN
  IF TG_OP<>'INSERT' THEN before_id:=OLD.company_id;END IF;
  IF TG_OP<>'DELETE' THEN after_id:=NEW.company_id;END IF;
 ELSIF TG_TABLE_NAME='company_roles' THEN
  IF TG_OP<>'INSERT' THEN before_id:=OLD.company_id;END IF;
  IF TG_OP<>'DELETE' THEN after_id:=NEW.company_id;END IF;
 ELSE
  IF TG_OP<>'INSERT' THEN before_id:=OLD.id;END IF;
  IF TG_OP<>'DELETE' THEN after_id:=NEW.id;END IF;
 END IF;
 IF TG_TABLE_NAME='users' AND TG_OP<>'DELETE' AND after_id IS NOT NULL AND (TG_OP='INSERT' OR before_id IS DISTINCT FROM after_id) THEN
  IF EXISTS(SELECT 1 FROM @S@.companies c WHERE c.id=after_id AND c.chat_deleting) THEN RAISE EXCEPTION 'company_deleting' USING ERRCODE='42501';END IF;
 END IF;
 FOR cid IN SELECT DISTINCT x FROM unnest(ARRAY[before_id,after_id]) x WHERE x IS NOT NULL ORDER BY x LOOP
  IF TG_TABLE_NAME='companies' AND TG_OP='INSERT' THEN
   INSERT INTO @S@.chat_company_access_fences(company_id) VALUES(cid);
  ELSE
   UPDATE @S@.chat_company_access_fences SET revision=revision+1,updated_at=pg_catalog.clock_timestamp() WHERE company_id=cid;
   IF NOT FOUND THEN RAISE EXCEPTION 'company fence missing';END IF;
  END IF;
 END LOOP;
 RETURN NULL;
END $f$;
DO $f$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='@REG@.users'::regclass AND tgname='chat_users_fence') THEN CREATE TRIGGER chat_users_fence AFTER INSERT OR DELETE OR UPDATE OF company_id,company_role ON @S@.users FOR EACH ROW EXECUTE FUNCTION @S@.chat_touch_company_fence();END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='@REG@.companies'::regclass AND tgname='chat_companies_fence') THEN CREATE TRIGGER chat_companies_fence AFTER INSERT OR DELETE OR UPDATE OF owner_id,chat_deleting ON @S@.companies FOR EACH ROW EXECUTE FUNCTION @S@.chat_touch_company_fence();END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='@REG@.company_roles'::regclass AND tgname='chat_roles_fence') THEN CREATE TRIGGER chat_roles_fence AFTER INSERT OR UPDATE OR DELETE ON @S@.company_roles FOR EACH ROW EXECUTE FUNCTION @S@.chat_touch_company_fence();END IF;
END $f$;
`
