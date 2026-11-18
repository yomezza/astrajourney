-- Release metadata only; later operator edits survive ordinary restarts.
INSERT INTO game_release(id,version,migration) VALUES(1,'0.30.2',302)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=clock_timestamp()
 WHERE game_release.migration<excluded.migration;
