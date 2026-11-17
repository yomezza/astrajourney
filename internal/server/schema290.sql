-- The displayed game version remains operator-editable in this single DB row.
-- Apply the release bump only once; ordinary restarts preserve later edits.
INSERT INTO game_release(id,version,migration) VALUES(1,'0.29.0',290)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=clock_timestamp()
 WHERE game_release.migration<excluded.migration;
