-- REVIEW ONLY. Astra Journey, audited main 470667919c06a871abd909dcd49213b9c67ceec4.
-- Proposed future Cosmic schema250+ fragment; NOT registered in initialization/database.go.
-- Run only with search_path set to the selected isolated TEST DB_SCHEMA by Cosmic migration runner.
-- Preflight: BACK UP galaxies.data; verify every star ID and planet ID, duplicates, founder_key linkage.
-- This migration is additive and does NOT mutate galaxies JSON or grant discovery to historical players.
-- Application MUST atomically update this registry and galaxies.data through World.Commit for renames.
-- Historical backfill is deliberately conservative; human review may refine before adoption.
-- IMPORTANT: on a BRAND-NEW DB, OpenDatabase runs schema upgrades BEFORE it
-- inserts the initial galaxies JSON. The backfill below then inserts ZERO rows.
-- Future Go integration MUST seed this registry atomically after initial world
-- generation (and after adding any new stars/planets), otherwise no claims work.

CREATE TABLE IF NOT EXISTS aj_discovery_objects (
  kind text NOT NULL CHECK(kind IN('system','planet')),
  star_id integer NOT NULL CHECK(star_id > 0),
  planet_id integer NOT NULL DEFAULT 0 CHECK(planet_id >= 0),
  galaxy_key text NOT NULL CHECK(char_length(galaxy_key) BETWEEN 1 AND 80),
  catalog_code text NOT NULL CHECK(char_length(catalog_code) BETWEEN 1 AND 160),
  original_name text NOT NULL CHECK(char_length(original_name) BETWEEN 1 AND 120),
  current_name text NOT NULL CHECK(char_length(current_name) BETWEEN 1 AND 120),
  -- Go supplies a Unicode NFKC+casefold+space-collapsed canonical key on new writes;
  -- SQL bootstrap uses lower(trim) only. The migration integration must reconcile seed keys.
  name_key text NOT NULL CHECK(char_length(name_key) BETWEEN 1 AND 240),
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
  CHECK (rename_used_at IS NULL OR claim_state='claimed'),
  CHECK (claim_state NOT IN('unclaimed','legacy_unattributed','protected','retired')
         OR rename_used_at IS NULL),
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
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(kind,star_id,planet_id)
    REFERENCES aj_discovery_objects(kind,star_id,planet_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS aj_discovery_history_by_object
 ON aj_discovery_name_history(kind,star_id,planet_id,changed_at DESC,id DESC);

-- Snapshot of legacy objects, IDs from the actual galaxies JSON.
-- This is NOT an attempt to infer historical first discoverers.
WITH stars AS (
  SELECT x.value AS s,
         (x.value->>'id')::integer AS sid,
         COALESCE(NULLIF(x.value->>'galaxy',''),'edem') AS galaxy_key
  FROM galaxies g
  CROSS JOIN LATERAL jsonb_array_elements(g.data->'stars') AS x(value)
  WHERE g.name='Edem'
)
INSERT INTO aj_discovery_objects
 (kind,star_id,planet_id,galaxy_key,catalog_code,original_name,current_name,
  name_key,claim_state,protected_founder_key)
SELECT 'system',sid,0,lower(galaxy_key),
       lower(galaxy_key)||':star-'||sid::text,
       s->>'name',s->>'name',lower(btrim(s->>'name')),
       CASE WHEN COALESCE((s->>'deleted')::boolean,false) THEN 'retired'
            WHEN COALESCE(s->>'founder','')<>'' THEN 'protected'
            WHEN EXISTS(SELECT 1 FROM surveys v WHERE v.star=sid) THEN 'legacy_unattributed'
            ELSE 'unclaimed' END,
       NULLIF(s->>'founder','')
FROM stars
ON CONFLICT(kind,star_id,planet_id) DO NOTHING;

WITH stars AS (
  SELECT x.value AS s,
         (x.value->>'id')::integer AS sid,
         COALESCE(NULLIF(x.value->>'galaxy',''),'edem') AS galaxy_key
  FROM galaxies g
  CROSS JOIN LATERAL jsonb_array_elements(g.data->'stars') AS x(value)
  WHERE g.name='Edem'
), planets AS (
  SELECT stars.*, p.value AS planet, (p.value->>'id')::integer AS pid
  FROM stars CROSS JOIN LATERAL jsonb_array_elements(COALESCE(s->'planets','[]'::jsonb)) AS p(value)
)
INSERT INTO aj_discovery_objects
 (kind,star_id,planet_id,galaxy_key,catalog_code,original_name,current_name,
  name_key,claim_state,protected_founder_key)
SELECT 'planet',sid,pid,lower(galaxy_key),
       lower(galaxy_key)||':star-'||sid::text||':planet-'||pid::text,
       planet->>'name',planet->>'name',lower(btrim(planet->>'name')),
       CASE WHEN COALESCE((s->>'deleted')::boolean,false) THEN 'retired'
            WHEN COALESCE(s->>'founder','')<>'' THEN 'protected'
            WHEN EXISTS(SELECT 1 FROM surveys v WHERE v.star=sid) THEN 'legacy_unattributed'
            WHEN EXISTS(SELECT 1 FROM research_jobs j WHERE j.star=sid AND j.scanned>=pid) THEN 'legacy_unattributed'
            WHEN EXISTS(SELECT 1 FROM planet_surfaces ps WHERE ps.star=sid AND ps.planet=pid) THEN 'legacy_unattributed'
            ELSE 'unclaimed' END,
       NULLIF(s->>'founder','')
FROM planets
ON CONFLICT(kind,star_id,planet_id) DO NOTHING;

-- Important: unprocessed planets in an incomplete research job remain unclaimed.
-- Companion checks:
-- 1) SELECT galaxy_key,name_key,count(*) FROM aj_discovery_objects WHERE kind='system'
--    GROUP BY 1,2 HAVING count(*)>1;
-- 2) SELECT star_id,name_key,count(*) FROM aj_discovery_objects WHERE kind='planet'
--    GROUP BY 1,2 HAVING count(*)>1;
-- 3) SELECT DISTINCT protected_founder_key FROM aj_discovery_objects
--    WHERE protected_founder_key IS NOT NULL AND NOT EXISTS
--      (SELECT 1 FROM companies c WHERE c.founder AND c.founder_key=protected_founder_key);
-- 4) Verify JSON star_id == global Stars index and planet IDs match SystemSites order.
-- No rollback with DROP TABLE after player claims are created. Restore from backup if necessary.
