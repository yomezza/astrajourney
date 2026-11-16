-- P2-4: cosmic writes controls/tickets; planet writes characters/uses/visits.
CREATE TABLE IF NOT EXISTS planet_transfer_controls (
 user_id bigint PRIMARY KEY REFERENCES users,
 generation bigint NOT NULL DEFAULT 1,
 reserved_ship bigint UNIQUE REFERENCES ships,
 active_operation uuid,
 revoke_requested_at timestamptz
);
CREATE TABLE IF NOT EXISTS planet_handoffs (
 id uuid PRIMARY KEY,user_id bigint NOT NULL REFERENCES users,
 operation uuid NOT NULL,digest text NOT NULL,
 landing_site bigint NOT NULL REFERENCES planet_landing_sites,
 surface bigint NOT NULL REFERENCES planet_surfaces,
 ship bigint NOT NULL REFERENCES ships,session_hash text NOT NULL,
 generation bigint NOT NULL,revision bigint NOT NULL,
 token_hash text NOT NULL UNIQUE,expires_at timestamptz NOT NULL,
 revoked_at timestamptz, UNIQUE(user_id,operation)
);
CREATE TABLE IF NOT EXISTS planet_handoff_uses (
 handoff_id uuid PRIMARY KEY REFERENCES planet_handoffs,
 visit_id uuid NOT NULL UNIQUE,redeemed_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE planet_visits ALTER COLUMN colony DROP NOT NULL;
ALTER TABLE planet_visits ALTER COLUMN ship DROP NOT NULL;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS visit_id uuid UNIQUE;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS state text NOT NULL DEFAULT 'legacy' CHECK(state IN('legacy','active','disconnected','ended'));
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS generation bigint NOT NULL DEFAULT 0;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS runtime_fence bigint NOT NULL DEFAULT 0;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS connection_epoch bigint NOT NULL DEFAULT 0;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS current_surface bigint REFERENCES planet_surfaces;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS current_sector int;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS revision bigint;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS landing_site bigint REFERENCES planet_landing_sites;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS session_hash text;
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS end_reason text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS planet_characters (
 user_id bigint PRIMARY KEY REFERENCES users,
 last_surface bigint REFERENCES planet_surfaces,last_sector int,last_revision bigint,
 x_q int NOT NULL CHECK(x_q BETWEEN 0 AND 262143),
 y_q int NOT NULL CHECK(y_q BETWEEN 0 AND 262143),position_version bigint NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS planet_live_visits ON planet_visits(current_surface,current_sector) WHERE state IN('active','disconnected');
-- Covers every physical ship writer, including raw SQL paths and future callers.
CREATE OR REPLACE FUNCTION planet_reserved_ship() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE held boolean;
BEGIN
 IF ROW(NEW.star,NEW.object_id,NEW.position_mode,NEW.destination,NEW.local_target,NEW.waypoints)
 IS DISTINCT FROM ROW(OLD.star,OLD.object_id,OLD.position_mode,OLD.destination,OLD.local_target,OLD.waypoints) THEN
  SELECT EXISTS(SELECT 1 FROM planet_transfer_controls WHERE reserved_ship=OLD.id) INTO held;
  IF held THEN RAISE EXCEPTION 'Корабль закреплён за планетарным визитом'; END IF;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS planet_reserved_ship ON ships;
CREATE TRIGGER planet_reserved_ship BEFORE UPDATE ON ships FOR EACH ROW EXECUTE FUNCTION planet_reserved_ship();
UPDATE planet_schema_state SET version=4 WHERE id=1 AND version=3;
-- Narrow account-session lookup: no password hash or wallet fields for planet.
DO $$BEGIN
 IF to_regclass('planet_session_lookup') IS NULL THEN
  EXECUTE 'CREATE VIEW planet_session_lookup AS SELECT s.token_hash,s.user_id,s.expires_at,u.is_bot FROM sessions s JOIN users u ON u.id=s.user_id';
 END IF;
END $$;
ALTER TABLE planet_transfer_controls ADD COLUMN IF NOT EXISTS checked_at timestamptz NOT NULL DEFAULT '-infinity';
ALTER TABLE planet_visits ADD COLUMN IF NOT EXISTS checked_at timestamptz NOT NULL DEFAULT '-infinity';
CREATE INDEX IF NOT EXISTS planet_transfer_check ON planet_transfer_controls(checked_at,user_id) WHERE reserved_ship IS NOT NULL OR revoke_requested_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS planet_visit_check ON planet_visits(checked_at,user_id) WHERE state<>'ended';
DO $$BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='planet_characters'::regclass AND conname='planet_character_sector') THEN
  ALTER TABLE planet_characters ADD CONSTRAINT planet_character_sector FOREIGN KEY(last_surface,last_sector) REFERENCES planet_sectors(surface,number);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='planet_visits'::regclass AND conname='planet_visit_sector') THEN
  ALTER TABLE planet_visits ADD CONSTRAINT planet_visit_sector FOREIGN KEY(current_surface,current_sector) REFERENCES planet_sectors(surface,number);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='planet_visits'::regclass AND conname='planet_active_visit_fields') THEN
  ALTER TABLE planet_visits ADD CONSTRAINT planet_active_visit_fields CHECK(state IN('legacy','ended') OR (visit_id IS NOT NULL AND colony IS NOT NULL AND ship IS NOT NULL AND landing_site IS NOT NULL AND current_surface IS NOT NULL AND current_sector IS NOT NULL AND revision IS NOT NULL AND session_hash IS NOT NULL AND generation>0 AND runtime_fence>0 AND connection_epoch>0));
 END IF;
END$$;
