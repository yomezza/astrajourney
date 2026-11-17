-- Release metadata is stored in the database. This migration updates it once,
-- so a deliberate operator edit survives ordinary process restarts.
CREATE TABLE IF NOT EXISTS game_release (
 id smallint PRIMARY KEY CHECK(id=1),
 version text NOT NULL CHECK(version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'),
 migration integer NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
COMMENT ON TABLE game_release IS 'Текущая версия игры для всех страниц клиента';
COMMENT ON COLUMN game_release.id IS 'Единственная запись установки';
COMMENT ON COLUMN game_release.version IS 'Версия игры, отображаемая клиентом';
COMMENT ON COLUMN game_release.migration IS 'Последняя миграция, обновившая версию игры';
COMMENT ON COLUMN game_release.updated_at IS 'Время обновления версии';
INSERT INTO game_release(id,version,migration) VALUES(1,'0.28.0',280)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=clock_timestamp()
 WHERE game_release.migration<excluded.migration;
ALTER TABLE stations ADD COLUMN IF NOT EXISTS name_origin text NOT NULL DEFAULT 'legacy';
ALTER TABLE stations ALTER COLUMN name_origin SET DEFAULT 'player';
ALTER TABLE stations ADD COLUMN IF NOT EXISTS created_by bigint REFERENCES users(id) ON DELETE SET NULL;
COMMENT ON COLUMN stations.name_origin IS 'Происхождение станции: generated, player или legacy_custom';
COMMENT ON COLUMN stations.created_by IS 'Создатель станции; NULL для генератора и неизвестного исторического автора';

ALTER TABLE company_projects ADD COLUMN IF NOT EXISTS created_by bigint REFERENCES users(id) ON DELETE SET NULL;
COMMENT ON COLUMN company_projects.created_by IS 'Автор проекта; NULL для неизвестного исторического автора';
