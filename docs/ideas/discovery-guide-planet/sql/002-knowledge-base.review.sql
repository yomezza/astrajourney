-- REVIEW ONLY. PostgreSQL editables for Pilot Guide Original C.
-- Proposed future Cosmic schema250+ fragment. No automatic migration hook and no writes to production.

CREATE TABLE IF NOT EXISTS aj_guide_categories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  parent_id uuid REFERENCES aj_guide_categories(id) ON DELETE RESTRICT,
  slug text NOT NULL UNIQUE CHECK(slug ~ '^[a-z0-9][a-z0-9-]{0,79}$'),
  sort_order integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'visible' CHECK(status IN('visible','hidden','archived')),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK(parent_id IS NULL OR parent_id<>id)
);
CREATE INDEX IF NOT EXISTS aj_guide_category_children
 ON aj_guide_categories(parent_id,sort_order,id);

CREATE TABLE IF NOT EXISTS aj_guide_category_i18n (
  category_id uuid NOT NULL REFERENCES aj_guide_categories(id) ON DELETE CASCADE,
  locale text NOT NULL CHECK(locale IN('ru','en')),
  title text NOT NULL CHECK(char_length(btrim(title)) BETWEEN 1 AND 100),
  description text NOT NULL DEFAULT '' CHECK(char_length(description)<=1000),
  PRIMARY KEY(category_id,locale)
);

CREATE TABLE IF NOT EXISTS aj_guide_articles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  category_id uuid NOT NULL REFERENCES aj_guide_categories(id) ON DELETE RESTRICT,
  slug text NOT NULL UNIQUE CHECK(slug ~ '^[a-z0-9][a-z0-9-]{0,100}$'),
  sort_order integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'active' CHECK(status IN('active','hidden','archived')),
  created_by bigint REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS aj_guide_article_category
 ON aj_guide_articles(category_id,status,sort_order,id);

-- Each saved article revision is immutable. One revision number may have both locales,
-- but publication points to the exact (article_id, revision, locale).
CREATE TABLE IF NOT EXISTS aj_guide_article_revisions (
  article_id uuid NOT NULL REFERENCES aj_guide_articles(id) ON DELETE RESTRICT,
  revision integer NOT NULL CHECK(revision>0),
  locale text NOT NULL CHECK(locale IN('ru','en')),
  title text NOT NULL CHECK(char_length(btrim(title)) BETWEEN 1 AND 180),
  summary text NOT NULL DEFAULT '' CHECK(char_length(summary)<=1200),
  markdown_source text NOT NULL CHECK(octet_length(markdown_source)<=262144),
  editor_id bigint REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(article_id,revision,locale)
);
CREATE INDEX IF NOT EXISTS aj_guide_revision_newest
 ON aj_guide_article_revisions(article_id,locale,revision DESC);

CREATE TABLE IF NOT EXISTS aj_guide_publications (
  article_id uuid NOT NULL,
  locale text NOT NULL CHECK(locale IN('ru','en')),
  revision integer NOT NULL CHECK(revision>0),
  published_by bigint REFERENCES users(id) ON DELETE SET NULL,
  published_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(article_id,locale),
  FOREIGN KEY(article_id,revision,locale)
    REFERENCES aj_guide_article_revisions(article_id,revision,locale) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS aj_guide_publications_time
 ON aj_guide_publications(published_at DESC);

-- For audit even after unpublishing. Backend appends rows, no UPDATE/DELETE.
CREATE TABLE IF NOT EXISTS aj_guide_publication_history (
  id bigserial PRIMARY KEY,
  article_id uuid NOT NULL REFERENCES aj_guide_articles(id) ON DELETE RESTRICT,
  locale text NOT NULL CHECK(locale IN('ru','en')),
  revision integer,
  operation text NOT NULL CHECK(operation IN('publish','unpublish','archive','restore')),
  actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
  happened_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK((operation='publish' AND revision IS NOT NULL) OR operation<>'publish')
);
CREATE INDEX IF NOT EXISTS aj_guide_pub_history_by_article
 ON aj_guide_publication_history(article_id,locale,happened_at DESC);

CREATE TABLE IF NOT EXISTS aj_guide_related (
  article_id uuid NOT NULL REFERENCES aj_guide_articles(id) ON DELETE RESTRICT,
  related_id uuid NOT NULL REFERENCES aj_guide_articles(id) ON DELETE RESTRICT,
  position integer NOT NULL DEFAULT 0,
  PRIMARY KEY(article_id,related_id),
  CHECK(article_id<>related_id)
);
CREATE INDEX IF NOT EXISTS aj_guide_related_inverse ON aj_guide_related(related_id,article_id);

-- Nested categories are optional. The DB prevents direct self-parenting and cycles
-- through this bounded recursive guard; accompanying Go admin writes must serialize
-- structural edits and test concurrent reparenting. No cascaded article deletion.
CREATE OR REPLACE FUNCTION aj_guide_category_no_cycle() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE seen uuid;
BEGIN
  IF NEW.parent_id IS NULL THEN RETURN NEW; END IF;
  IF NEW.parent_id = NEW.id THEN RAISE EXCEPTION 'guide category self parent'; END IF;
  -- Structural changes are serialized with a dedicated transaction advisory lock.
  PERFORM pg_advisory_xact_lock(792503);
  WITH RECURSIVE ancestors(id,parent_id) AS (
    SELECT id,parent_id FROM aj_guide_categories WHERE id=NEW.parent_id
    UNION ALL
    SELECT c.id,c.parent_id FROM aj_guide_categories c JOIN ancestors a ON c.id=a.parent_id
  ) SELECT id INTO seen FROM ancestors WHERE id=NEW.id LIMIT 1;
  IF seen IS NOT NULL THEN RAISE EXCEPTION 'guide category cycle'; END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS aj_guide_category_cycle_guard ON aj_guide_categories;
CREATE TRIGGER aj_guide_category_cycle_guard
 BEFORE INSERT OR UPDATE OF parent_id ON aj_guide_categories
 FOR EACH ROW EXECUTE FUNCTION aj_guide_category_no_cycle();

-- REVIEW: For strict immutability, application should have INSERT/SELECT only on
-- revisions/history and no UPDATE/DELETE; migration/DBA roles hold DDL.
-- Depth <=3 and article visibility/translation selection are Go/API rules.
-- Category deletion is RESTRICT: reparent/move articles first, then archive/delete.
