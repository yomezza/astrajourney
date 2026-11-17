-- Owned by Cosmic. IDs used by ships/jobs/colonies are retained unchanged.
CREATE TABLE IF NOT EXISTS universe_state (
 id smallint PRIMARY KEY CHECK(id=1), format_version int NOT NULL DEFAULT 1, storage_version bigint NOT NULL DEFAULT 1,
 name text NOT NULL, seed bigint NOT NULL, generator_version int NOT NULL,
 revision bigint NOT NULL, discovery_secret text NOT NULL,
 settlement_version int NOT NULL, ring_layout_version int NOT NULL, naming_version int NOT NULL,
 migrated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 legacy_sha256 text, details jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE IF NOT EXISTS galaxy_catalog (
 id text PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 80), ordinal int NOT NULL UNIQUE,
 name text NOT NULL, shape text NOT NULL, seed bigint NOT NULL,
 opened boolean NOT NULL, deleted boolean NOT NULL, region_base int NOT NULL, radius double precision NOT NULL,
 extra_lanes int NOT NULL, cross_links int NOT NULL, max_lane_length int NOT NULL,
 station_percent int NOT NULL, max_ports int NOT NULL,
 resource_weights jsonb, territories jsonb
);
CREATE TABLE IF NOT EXISTS universe_regions (ordinal int PRIMARY KEY, id int NOT NULL UNIQUE, name text NOT NULL, specialty text NOT NULL);
-- A global identity registry provides UNIQUE UUID/global integer IDs even though
-- PostgreSQL partitioned unique constraints must include the partition key.
CREATE TABLE IF NOT EXISTS system_identity (
 id integer PRIMARY KEY CHECK(id>0), object_uuid uuid NOT NULL UNIQUE,
 galaxy_id text NOT NULL REFERENCES galaxy_catalog(id), UNIQUE(galaxy_id,id)
);
CREATE TABLE IF NOT EXISTS star_systems (
 galaxy_id text NOT NULL, id integer NOT NULL, local_number int NOT NULL,
 name text NOT NULL, catalog_code text NOT NULL, x double precision NOT NULL, y double precision NOT NULL,
 region int NOT NULL, class text NOT NULL, deleted boolean NOT NULL,
 mass double precision NOT NULL, luminosity double precision NOT NULL, founder text NOT NULL,
 ports int, details jsonb NOT NULL CHECK(jsonb_typeof(details)='object'),
 PRIMARY KEY(galaxy_id,id), FOREIGN KEY(galaxy_id,id) REFERENCES system_identity(galaxy_id,id)
) PARTITION BY LIST(galaxy_id);
CREATE INDEX IF NOT EXISTS star_systems_active_region ON star_systems(galaxy_id,region,id) WHERE NOT deleted;
CREATE UNIQUE INDEX IF NOT EXISTS star_systems_active_number ON star_systems(galaxy_id,local_number) WHERE NOT deleted AND local_number>0;
CREATE TABLE IF NOT EXISTS system_planets (
 system_id int NOT NULL REFERENCES system_identity(id), id int NOT NULL CHECK(id>0),
 object_uuid uuid NOT NULL UNIQUE, ordinal int NOT NULL, name text NOT NULL, catalog_code text NOT NULL,
 kind text NOT NULL, orbit_au double precision NOT NULL, mass double precision NOT NULL,
 gravity double precision NOT NULL, temperature int NOT NULL,
 details jsonb NOT NULL CHECK(jsonb_typeof(details)='object'),
 PRIMARY KEY(system_id,id), UNIQUE(system_id,ordinal)
);
CREATE TABLE IF NOT EXISTS universe_lanes (
 ordinal int PRIMARY KEY, from_system int NOT NULL REFERENCES system_identity(id),
 to_system int NOT NULL REFERENCES system_identity(id), CHECK(from_system<>to_system),
 UNIQUE(from_system,to_system)
);
CREATE INDEX IF NOT EXISTS universe_lanes_from ON universe_lanes(from_system);
CREATE INDEX IF NOT EXISTS universe_lanes_to ON universe_lanes(to_system);
-- An old executable must fail instead of silently writing the stale archive.
CREATE OR REPLACE FUNCTION universe_legacy_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM universe_state WHERE id=1) THEN
  RAISE EXCEPTION 'legacy galaxies JSON is read-only after tabular migration; use the matching Cosmic release';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
DO $$BEGIN
 IF to_regclass('galaxies') IS NOT NULL THEN
  DROP TRIGGER IF EXISTS universe_legacy_read_only ON galaxies;
  CREATE TRIGGER universe_legacy_read_only BEFORE INSERT OR UPDATE OR DELETE ON galaxies FOR EACH ROW EXECUTE FUNCTION universe_legacy_write_guard();
 END IF;
END $$;
CREATE OR REPLACE FUNCTION universe_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (TG_TABLE_NAME='system_identity' AND (NEW.id<>OLD.id OR NEW.object_uuid<>OLD.object_uuid OR NEW.galaxy_id<>OLD.galaxy_id)) THEN
  RAISE EXCEPTION 'system identity is immutable';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS universe_system_identity_immutable ON system_identity;
CREATE TRIGGER universe_system_identity_immutable BEFORE UPDATE ON system_identity FOR EACH ROW EXECUTE FUNCTION universe_identity_guard();
CREATE OR REPLACE FUNCTION universe_planet_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.system_id<>OLD.system_id OR NEW.id<>OLD.id OR NEW.object_uuid<>OLD.object_uuid THEN RAISE EXCEPTION 'planet identity is immutable'; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS universe_planet_identity_immutable ON system_planets;
CREATE TRIGGER universe_planet_identity_immutable BEFORE UPDATE ON system_planets FOR EACH ROW EXECUTE FUNCTION universe_planet_identity_guard();

CREATE TABLE IF NOT EXISTS system_objects (
 system_id int NOT NULL REFERENCES system_identity(id),
 id int NOT NULL CHECK(id IN(101,102)), object_uuid uuid NOT NULL UNIQUE,
 kind text NOT NULL CHECK((id=101 AND kind='asteroid') OR (id=102 AND kind='ice')),
 name text NOT NULL, PRIMARY KEY(system_id,id)
);
COMMENT ON TABLE system_objects IS 'Природные поля системы с постоянными именами и UUID; без права переименования игроком';
COMMENT ON COLUMN system_objects.system_id IS 'Идентификатор звёздной системы';
COMMENT ON COLUMN system_objects.id IS 'Постоянный локатор поля: 101 астероиды, 102 лёд';
COMMENT ON COLUMN system_objects.object_uuid IS 'Постоянный UUID поля';
COMMENT ON COLUMN system_objects.kind IS 'Тип природного поля';
COMMENT ON COLUMN system_objects.name IS 'Сохранённое имя из словаря';

ALTER TABLE galaxy_catalog ADD COLUMN IF NOT EXISTS empty boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN galaxy_catalog.empty IS 'Создавать природные системы без основателей и станций';
