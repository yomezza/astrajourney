-- REVIEW ONLY: colony display names separate from planet_projects project names.
-- Existing planet_colonies(id,project,surface,sector,owner_user,owner_company).
-- Not registered in Cosmic migrations and not run on production.

CREATE TABLE IF NOT EXISTS aj_colony_names (
  colony_id bigint PRIMARY KEY REFERENCES planet_colonies(id) ON DELETE RESTRICT,
  current_name text NOT NULL CHECK(char_length(btrim(current_name)) BETWEEN 3 AND 120),
  name_key text NOT NULL CHECK(char_length(name_key)>0),
  revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
  last_player_rename_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX IF NOT EXISTS aj_colony_name_by_surface
 ON aj_colony_names(name_key,colony_id);

CREATE TABLE IF NOT EXISTS aj_colony_name_history (
  id bigserial PRIMARY KEY,
  operation_id uuid NOT NULL UNIQUE,
  colony_id bigint NOT NULL REFERENCES aj_colony_names(colony_id) ON DELETE RESTRICT,
  actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
  actor_source text NOT NULL CHECK(actor_source IN('personal_owner','corporate_manager','admin')),
  old_name text NOT NULL,
  new_name text NOT NULL,
  request_hash text NOT NULL CHECK(char_length(request_hash)=64),
  reason text NOT NULL DEFAULT '' CHECK(char_length(reason)<=400),
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS aj_colony_history_recent
 ON aj_colony_name_history(colony_id,changed_at DESC,id DESC);

-- IMPORTANT: unique display names within a surface require a JOIN from
-- aj_colony_names to planet_colonies.surface. A SQL UNIQUE index cannot use that JOIN.
-- Before publication either (a) keep no name uniqueness rule, showing colony ID
-- for disambiguation, or (b) denormalize immutable surface_id into aj_colony_names,
-- add composite FK to planet_colonies(id,surface,sector) and UNIQUE(surface_id,name_key)
-- with transactional maintenance on reset/relocation. The architecture recommends (b)
-- after DBA review; current draft has no false uniqueness guarantee.
-- Default display text is read from planet_projects.name if aj_colony_names row absent.
-- INSERT on first rename, not a write to planet_projects.
-- Server authorizes via current planet_colonies.owner_user/owner_company and
-- CompanyAccess(permission='colonize') or admin, not historical owner snapshot.
-- On transfer of ownership, history persists, rights derive from current row.
-- Suggested 24h user cooldown; atomic compare-and-set plus operation UUID.
-- Deleted/retired colonies keep archived history in a revised retention strategy;
-- ON DELETE RESTRICT here deliberately blocks accidental cascading data loss.
