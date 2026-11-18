-- Release metadata only. The common UI version remains database-owned;
-- operator edits after this release migration survive ordinary restarts.
INSERT INTO game_release(id,version,migration) VALUES(1,'0.30.1',301)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=clock_timestamp()
 WHERE game_release.migration<excluded.migration;
