-- REVIEW ONLY: colony names are distinct from planet_projects.name.
-- Depends on planet_colonies(id,surface,sector) unique index in schema160.sql.
-- NOT executed or integrated with Cosmic migration runner.

CREATE TABLE IF NOT EXISTS aj_colony_names (
  colony_id bigint PRIMARY KEY,
  surface_id bigint NOT NULL,
  sector_number integer NOT NULL,
  current_name text NOT NULL CHECK(char_length(btrim(current_name)) BETWEEN 3 AND 120),
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
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS aj_colony_history_recent
 ON aj_colony_name_history(colony_id,changed_at DESC,id DESC);

-- Default label when aj_colony_names row absent is planet_projects.name.
-- First rename inserts the registry row with OLD project label under colony row lock,
-- then performs one UPDATE and append-only history operation in the same SQL tx.
-- A new colony created at the same (surface,sector) receives a NEW colony_id.
-- Any old history remains tied to old colony_id; ON DELETE RESTRICT forces
-- an explicit archive/retention decision before deleting a named colony.
-- Ownership authorization stays in planet_colonies owner_user/owner_company
-- and CompanyAccess(permission='colonize') at actual commit time.
-- If a colony can MOVE surface/sector, rename writer must update these FK
-- columns atomically with the owner entity and re-check name uniqueness.
-- 24h user cooldown is a proposal, not a DB policy yet.
