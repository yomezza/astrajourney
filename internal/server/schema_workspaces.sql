CREATE TABLE IF NOT EXISTS pilot_workspace_preferences (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 layout jsonb NOT NULL CHECK (jsonb_typeof(layout) = 'object' AND octet_length(layout::text) <= 131072)
);

CREATE TABLE IF NOT EXISTS pilot_workspace_ids (
 id uuid PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES pilot_workspace_preferences(user_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS pilot_workspace_ids_owner ON pilot_workspace_ids(user_id);

INSERT INTO game_release(id,version,migration) VALUES(1,'0.31.3',313)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=now()
 WHERE game_release.migration<excluded.migration;
