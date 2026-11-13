-- Cosmic-owned additive migration, reviewed against the actual schema.
CREATE TABLE IF NOT EXISTS aj_planet_return_authorizations (
  user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  operation uuid NOT NULL,
  visit_id uuid NOT NULL,
  generation bigint NOT NULL CHECK(generation>0),
  connection_epoch bigint NOT NULL CHECK(connection_epoch>0),
  runtime_fence bigint NOT NULL CHECK(runtime_fence>0),
  surface_id bigint NOT NULL,
  sector_number integer NOT NULL CHECK(sector_number>0),
  landing_site_id bigint NOT NULL,
  position_x_q integer NOT NULL CHECK(position_x_q BETWEEN 0 AND 262143),
  position_y_q integer NOT NULL CHECK(position_y_q BETWEEN 0 AND 262143),
  distance_q integer NOT NULL CHECK(distance_q>=0),
  state text NOT NULL DEFAULT 'authorized'
    CHECK(state IN('authorized','consumed','expired')),
  authorized_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  PRIMARY KEY(user_id,operation),
  CHECK(expires_at>authorized_at),
  CHECK((state='consumed' AND consumed_at IS NOT NULL)
       OR (state<>'consumed' AND consumed_at IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS aj_planet_return_one_active
 ON aj_planet_return_authorizations(user_id,visit_id)
 WHERE state='authorized';
CREATE INDEX IF NOT EXISTS aj_planet_return_unconsumed_expiry
 ON aj_planet_return_authorizations(expires_at,user_id)
 WHERE state='authorized';

-- Required normal-return protocol (implementation in Go):
-- Planet WS "return.prepare": authoritative current server position + landing
-- object, correct active visit, same sector, near threshold and action UUID;
-- DB transaction INSERT authorization (e.g. 30s) with full fence fields, COMMIT.
-- Cosmic POST /api/planet/return: start existing global/player locking;
-- SELECT auth FOR UPDATE by user+operation, reject expired/fence mismatch/not active;
-- mark consumed once; set revoke/pending through existing transfer code; COMMIT.
-- Planet then safely ends active visit via existing runtime/lease machinery.
-- Duplicate operation returns stored/status result, never reissues authorization.
-- Emergency return must be a separate audited policy, NOT INSERT fabricated
-- "authorized" evidence or a normal user-controlled bypass.
-- On old data, this table remains EMPTY; migration alone changes no behavior.

ALTER TABLE aj_planet_return_authorizations ADD COLUMN IF NOT EXISTS position_version bigint NOT NULL DEFAULT 1 CHECK(position_version>0);
UPDATE planet_schema_state SET version=8 WHERE id=1 AND version=7;

-- Planet writes only verified scene failures; Cosmic may use the bound,
-- short-lived diagnosis for audited emergency recovery, never as a normal proof.
CREATE TABLE IF NOT EXISTS aj_planet_recovery_diagnostics (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 visit_id uuid NOT NULL,
 generation bigint NOT NULL,
 connection_epoch bigint NOT NULL,
 runtime_fence bigint NOT NULL,
 reason text NOT NULL CHECK(reason IN('scene_corrupt','scene_source_changed')),
 diagnosed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 PRIMARY KEY(user_id,visit_id),
 CHECK(expires_at>diagnosed_at)
);
