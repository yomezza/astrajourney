-- Cosmic-owned additive migration, reviewed against the actual schema.
CREATE TABLE IF NOT EXISTS aj_guide_media (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- User-supplied SVG may be accepted as INPUT, but public file is a re-encoded
  -- safe PNG/WebP. Never serve raw uploaded SVG from the same game origin.
  source_format text NOT NULL CHECK(source_format IN('png','jpeg','webp','svg')),
  served_format text NOT NULL CHECK(served_format IN('png','jpeg','webp')),
  sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
  byte_size bigint NOT NULL CHECK(byte_size BETWEEN 1 AND 8388608),
  pixel_width integer NOT NULL CHECK(pixel_width BETWEEN 1 AND 4096),
  pixel_height integer NOT NULL CHECK(pixel_height BETWEEN 1 AND 4096),
  -- Relative opaque generated object key ONLY, not an arbitrary OS path.
  object_key uuid NOT NULL UNIQUE,
  state text NOT NULL DEFAULT 'ready' CHECK(state IN('pending','ready','quarantined','deleted')),
  alt_ru text NOT NULL DEFAULT '' CHECK(char_length(alt_ru)<=300),
  alt_en text NOT NULL DEFAULT '' CHECK(char_length(alt_en)<=300),
  uploaded_by bigint REFERENCES users(id) ON DELETE SET NULL,
  uploaded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  last_verified_at timestamptz
);
CREATE INDEX IF NOT EXISTS aj_guide_media_by_state
 ON aj_guide_media(state,uploaded_at DESC);

CREATE TABLE IF NOT EXISTS aj_guide_revision_media (
  article_id uuid NOT NULL,
  revision integer NOT NULL,
  locale text NOT NULL CHECK(locale IN('ru','en')),
  position integer NOT NULL CHECK(position>=0),
  role text NOT NULL DEFAULT 'illustration' CHECK(role IN('illustration','hologram','cover')),
  uploaded_media_id uuid REFERENCES aj_guide_media(id) ON DELETE RESTRICT,
  builtin_asset_key text CHECK(builtin_asset_key IS NULL OR
                              builtin_asset_key ~ '^[a-z0-9][a-z0-9/_-]{0,127}$'),
  caption text NOT NULL DEFAULT '' CHECK(char_length(caption)<=500),
  PRIMARY KEY(article_id,revision,locale,position),
  FOREIGN KEY(article_id,revision,locale)
    REFERENCES aj_guide_article_revisions(article_id,revision,locale) ON DELETE RESTRICT,
  CHECK((uploaded_media_id IS NULL) <> (builtin_asset_key IS NULL)),
  CHECK(role <> 'hologram' OR builtin_asset_key IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS aj_guide_revision_media_uploaded
 ON aj_guide_revision_media(uploaded_media_id) WHERE uploaded_media_id IS NOT NULL;

-- IMPORTANT: builtin_asset_key is NOT a filesystem path. Validate it against a
-- versioned server-side allowlist built from trusted game assets; prohibit ../,
-- URL, file://, query parameters and arbitrary URLs in Go. Missing assets give
-- a stable unavailable placeholder, never a fallback to user-provided path.
-- Backing files reside outside webroot, with generated paths derived from
-- object_key and strict fixed extension. Media backups must be coordinated with DB.
-- Deleting an article/revision with media references must be deliberate; SQL RESTRICT
-- prevents silent loss. A GC job may remove unreferenced ready/deleted media only
-- after retention window and checked backup.
-- No path or opaque object key should be exposed to an unauthenticated uploader.

ALTER TABLE aj_guide_media ADD COLUMN IF NOT EXISTS request_hash text NOT NULL DEFAULT '';
ALTER TABLE aj_guide_media ADD COLUMN IF NOT EXISTS last_verified_at timestamptz;
