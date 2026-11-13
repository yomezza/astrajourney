# Astra Journey — Первoоткрыватели, имена, база знаний и Planet Mode

**Статус: architecture review, не реализация.** Подготовлено на основе актуального на 13.11.2026 main (исходный SHA 470667919c06a871abd909dcd49213b9c67ceec4), AGENTS.md, README.md, docs/repository-organization.ru.md, реализации Cosmic/Planet/Chat, SQL schema.sql–schema240.sql и миграционного runner. **Ни одна схема не применялась, Go/JS/HTML/CSS не менялись.**

Цель: подготовить для последующего согласования три крупных изменения игры. Владельцем выбраны: механика первооткрывателей и однократных глобальных имён систем/планет; переименование колоний; PostgreSQL-база знаний; визуальный интерфейс **Original C «База знаний пилота»**; полноэкранное планетарное представление, возвращение через реальный посадочный объект и переиспользование существующего #startup-screen.

## Архитектурные документы

| Файл | Содержание |
|---|---|
| [01 — Discoveries and Naming](./01-discoveries-and-naming.md) | Источники исследований, system/planet first claim, founder protection, Unicode, legacy, colony naming |
| [02 — Knowledge Base](./02-knowledge-base.md) | CMS PostgreSQL, RU/EN категории и публикации, dynamic game catalogs, Original C reader |
| [03 — Knowledge Admin](./03-knowledge-admin.md) | Admin panel, Markdown, upload/sanitize SVG, manifest trusted assets, backup |
| [04 — Planet Mode](./04-planet-mode-transitions.md) | Space ↔ Planet state machine, return near landing, startup reuse, recovery |
| [05 — API Contracts](./05-api-contracts.md) | Существующие и предлагаемые HTTP/WS routes, ошибки, idempotency, invalidation |
| [06 — Database Design](./06-database-design.md) | Таблицы, SQL review drafts, транзакции, lock order, seeds, migration/rollback |
| [07 — Implementation Plan](./07-implementation-plan.md) | A discoveries, B knowledge, C Planet; gates, минимально необходимые тесты |
| [08 — Risks and Decisions](./08-risks-and-decisions.md) | Технические P0/P1, предлагаемые игровые правила, вопросы на одобрение |
| [SQL review](./sql/README.md) | Пять реальных SQL-файлов для DBA/backend review, **не подключены к работающей игре** |

**Отдельный полный промпт backend-разработчику:** [ai/prompts/backend/002-discovery-guide-planet/prompt.md](../../../ai/prompts/backend/002-discovery-guide-planet/prompt.md).

## Обязательный эталон Frontend

[Original C — выбранный прототип](https://github.com/yomezza/astrajourney/tree/frontend/pilot-guide-prototype/docs/ideas/pilot-guide-prototype/original-c).

В исходной ветке необходимо прочитать README.md, FUNCTIONALITY.md, HANDOFF.md, STYLE_GUIDE.md, index.html, app.js, styles.css, fixtures.js и hologram.js. Использовать **первоначальный C**, а не поздние вариации. Исправления прокрутки и полных 360° уже являются частью выбранного эталона. При переносе сохранить существующее игровое окно Modules, динамические данные кораблей/ресурсов, собственные игровые SVG; не импортировать фиктивный журнал/статьи из fixtures как game content.

## Ключевые архитектурные решения

- First discoverer системы = первый личный успешный завершённый survey(source='survey') при COMMIT. First discoverer планеты = первый личный шаг сканирования, раскрывающий именно planet-N. В том же SQL tx, что исследование. Покупка данных/корпоративные станции не grant.
- Canonical IDs и каталожные коды не меняются после rename. Одноразовое user entitlement; админ correction отдельно. Founder territory по Star.Founder ↔ companies.founder_key (не жёсткий список ID) — protected.
- Исторические объекты при недостатке достоверных данных не получают выдуманного discoverer. Legacy policy предложен в 01/08 и требует согласования.
- Реестр новых названий хранится в PostgreSQL и обновляет существующую Galaxy JSON в одной tx через World.Commit, чтобы все API получали одинаковое название. Cache refresh работает и без NATS.
- Колонии: отдельные name/history от planet_projects, owner/Corp ACL текущие; не менять действующую ownership систему.
- База знаний: Markdown (без raw HTML), опубликованные RU/EN ревизии, динамические игровые карточки без дублирования чисел, admin role server-side, отдельные изображения и trusted SVG manifest. Uploaded SVG как input только при безопасной растеризации; не выдавать raw SVG из пользовательского каталога.
- Planet Mode не является вторым браузерным процессом: сохранить Cosmic DOM/окна, переиспользовать Planet renderer/WS и #startup-screen, опционально Fullscreen API.
- Нормальный возврат: настоящий объект scene.landing, server-owned proximity, одноразовый PostgreSQL proof, проверка в Cosmic. Старые WS leave / HTTP return нельзя оставлять bypass. Emergency recovery отдельно и с аудитом.
- NATS только как необязательный Core сигнал, не grant, не основной способ координации и не replacement PostgreSQL.

## SQL review — важные условия

Драфты располагаются исключительно в docs/ideas/discovery-guide-planet/sql/ и имеют расширение .review.sql. Будущий backend-разработчик должен перенести исправленные SQL в следующий свободный файл internal/server/schemaXYZ.sql и добавить в Cosmic migration runner **после согласования и тестов**, не выполнять вручную на рабочей БД.

**Обнаруженная интеграционная ловушка:** в новой пустой базе OpenDatabase сначала применяет schema upgrades, а затем создаёт galaxy JSON. Поэтому SQL seed по galaxies JSON вернёт 0 строк на новой установке — Go должен заполнить aj_discovery_objects после initial world creation и при добавлении новых систем/планет, в согласованной транзакции. Все исторические backfill/Unicode name_key и founder zones проверить до открытия rename API.

## Что ждёт одобрения владельца

Предложения: точное событие first discovery, conservative legacy, Unicode/unique name scope, права/cooldown имени колонии, RU fallback, SVG-only-after-rasterize, расстояние от посадки, emergency recovery policy. Подробности 08. Не считать эти предлагаемые параметры утверждённым gameplay до решения владельца.

## Запреты и следующий шаг

В этом PR разрешены только Markdown, пять SQL-файлов для review и backend-промпт. **Нельзя** менять production Go/JS/HTML/CSS, конфигурации, базы данных, существующие миграции, релизы или NATS сервер. Создать PR без merge.

После архитектурного review владелец принимает или корректирует решения из 08. Лишь после этого backend-разработчик получает отдельную команду реализовать стадии A/B/C и PR на feature-ветке.

**Проверено:** GitHub исходники и документация; существование эталона Original C. **Не проверено:** PostgreSQL syntax на test DB, реальная чистота SQLite/кэша (не используется), браузерные и серверные test runs, deployment. Не выдавать проектный SQL за испытанный.
