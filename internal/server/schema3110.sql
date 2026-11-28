CREATE TABLE IF NOT EXISTS player_idea_categories (
 id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
 name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
 active BOOLEAN NOT NULL DEFAULT true
);
CREATE UNIQUE INDEX IF NOT EXISTS player_idea_categories_name ON player_idea_categories (lower(name COLLATE pg_catalog.pg_c_utf8));
INSERT INTO player_idea_categories(id,name) SELECT * FROM (VALUES
 ('gameplay','Игровой процесс'),('interface','Интерфейс'),('economy','Экономика'),('other','Другое')) AS seed(id,name)
WHERE NOT EXISTS (SELECT 1 FROM player_idea_categories) ON CONFLICT DO NOTHING;
ALTER TABLE player_ideas DROP CONSTRAINT IF EXISTS player_ideas_category_check;
DO $$ BEGIN
 ALTER TABLE player_ideas ADD CONSTRAINT player_ideas_category_fk FOREIGN KEY(category) REFERENCES player_idea_categories(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

INSERT INTO game_release(id,version,migration) VALUES(1,'0.31.10',3110)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=now()
 WHERE game_release.migration<excluded.migration;
