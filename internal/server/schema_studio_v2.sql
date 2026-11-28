ALTER TABLE pilot_workspace_preferences
 ADD COLUMN IF NOT EXISTS layout_v2 jsonb
 CHECK (layout_v2 IS NULL OR (jsonb_typeof(layout_v2) = 'object' AND octet_length(layout_v2::text) <= 131072));

CREATE TABLE IF NOT EXISTS pilot_studio_commands (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 request_id text NOT NULL CHECK (length(request_id) BETWEEN 16 AND 64),
 digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (user_id, request_id)
);

-- Old Cosmic binaries delete UUIDs absent from V1. Keep V2 UUID claims
-- through a binary rollback while still allowing account deletion to cascade.
CREATE OR REPLACE FUNCTION preserve_studio_v2_uuid() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM pilot_workspace_preferences p
  CROSS JOIN LATERAL jsonb_array_elements(p.layout_v2->'workspaces') AS space(w)
  WHERE p.user_id=OLD.user_id AND
   (w->>'id'=OLD.id::text OR EXISTS (
    SELECT 1 FROM jsonb_array_elements(w->'modules') AS module(m) WHERE m->>'id'=OLD.id::text
   ))
 ) THEN
  RETURN NULL;
 END IF;
 RETURN OLD;
END;
$$;
DROP TRIGGER IF EXISTS preserve_studio_v2_uuid ON pilot_workspace_ids;
CREATE TRIGGER preserve_studio_v2_uuid BEFORE DELETE ON pilot_workspace_ids
 FOR EACH ROW EXECUTE FUNCTION preserve_studio_v2_uuid();
