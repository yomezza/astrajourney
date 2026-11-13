# 06. PostgreSQL, миграции, транзакции и обратная совместимость

**Статус:** схема для review, не применена. Актуальный код: Cosmic OpenDatabase() и schema.sql, schema033…schema240.sql; настраиваемый DB_SCHEMA, advisory_xact_lock(784216); Planet и Chat не владеют миграциями Cosmic. Подготовленные файлы находятся только в sql/, предложенная интеграционная нумерация schema250+ должна быть проверена заново перед PR реализации.

## Проектные сущности

| Назначение | Новые таблицы | Гарантии |
|---|---|---|
| Системы и планеты | aj_discovery_objects, aj_discovery_name_history | PK(kind,star_id,planet_id); однократное rename_used_at, UUID-идемпотентность; текущий owner названия и исходный catalog |
| Колонии | aj_colony_names, aj_colony_name_history | Отдельно от planet_projects, FK к актуальной planet_colonies, история смены имени и владельцев |
| Категории | aj_guide_categories, aj_guide_category_i18n | Дерево, parent FK RESTRICT, RU/EN, cycle checks |
| Статьи и связи | aj_guide_articles, aj_guide_article_revisions, aj_guide_related | Immutable (article,revision,locale), UUID links, без каскадного удаления |
| Публикация | aj_guide_publications, aj_guide_publication_history | Foreign key точно к существующей RU/EN revision, publish/unpublish аудируется |
| Изображения | aj_guide_media, aj_guide_revision_media | Служебный UUID и безопасный растровый output, ссылки на встроенные manifest keys |
| Возврат на корабль | aj_planet_return_authorizations | Server-generated proof с visit_id, epoch, generation, runtime_fence, TTL и consumed once |

Исходный мир хранится в JSON galaxies.data, отдельной реляционной таблицы stars/planets в актуальной игре нет. Нельзя создавать внешние ключи к отсутствующим таблицам. Действующий стабильный star.ID, planet.ID внутри star и galaxy catalog_code сохраняются.

## Набор SQL для review

- [001-discoveries-and-names.review.sql](./sql/001-discoveries-and-names.review.sql): имена/первооткрыватели/history, backfill из galaxies JSON.
- [002-knowledge-base.review.sql](./sql/002-knowledge-base.review.sql): RU/EN categories, revisions, publication, related links.
- [003-guide-media.review.sql](./sql/003-guide-media.review.sql): upload metadata and per-revision media links.
- [004-planet-return-authorizations.review.sql](./sql/004-planet-return-authorizations.review.sql): одноразовые разрешения с server-validated proximity.
- [005-colony-names.review.sql](./sql/005-colony-names.review.sql): отдельная запись имени и истории колоний.
- [sql/README.md](./sql/README.md): порядок, preflight и ограничения.

**Ни один файл не встроен в Go migrations и не выполнялся на настоящей БД.**

## Транзакционные правила

**Claim system:** внутри navigation.AdvancePlayer, в той же tx, что INSERT surveys(source='survey') и начисление XP, выполнить conditional UPDATE discovery object WHERE claim_state='unclaimed'. Повтор, конкуренция и rollback не назначают двух первооткрывателей.

**Claim planet:** при росте research_jobs.scanned рассчитать newly revealed planet IDs от старой до новой позиции SystemSites. Обработать также последний шаг перед DELETE research_jobs. Conditional UPDATE на отдельный object key. Покупные surveys и revealed corporate station не выдают claim.

**Rename:** под существующей world lock 784218 и row locks галактики и объекта, сначала проверить idempotency operation+hash, затем actor, consumed право, founder flag, current version, Unicode name key UNIQUE. В той же tx обновить registry, galaxies.data (по stable IDs), history, world revision, вызвать worldstore.Commit. При конфликте/откате право не расходуется. Админ исправляет название отдельным action без сброса used_at.

**Колония:** lock planet_colonies, проверить ТЕКУЩИЕ owner_user/owner_company и роли на DB внутри транзакции; идемпотентный UUID, 24h cooldown (предлагаемый), revision и history; административное исправление отдельно. Нельзя доверять owner из кэша фронтенда.

**Категории:** parent FK, запрет циклов, сериализация одновременного reparent; статьи никогда не удаляются автоматически при archive/delete category. **Статьи:** immutable revisions + atomic publication pointer and history. **Медиа:** двустадийная staging/DB ready операция, orphan GC и согласованные backup.

**Planet:** Planet WS return.prepare записывает proof только после проверки реального положения и epoch, Cosmic проверяет FOR UPDATE в своём мире/transfer transaction и потребляет одноразово. NATS не участвует.

## Кэш имён: особенно важное

Canonical current_name находится в aj_discovery_objects, а реальный worldstore читает Star.Name и Planet.Name из galaxies.data. До изменения UI нужно обеспечить **атомарное** изменение реестра и JSON мира одной SQL-транзакцией, world revision и корректную инвалидацию cached Galaxy. Не редактировать name только в одном месте. World generator/admin editor и initial migrations должны сохранять пользовательские имена; planet_surfaces.name — прежний snapshot, не самостоятельный источник текущего официального названия.

Все маршруты, сектора, ресурсы, поверхности, ships.star и станции используют ID, не изменять эти связи. Не менять seeds или geology от display name.

## Исторические объекты

Backfill:
- Системы с уже имеющейся surveys → legacy_unattributed, без выбора «самого раннего» по received_at.
- Планеты, видимые через завершённый surveys, частичный scanned или созданную ранее planet_surfaces → legacy_unattributed.
- Другие планеты/системы → unclaimed, будущий claim возможен только при личном серверном событии.
- Founder zones с непустым Star.Founder → protected (включая планеты) даже при ошибочной связи с компании-основателем — безопаснее fail closed.

До публикации проверить источник surveys.source, порядок объектов и prior hidden revisions: миграция не обязана восстановить то, чего исторически не хранили. Не импортировать fixtures.js Original C как реальные статьи.

## SQL preflight и тесты

1. Backup/PITR, disposable TEST_DATABASE_URL, активный DB_SCHEMA, schema240 installed, проверить все имена/IDs/ключи founder и count системы/планеты.
2. Проверить уникальность (galaxy_key,name_key) для систем и (star_id,name_key) для планет, особенно Unicode. Seed через SQL lower(trim) пока не идентичен полноценному Go NFKC+casefold: **согласовать и пересчитать все name_key перед открытием API**.
3. Убедиться, что нет конфликтов глобальных Star.ID и неверных Planet.ID; при preflight mismatch отказать в миграции, не чинить игровую географию случайным UPDATE.
4. Прогнать SQL в чистой и обновлённой тестовой схеме, проверять повторное применение, права, FK, article/cycle concurrency, concurrent rename, rollback, UUID duplicate, сессии/expiry proof.
5. Проверить утечки SVG, GC, backups PostgreSQL + внешние файлы. Не считать успешной SQL компиляцией интеграцию Go.

## Rollback

До публикации функций — additive tables без влияния на existing API. После пользовательских открытий и редакций удалять таблицы нельзя без потери истории. Безопасный откат: выключить feature flags, сохранить таблицы и legacy API, остановить новые записи; при необходимости восстановить согласованный backup. Удаление и обратные DDL требуют отдельного согласования.

## Оставшиеся SQL-review вопросы

- Драфт 005 уже содержит UNIQUE(surface_id,name_key) и составной FK (colony_id,surface_id,sector_number) на planet_colonies(id,surface,sector); проверить их в реальной PostgreSQL, поведение при удалении поверхности и при наследовании исторического имени проекта.
- Для строгой защиты от concurrent category move проверить транзакционную сериализацию + SQL trigger под реальным isolation, не только single-thread unit test.
- Для world revisions проверить все writer-пути galaxies.data, включая admin galaxies и инкрементальное расширение. Всякий неверно обновлённый name mirror должен обнаруживаться consistency checker.
- Чувствительные имена пользователя и audit retention: не показывать личную историю без политики приватности.
