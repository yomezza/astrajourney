-- Cosmic-owned discovery registry. Seeded by discovery.Sync after world creation.
CREATE TABLE IF NOT EXISTS aj_discovery_objects (
  kind text NOT NULL CHECK(kind IN('system','planet')),
  star_id integer NOT NULL CHECK(star_id > 0),
  planet_id integer NOT NULL DEFAULT 0 CHECK(planet_id >= 0),
  galaxy_key text NOT NULL CHECK(char_length(galaxy_key) BETWEEN 1 AND 80),
  catalog_code text NOT NULL CHECK(char_length(catalog_code) BETWEEN 1 AND 160),
  original_name text NOT NULL CHECK(char_length(original_name) BETWEEN 1 AND 1024),
  current_name text NOT NULL CHECK(char_length(current_name) BETWEEN 1 AND 1024),
  -- Go supplies a Unicode NFKC+casefold+space-collapsed canonical key on new writes;
  -- Post-world seed uses the same Go normalizer; no SQL lower(trim) approximation.
  name_key text NOT NULL CHECK(char_length(name_key) BETWEEN 1 AND 2048),
  claim_state text NOT NULL CHECK(claim_state IN
    ('unclaimed','legacy_unattributed','claimed','protected','retired')),
  discoverer_id bigint REFERENCES users(id) ON DELETE SET NULL,
  discovered_at timestamptz,
  claim_source text CHECK(claim_source IS NULL OR claim_source IN('survey','planet_scan','admin_verified')),
  protected_founder_key text,
  rename_used_at timestamptz,
  name_revision bigint NOT NULL DEFAULT 0 CHECK(name_revision >= 0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(kind,star_id,planet_id),
  CHECK ((kind='system' AND planet_id=0) OR (kind='planet' AND planet_id>0)),
  CHECK (claim_state<>'claimed' OR discovered_at IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS aj_discovery_by_discoverer
 ON aj_discovery_objects(discoverer_id,discovered_at DESC)
 WHERE discoverer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS aj_discovery_by_state
 ON aj_discovery_objects(kind,claim_state,star_id,planet_id);

-- Names are unique only inside the corresponding scope:
-- systems within their galaxy, planets within their star.
-- If existing generated names collide, migration FAILS. Run duplicate preflight first.
CREATE UNIQUE INDEX IF NOT EXISTS aj_discovery_system_name_unique
 ON aj_discovery_objects(galaxy_key,name_key)
 WHERE kind='system' AND claim_state<>'retired';
CREATE UNIQUE INDEX IF NOT EXISTS aj_discovery_planet_name_unique
 ON aj_discovery_objects(star_id,name_key)
 WHERE kind='planet' AND claim_state<>'retired';

CREATE TABLE IF NOT EXISTS aj_discovery_name_history (
  id bigserial PRIMARY KEY,
  operation_id uuid NOT NULL UNIQUE,
  kind text NOT NULL,
  star_id integer NOT NULL,
  planet_id integer NOT NULL,
  actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
  actor_source text NOT NULL CHECK(actor_source IN('discoverer','admin')),
  old_name text NOT NULL,
  new_name text NOT NULL,
  request_hash text NOT NULL CHECK(char_length(request_hash)=64),
  reason text NOT NULL DEFAULT '' CHECK(char_length(reason)<=400),
  result jsonb NOT NULL,
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(kind,star_id,planet_id)
    REFERENCES aj_discovery_objects(kind,star_id,planet_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS aj_discovery_history_by_object
 ON aj_discovery_name_history(kind,star_id,planet_id,changed_at DESC,id DESC);


CREATE OR REPLACE FUNCTION aj_discovery_entitlement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.rename_used_at IS NOT NULL AND NEW.rename_used_at IS DISTINCT FROM OLD.rename_used_at THEN RAISE EXCEPTION 'rename entitlement cannot be reset'; END IF;
 IF OLD.discovered_at IS NOT NULL AND (NEW.discoverer_id IS DISTINCT FROM OLD.discoverer_id AND NEW.discoverer_id IS NOT NULL OR NEW.discovered_at IS DISTINCT FROM OLD.discovered_at) THEN RAISE EXCEPTION 'first discovery is immutable'; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS aj_discovery_entitlement ON aj_discovery_objects;
CREATE TRIGGER aj_discovery_entitlement BEFORE UPDATE ON aj_discovery_objects FOR EACH ROW EXECUTE FUNCTION aj_discovery_entitlement_guard();

CREATE OR REPLACE FUNCTION aj_name_history_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'name history is immutable'; END IF;
 IF (to_jsonb(NEW)-'actor_id') IS DISTINCT FROM (to_jsonb(OLD)-'actor_id')
 OR (NEW.actor_id IS NOT NULL AND NEW.actor_id IS DISTINCT FROM OLD.actor_id) THEN
  RAISE EXCEPTION 'name history is immutable';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS aj_discovery_history_immutable ON aj_discovery_name_history;
CREATE TRIGGER aj_discovery_history_immutable BEFORE UPDATE OR DELETE ON aj_discovery_name_history FOR EACH ROW EXECUTE FUNCTION aj_name_history_immutable();
