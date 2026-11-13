# Astra Journey — backend: первооткрыватели, база знаний Original C и Planet Mode

Ты — backend-разработчик проекта Astra Journey. Репозиторий: https://github.com/yomezza/astrajourney.

**Начинай implementation только после отдельного явного согласования архитектурного PR владельцем.** Само наличие этого файла не разрешает внедрение.

## Обязательно прочитай до изменения кода

1. AGENTS.md, README.md, docs/repository-organization.ru.md.
2. Все документы docs/ideas/discovery-guide-planet/README.md и 01–08, SQL review files из sql/ (особенно sql/README.md).
3. Текущие исходники Cosmic, Planet, Chat, внутреннюю PostgreSQL-схему и Cosmic migration runner. Перед каждым PR проверь свежий main — номера схем/пакетов могли измениться.
4. Утверждённый пользователем визуальный Original C — ветка frontend/pilot-guide-prototype, каталог docs/ideas/pilot-guide-prototype/original-c: README.md, FUNCTIONALITY.md, HANDOFF.md, STYLE_GUIDE.md, index.html, app.js, styles.css, fixtures.js, hologram.js.
5. Существующие internal/server/web/features/research, guide, planet, shell, ui/windows.js; server-side navigation.AdvancePlayer, research.RevealedSites, worldstore, colonies, corporations, planet visits/transfer.

**Не создавай новую визуальную концепцию.** Original C — утверждённая база. Прототипные fixtures и процедурный hologram.js не являются production контентом/ассетами.

## Разрешённый процесс

После подтверждения владельца создай отдельную feature-ветку от актуального main. Реализуй по стадиям с адресными тестами. Используй имеющиеся технологии Go+PostgreSQL+vanilla JS; не добавляй обязательных внешних сервисов. Не редактируй рабочую БД вручную, не меняй реальные конфиги/секреты и не запускай deployment. Подготовь PR и краткий отчёт. **Не выполняй merge, не создавай релиз, не выкатывай изменения без разрешения владельца.**

## Этап A — исследования и глобальные имена

1. Проверить архитектуру событий: в navigation.AdvancePlayer личный survey(source=survey) при окончании сканирования и последовательные newly revealed Planet sites при росте scanned. Первые права фиксировать в той же PostgreSQL transaction, где зафиксировано исследование; покупные сведения и корпоративные станции не дают claim.
2. Сохранить уникальность (kind,star_id,planet_id), transaction row-lock/CAS для двух игроков, first successful COMMIT wins. Систему и конкретные планеты могут открывать разные игроки.
3. Исторические объекты оставить legacy_unattributed без выдуманного discoverer. Защищённые системы корпораций-основателей определять по реальному Star.Founder и companies(founder=true,founder_key), не фиксированным ID.
4. Однократное пользовательское rename для системы/планеты: UTF-8 Unicode NFKC+casefold+space normalization, duplicate keys, prohibited text, stable ID, UUID-idempotency, history, rename_used_at. Админ correction отдельная команда, не возвращающая право.
5. PostgreSQL metadata и galaxies.data обновлять атомарно через существующий world lock и World.Commit; поднять revision, инвалидировать карты/Planet labels. На **пустой новой БД** schema migration происходит **раньше генерации galaxies JSON** — обеспечить post-seed заполнение registry и для всех новых систем.
6. Переименование готовых planet_colonies: отдельный name/history, current personal/corporate owner ACL (CompanyAccess colonize/manage), owner change handling, cooldown если согласован; не менять существующее владение и planet_projects.name.
7. Расширить существующий модуль «Исследования» и API, не делать лишнего окна. Не сломать routes, resource seeds, planet surfaces и гиперкоридоры.

## Этап B — PostgreSQL база знаний и Original C

1. Интегрировать согласованные SQL review drafts 002/003 в **Cosmic migration runner** после проверки номера схемы и тестовой базы. Не использовать runtime DDL. Файлы в docs/ideas/.../sql — review templates, не готовые применённые миграции.
2. Категории RU/EN с иерархией, порядком, archive/hidden и безопасным RESTRICT на непустую категорию. Статьи: Markdown без raw HTML, immutable per-locale revisions, drafts, публикация per locale, related UUID, full audit, safe fallback на RU.
3. Публичный read API — только опубликованные материалы; admin create/edit/publish/media — только users.role=admin server-side. Конкурентные правки через expected_revision, недоступные статьи скрывать.
4. Сохранить реальные динамические справочники ресурсов, кораблей и объектов из кода; не переносить в БД копии меняющихся игровых характеристик. Совмещать их в Original C каталоге.
5. Загрузка PNG/JPEG/WebP и SVG как input: реальное content sniffing, пиксельные/размерные лимиты, защита от bombs/XSS. SVG безопасно растрировать в sandbox без сети; если безопасного renderer нет — временно 415 на SVG. Raw загруженный SVG никогда не рендерить в same-origin UI. Файлы вне webroot, PostgreSQL metadata, UUID, backup/GC.
6. Каталог встроенных trusted SVG — whitelist/manifest с stable keys, не произвольный файловый путь. Использовать игровые objectHologram и SVG (плавный цикл 360°, без сброса на 180°), reduce-motion.
7. Перенести только двухпанельную компоновку Original C **в существующий Modules.open('guide')**, сохранить текущую titlebar, resize/move, I18n RU/EN, поисковые вкладки, related links, favorites/recent scoped к пользователю и scroll списка без скачков. Не импортировать demo window или fixtures как authoritative.

## Этап C — цельный планетарный режим

1. Убрать представление Planet как обычного отдельного плавающего окна, а не менять физику и WS. Новый Space Mode ↔ Planet Mode на том же браузерном клиенте, сохраняет скрытые космические окна, их геометрию и состояние. Полноэкранный контейнер, опциональный browser Fullscreen API (ESC не завершает визит).
2. Использовать существующий #startup-screen/#startup-title/#startup-message/#startup-retry и единый переходный controller с token/cancel/timeout/retry. Не создавать вторую загрузочную заставку.
3. Для нормального возврата использовать настоящий scene.landing/посадочный модуль. Planet сервер проверяет текущий sector, действительный visit/epoch/fence, authoritative location и допустимую дистанцию; выдаёт одноразовый DB return proof по UUID. Cosmic POST /api/planet/return подтверждает и потребляет этот proof до revoke/refund-safe reconciliation. Старый WS leave и HTTP return нельзя оставлять доступными в обход проверки.
4. Ошибки связи, F5, перезапуск Planet, missing landing и return pending не должны запирать игрока навечно. Отдельный server-diagnosed audited emergency recovery, а не свободная UI кнопка обхода.
5. Один Planet WS control lease, действующая авторизация и fencing, сохранённый renderer/A*/transition, никаких двойных обработчиков и конфликтующих сессий. Chat не дублировать.

## PostgreSQL и миграции

Проверить, исправить и интегрировать только после утверждения пять файлов docs/ideas/discovery-guide-planet/sql/*.review.sql. Убедиться, что они подходят реальной PostgreSQL/DB_SCHEMA, seed нового мира, валютному и game schema compatibility. На всех критических операциях — SQL гарантии, constraints, foreign keys, unique operation, блокировки и idempotency. Сначала тестовая база, backup/preflight, then Cosmic migration runner; не применять напрямую к рабочему серверу. SQL 005 уже описывает per-surface uniqueness и composite colony FK; подтвердить на реальной схеме. Нельзя удалять исторические данные для rollback.

## Минимальные обязательные проверки

- Конкурентный first claim и один rename; повтор UUID, иной body, неправильный discoverer, founder protection, legacy without ownership.
- Кэши/маршруты/Planet названия после rename и отсутствие смены stable ID.
- CRUD/архивирование категории без потери статей; RU/EN publication/fallback; XSS/SVG/media, unauthorized admin requests.
- Original C: scroll stays, текущие game facts, 360° SVG, RU/EN, 420×580 и 810×550.
- Планета: near/far return, прямой обход старого endpoint, двойной запрос, stale epoch, сетевой сбой/F5, единственный #startup-screen, сохранение окон и movement.
- Запускать только адресные Go integration/unit tests и короткие browser smoke. Тяжёлые многократные полные прогоны не делать без причины. В отчёте отделить реально выполненные от пропущенных тестов.

## Результат

Сделай последовательные проверяемые изменения через feature-ветку, подготовь PR на main и отчёт: какие схемы/Go/API/Frontend изменены, какие тесты пройдены, что не проверялось, риски/совместимость и рекомендации. **Не merge, не deployment, не релиз.** Если проектные правила противоречат актуальному коду или спорный продуктовый пункт ещё не утверждён, зафиксируй проблему и предложи минимальное решение — не изобретай своё игровое правило в молчаливом режиме.