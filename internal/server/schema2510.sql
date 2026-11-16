-- Player-cleared decor belongs to Planet, separately from immutable geography.
ALTER TABLE planet_sector_buildings ADD COLUMN IF NOT EXISTS removed_objects jsonb NOT NULL DEFAULT '[]'::jsonb;
DO $$BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='planet_removed_objects_valid' AND conrelid='planet_sector_buildings'::regclass) THEN
  ALTER TABLE planet_sector_buildings ADD CONSTRAINT planet_removed_objects_valid
   CHECK(jsonb_typeof(removed_objects)='array' AND jsonb_array_length(removed_objects)<=2048);
 END IF;
END $$;
UPDATE planet_schema_state SET version=10 WHERE id=1 AND version=9;
