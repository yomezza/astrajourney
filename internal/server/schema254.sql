-- Cosmic-owned additive migration, reviewed against the actual schema.
CREATE TABLE IF NOT EXISTS aj_colony_names (
  colony_id bigint PRIMARY KEY,
  surface_id bigint NOT NULL,
  sector_number integer NOT NULL,
  -- Go enforces 3–60 graphemes, including combining Unicode marks.
  current_name text NOT NULL CHECK(char_length(btrim(current_name)) BETWEEN 3 AND 1024),
  name_key text NOT NULL CHECK(char_length(name_key)>0),
  revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
  last_player_rename_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(colony_id,surface_id,sector_number)
    REFERENCES planet_colonies(id,surface,sector) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS aj_colony_name_unique_on_surface
 ON aj_colony_names(surface_id,name_key);

CREATE TABLE IF NOT EXISTS aj_colony_name_history (
  id bigserial PRIMARY KEY,
  operation_id uuid NOT NULL UNIQUE,
  colony_id bigint NOT NULL REFERENCES aj_colony_names(colony_id) ON DELETE RESTRICT,
  actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
  actor_source text NOT NULL CHECK(actor_source IN('personal_owner','corporate_manager','admin')),
  old_name text NOT NULL,
  new_name text NOT NULL,
  request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
  reason text NOT NULL DEFAULT '' CHECK(char_length(reason)<=400),
  result jsonb NOT NULL,
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS aj_colony_history_recent
 ON aj_colony_name_history(colony_id,changed_at DESC,id DESC);

-- planet_projects has no name column in the actual game schema. Without a name
-- row, the stable default label is 'Колония <colony_id>'. Rename never edits a project.
-- First rename inserts a registry row and history under the colony row lock.
-- A new colony created at the same (surface,sector) receives a NEW colony_id.
-- Any old history remains tied to old colony_id; ON DELETE RESTRICT forces
-- an explicit archive/retention decision before deleting a named colony.
-- Ownership authorization stays in planet_colonies owner_user/owner_company
-- and CompanyAccess(permission='colonize') at actual commit time.
-- If a colony can MOVE surface/sector, rename writer must update these FK
-- columns atomically with the owner entity and re-check name uniqueness.
-- The approved 24h player cooldown is enforced by the Go transaction; admin
-- correction preserves the last player timestamp and requires an audit reason.
DROP TRIGGER IF EXISTS aj_colony_history_immutable ON aj_colony_name_history;
CREATE TRIGGER aj_colony_history_immutable BEFORE UPDATE OR DELETE ON aj_colony_name_history FOR EACH ROW EXECUTE FUNCTION aj_name_history_immutable();
