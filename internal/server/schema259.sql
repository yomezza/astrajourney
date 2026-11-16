-- Planet writes placed objects; Cosmos remains the only migration/geography writer.
CREATE TABLE IF NOT EXISTS planet_sector_buildings (
 surface bigint NOT NULL, sector integer NOT NULL, revision bigint NOT NULL,
 version bigint NOT NULL DEFAULT 0 CHECK(version>=0),
 objects jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(objects)='array' AND jsonb_array_length(objects)<=256),
 PRIMARY KEY(surface,sector,revision),
 FOREIGN KEY(surface,sector,revision) REFERENCES planet_local_locations(surface,sector,revision) ON DELETE CASCADE
);
UPDATE planet_schema_state SET version=9 WHERE id=1 AND version=8;
CREATE INDEX IF NOT EXISTS planet_characters_build_positions
 ON planet_characters(last_surface,last_sector,last_revision) WHERE last_surface IS NOT NULL;
