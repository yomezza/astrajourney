ALTER TABLE player_ideas ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ;

INSERT INTO game_release(id,version,migration) VALUES(1,'0.31.6',316)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=now()
 WHERE game_release.migration<excluded.migration;
