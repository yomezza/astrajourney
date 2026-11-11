# Архитектурный аудит Astra Journey и план рефакторинга

Дата: 28 октября 2026. Проверенный commit: `3c189ec24c7615f7f296552881662d40f13dc453`, версия 0.12.1.

## Область и достоверность

Выполнены чтение исходников, поиск символов и вызовов, безопасные тесты без интеграционной БД, go vet и чтение системных каталогов доступной локальной PostgreSQL. Исходный код, зависимости, схемы и игровые данные аудитором не менялись; миграции, benchmark, игровые API и исправления не запускались. Коммитов и релизов нет. Материалы отчёта находятся вне репозитория.

Это аудит исходного кода и локального экземпляра разработки, не доказательство идентичности вашей Windows-инсталляции. Метаданные PostgreSQL получены в BEGIN READ ONLY с statement_timeout. SELECT статистики не является нагрузочным тестом; работающий игровой процесс может независимо изменять свои данные. Показатели не используются как доказательство промышленной производительности.

Обозначения: Ф — подтверждённый факт кода/каталога; Р — обоснованный риск без воспроизведения инцидента; Г — гипотеза для измерений. Отсутствие найденной уязвимости не является доказательством их отсутствия.

## А. Общая оценка

Практический вывод: сохранить Go, PostgreSQL и обычный браузерный клиент, перейти постепенно к модульному монолиту по игровым возможностям. Использовать узкие порты на реальных границах, без обязательной четырёхслойной обёртки каждой функции. Отдельный сервер понадобится активным секторным локациям, а не генерации глобальной поверхности и не колониальным проектам.

Сильные стороны: серверная проверка прав; ограниченный JSON и дедлайны; приватная генерация скрытых данных; транзакции и блокировки; частичная уникальность резервирования; идемпотентность логистики и колонизации; фиксация стоимости и географии; интеграционные и конкурентные тесты; автономный EXE и выбранная DB_SCHEMA.

Главные ограничения: один пакет server без компиляторных границ подсистем, широкие команды/DTO, SQL внутри оркестраторов, слишком крупная сериализация мутаций, процессные кэши, миграции при запуске, повторяющиеся клиентские обновления. Из размера файла самого по себе вывод о дефекте не делается.

## Б. Фактическая структура

```text
cmd/cosmosimulator/        main.go, platform_other.go, platform_windows.go
internal/server/          один рабочий Go-пакет; Go, SQL и embedded web
  web/                    HTML, 16 JS, 2 CSS, RU/EN JSON, SVG
scripts/                  генераторы графики Python
.github/workflows/        публикация релиза по v*-тегу
release-notes/            история выпусков
Downloads (downloads/)    сохранённые Windows EXE и SHA256SUMS
docs/                     roadmap, дизайн, контекст и спецификации
```

`go list ./...` даёт два пакета: cmd/cosmosimulator и internal/server. В server: 64 Go-файла, из них 39 рабочих и 25 тестовых; 10 263 строки рабочего Go и 14 880 вместе с тестами; 237 рабочих объявлений функций/методов, 69 Test-функций. Подсчёт строк включает комментарии и пустые строки. Каталог JS содержит 16 файлов. Подробная инвентаризация каждого Go-файла — files.csv, функции и ориентировочные диапазоны — functions.csv.

Вход: main → runPlatform → runConsole → server.Run. Windows-обвязка отдельно, основной сервер общий. Run открывает PostgreSQL/миграции, читает конфигурацию, выполняет bootstrap, создаёт App и маршруты, запускает simulate и HTTP. Завершение инициируется context и Shutdown. Нельзя переносом поменять семантику отмены и Windows SCM.

## В. Карта internal/server

| Группа / файлы | Фактическая ответственность | Будущая граница |
|---|---|---|
| server.go | Run, routes, auth, bootstrap, state, tick, HTTP helpers, legacy adminUsers | composition root + identity + transport + scheduler |
| database.go | schema selection, pool, все миграции, обновление мира | platform/postgres + migrations; world bootstrap вызывается явно |
| request_security.go, compression.go | proxy identity, JSON limits, deadline, gzip | transport/http |
| terminal.go, workbench.go | смешанные profile/admin/corporations/markets/system/mining/action dispatch | разнести по владельцам сценариев, не переносить целиком |
| economy.go, markets.go | кошельки, склады, заказы, доставка, рыночные операции | economy + inventory + freight |
| logistics.go | batch transfer, доступ к хранилищам, идемпотентность | inventory и freight; общий tx сохраняется |
| ships.go, shipyards.go | характеристики, покупка, лимит флота, остатки верфей | space/fleet, economy вызывается через узкую границу |
| expedition.go, navigation.go, tasks.go, estimate.go, sites.go | манёвры, cargo/fuel, research, добыча, чтение корабля, оценка пути | space/navigation, extraction, research; затем scheduler |
| planner.go | HTTP планировщика, очередь и исполнение | automation с явными портами navigation/inventory/extraction |
| galaxy.go, universe.go, names.go, catalog.go | модели, генерация, пути, справочники и HTTP/cache | space/world + resource catalog |
| discovery.go, discovery_private.go | раскрытие объектов и приватные RNG | space/discovery; не объединять с публичным RNG |
| galaxy_management.go, galaxy_delete.go | изменение мира, коридоров, станций, preview/delete | world administration/use cases |
| founder_settlements.go, galaxy_rings.go, galaxy_portals.go | распределение владений, геометрия и портал | space/world специализированные компоненты |
| corporations.go | роли, казна, склады, проекты, портал | corporations; портал отдать space при выделении |
| planet_surface.go | географические данные и чистая генерация | space/planetmap; НЕ сервер 2D-секторов |
| planet_projects.go, planet_api.go | доступ, HTTP, география, проекты, деньги, возвраты, tick/landing | space/colonization, обращения к inventory/economy |
| gameplay.go | настройки, квоты, дата, страницы пользователей | settings + extraction + players administration |
| admin_resources.go, admin_teleport.go, admin_changes.go | административные сценарии и аудит | admin endpoints, делегирующие владельцам данных |
| ideas.go, status.go | предложения и статус сервера | небольшие feedback/observability handlers |
| benchmark.go | виртуальная нагрузка, временные схемы, глобальный контроль | отдельная административная capability; не runtime planet |

Крупные функции: expedition 106–496 expedition.go; corporationCommand 38–365 corporations.go; colonyAction 26–348 planet_api.go; openDatabase 39–290 database.go; globalMarket 537–754 terminal.go; logistics 113–363 logistics.go. Главная проблема — совмещение ответственностей и зависимостей, не числовой порог строк.

## Г. Дублирование и различия, которые нужно сохранить

| Случай | Источники | Что делать впоследствии |
|---|---|---|
| Изменение credits через helpers и прямой SQL | economy.go:33–52 debit/credit; expedition.go:346–410; terminal.go:160,288,708–732; corporations.go:75–104 | единые проверенные денежные примитивы, но сохранить котировки, комиссии, ledger reasons и атомарность целого действия |
| Идемпотентность request/digest/result | logistics.go:180–211,354; planet_api.go:65–78,340 | общий небольшой transactional receipt helper; отдельные namespaces, тела и бизнес-проверки |
| Read/modify/write JSON-инвентарей | warehouse/putWarehouse economy.go:14–32; storageAccess/writeStorage logistics.go:28–91; adminResources; claimPlanetRefund | общий низкоуровневый адаптер количества; НЕ единая функция, снимающая различия прав корпорации и игрока |
| Проверка admin до и внутри tx | adminResources, colonyAction, planetSettings, adminGalaxies | транспортный guard + повторная авторитетная проверка в защищённой операции; повтор здесь защищает от смены прав |
| Уведомления через trigger и явные INSERT | schema033.sql:70–73; planner.go; advanceColonies | определить события и единственного доставщика для каждого вида; сейчас это разные виды, двойная доставка не доказана |
| Правила Go и ограничения PostgreSQL | MaxCredits и enforce_credit_ceiling | это нужная защита двух уровней; синхронизировать контрактом/тестами, не удалять один слой |
| Планетарная интерполяция Go/JS | PlanetSurface.sample, planet.js terrain | общие версионные fixtures граничных координат; нельзя заменить алгоритм с изменением старых берегов |
| Справка лимита флота и fleetLimit | ships.go:195–210; expedition.js техническая статья | справку формировать из read-only capabilities; не менять сами диапазоны при рефакторинге |
| Формы, запросы и перерисовки клиента | expedition.js, operations070.js, logistics.js, planet.js | сохранить UI/UX, выделять по фичам поверх UI/Modules, а не новый generic framework |

Разные приватные/публичные генераторы, транзакции казны и личного кошелька, ручная и автоматическая добыча не объявляются взаимозаменяемыми только из-за сходного кода. Транзакцию сценария нельзя заменить набором самостоятельных repository.Commit.

## Д. Проблемы и приоритеты

| ID | Факт/риск | Приоритет / риск изменения | Основание и рекомендация |
|---|---|---|---|
| A1 | Ф: миграции выполняются повторно при каждом старте, вместе с bootstrap мира | высокий / высокий | openDatabase; schema033 DELETE уведомлений и DROP/CREATE triggers, schema070 затем schema110 заменяют одну функцию; создать отдельный план версии/checksum, но не пропускать старые операции вслепую |
| A2 | Ф: значительная часть мутаций использует одну advisory-блокировку 784218 | высокий / высокий | advanceTick, planner, colonyAction, logistics; корректность сейчас зависит от неё. Она действует в рамках БД и не включает DB_SCHEMA, поэтому независимые игровые схемы тоже конкурируют. Сначала измерить ожидания и зафиксировать lock-order |
| A3 | Р: оставшиеся обращения loadUniverse(ctx) из tx могут ждать второй pool connection/cache mutex | высокий / высокий | planner.go:590; tasks.go:218; workbench.go:282; expedition.go:128. Исправление planet q-path не является доказательством безопасности всех старых путей; нужен отдельный saturation test после разрешения |
| A4 | Ф: process-local кэш не имеет общего механизма инвалидирования | высокий перед несколькими writers / средний-высокий | App.universe и station state; два Go-процесса могут видеть разные версии. Нужны revision contracts; LISTEN/NOTIFY только ускорение, не единственная гарантия |
| A5 | Ф: финансовые и навигационные части корабля сохраняются одним saveShip | высокий перед разделением серверов / высокий | navigation.go saveShip пишет cargo/fuel/position вместе. Планетарному серверу нельзя параллельно писать ships.cargo |
| A6 | Ф: PostgreSQL не проверяет все межтабличные/JSON-инварианты | высокий перед независимыми writers / высокий | нет cargo CHECK; colony/project owner-соответствие не FK-контракт; app охраняет сегодня. Нет доказанного обхода через HTTP; сначала диагностика старых данных |
| A7 | Ф: GET workbench/fleet/system и colonyView не являются чистыми чтениями | средний-высокий / высокий | advancePlayer/seedStations/ensureShip; colonyView берёт project FOR UPDATE и вызывает ensureShip в откатываемой tx. Нельзя просто убрать побочные эффекты: это меняет нынешнее доведение состояний |
| A8 | Ф: расхождение q/search в клиенте выдачи ресурсов | средний / низкий-средний | operations070.js:413,437 отправляет search; userPages gameplay.go:298 читает q. Поиск может игнорироваться. Отдельное исправление с разрешения, не смешивать с переносом пакетов |
| A9 | Ф: два одинаковых по ключу индекса ships.id | средний / высокий для удаления | ships_pkey и ships_identity. mining_jobs_ship_id_fkey привязан к ships_identity; сначала спланировать перенос зависимости, не DROP INDEX |
| A10 | Ф: публичный fleet перечитывает корабли по одному и отдаёт чужой флот без viewport-фильтра | средний / средний | navigation.go:18–84, readShip; измерить N+1 и размер ответа, не объявлять текущим bottleneck без нагрузки |
| A11 | Р: ошибка одного проекта/операции откатывает общий tick | высокий доступность / высокий | advanceTick → advanceColonies → completeMining → bots в одной tx. Бounded jobs требуют нового согласованного tick-протокола, не разрезать атомарность механически |
| A12 | Ф: planet_visits — запись допуска, не протокол активного персонажа | обязательный этап перед 2D / высокий | colonyAction land/leave, движения корабля с visits не связаны. Нельзя использовать старую запись или replay ответа land как вечное разрешение симуляции |
| A13 | Ф: встроенные окна и HTTP обновляются крупными блоками | средний / средний | app.js:96–120 параллельно читает 7 endpoints; gamedata вызывает дополнительные рендеры. Профилировать объём и частоту, не обещать выигрыш от framework |
| A14 | Ф: роль локальной dev-БД cosmos — superuser | высокий перед внешним развёртыванием / средний | подтверждено pg_roles; это не утверждение о Windows/production. Runtime-пользователю будущего двухсерверного приложения нужны ограниченные права, миграции — отдельной роли |

Подтверждённой потери денег, воспроизведённого взаимного deadlock или работающего HTTP-обхода прав этот аудит не установил. В частности, разный порядок user/task row locks в planner и advanceTask сейчас прикрыт общей advisory-блокировкой; убирать её без исследования всего графа ожиданий опасно.

## Е. PostgreSQL: фактические объекты

Локальная версия — PostgreSQL 17.11 (Debian). В игровой схеме cosmosimulator: **48 таблиц, 26 последовательностей, 80 индексов; 48 PK, 63 FK, 44 CHECK, 12 UNIQUE constraints**. Частичные уникальные индексы учитываются в индексах, не обязаны быть UNIQUE constraints. Прикладных views, materialized views, procedures и пользовательских enum/domain/base types не найдено; row-composite и служебные массивные типы не считаются отдельными игровыми типами. Установлено только стандартное plpgsql. Вне системных схем присутствуют cosmosimulator и public.

Три функции SECURITY INVOKER, без proconfig search_path:

- ledger_credit_change: AFTER UPDATE users.credits, пишет financial_ledger и tx-local cosmos.reason;
- event_notification: AFTER INSERT game_events, создаёт уведомления выбранных видов;
- enforce_credit_ceiling: BEFORE INSERT/UPDATE credits на users, companies, local_markets; текущий предел 9 007 199 254 740 991.

Всего пять прикладных triggers. Активной SECURITY DEFINER-функции нет. В schema070 остаётся историческая версия 500 млрд, schema110 заменяет её при запуске; это последовательность миграций, а не две одновременно действующие функции.

Полный каталог определений — postgresql-inventory.json. Доступная база почти пуста: 3 users, 3 ships, 0 planet_projects. По такой базе нельзя утверждать, что индекс не нужен на промышленной нагрузке. EXPLAIN ANALYZE, ANALYZE, расширения и изменение статистики не выполнялись.

Транзакции по умолчанию READ COMMITTED; row locks и conditional UPDATE обеспечивают основные инварианты. Isolation SERIALIZABLE не предлагается включать повсеместно: нужны retry semantics и измерения. planet_sector_claim уже запрещает два funding/building/complete проекта. planet_colonies имеет unique(project) и unique(surface,sector). Не нужно заменять эти гарантии предварительным SELECT.

JSONB географии и фиксированных snapshot условий уместен. JSONB высокочастотно изменяемого cargo требует более сильного контракта: при росте конфликтов рассмотреть строки (storage, resource, quantity), но не немедленную массовую нормализацию мира. Отрицательные и нецелые значения, ownership-связи и допустимые переходы нельзя считать защищёнными только потому, что поле JSONB.

## Ж. Клиент

expedition.js — 3364 строки, galaxy.js — 1805, planet.js — 1044, logistics.js — 832, windows.js — 641, controls.js — 636. terminal033.css — 3346 строк/60 759 bytes; style.css — 14 длинных строк/19 042 bytes. Совпадающие selectors местами являются каскадными переопределениями, не доказательством ненужного CSS.

Основа IIFE + globals UI, Modules, Game, gameData, api и DOM events. Сохранять этот стек допустимо. Предлагаются небольшие feature scripts или постепенно native ES modules: session/api, windows, fleet, markets, corporations, automation, planet-map, colony-projects. Сначала описать event payloads и правила cancellation/authEpoch, затем извлекать без изменения подписок. Не допустить двойных listeners, разной фазы голограмм, сброса раскрытий и повторного выполнения кнопок.

Географию/геосетку/сектора нельзя унести в planet-runtime UI вместе со всем planet.js: map и projects остаются клиентом космического API. Будущая 2D-сцена подгружается отдельно. Не требуется React/Vue, новая система сборки или перерисовка дизайна.

## З. Сравнение архитектур

| Подход | Польза здесь | Цена / ограничение |
|---|---|---|
| Модульный монолит | общий процесс/tx, постепенное выделение и Windows EXE | границы надо проверять imports и API модулей |
| Feature-based пакеты | понятно AI и человеку, искать изменения в одной игровой области | не отменяет необходимость транзакционных контрактов |
| Clean Architecture | изоляция правил от HTTP/pgx в критических сценариях | полное применение к каждому CRUD создаст лишние DTO/интерфейсы |
| Onion | полезное направление зависимостей | один огромный domain снова станет God package |
| Hexagonal | хорошие порты для navigation/inventory и будущего handoff | интерфейс на каждую функцию не нужен |
| Немедленные микросервисы | отдельный жизненный цикл active sectors в будущем | сейчас раздробят tx и увеличат эксплуатационные риски |

Выбор: feature-based модульный монолит с отдельной композицией, узкими consumer-defined портами и выборочной изоляцией чистых правил. Clean/Onion — принципы зависимости, а не шаблон каталогов для копирования.

## И. Предлагаемая структура (не создана)

```text
cmd/cosmosimulator/          сохранить Windows/console entrypoint
cmd/planet-server/          только следующая очередь
internal/app/space/         wiring, HTTP, фоновые процессы
internal/app/planet/        будущий wiring активных локаций
internal/platform/postgres/ pool, DBTX, ошибка SQL, migration runner
internal/platform/httpx/    JSON, deadline, principal transport, responses
internal/identity/          accounts, sessions, login policy
internal/players/           стаж и возможности
internal/resources/         каталог и определения ресурсов
internal/economy/           кошельки, рынок, ledger conventions
internal/inventory/         авторитетные операции со station/ship storage
internal/corporations/      membership, roles, permissions, treasury workflows
internal/space/world/       galaxy, discovery, immutable world revisions
internal/space/navigation/ ship positions/routes/fuel
internal/space/research/    scanner and reveal rules
internal/space/extraction/  shared quotas and mining
internal/space/planetmap/   global geography, hex IDs and sampling versions
internal/space/colonization/projects, claims, contributions, landing admission
internal/automation/       queues, freight/task orchestration
internal/notifications/    append/delivery contracts
internal/scheduler/        current cycle orchestration
internal/admin/            guards/endpoints delegating to owners
internal/feedback/         ideas
internal/sector/            future local 2D runtime; not global planet map
internal/contracts/        only actual cross-server DTO/IDs, no God domain
```

Каталоги web/migrations сначала сохраняют embed-пути. Позднее перенос ресурсов допустим только вместе с embed/build проверками. Не создавать все пакеты одним этапом. Небольшие handlers/status допустимо оставить в composition root до реальной необходимости.

Правила: HTTP → application service → узкие операции/порты; domain не импортирует HTTP; tx принадлежит целому сценарию; infrastructure не решает правила игры. Composition root соединяет модули. Не позволять обратному импорту economy → corporations → economy: orchestrator получает permission и money ports, либо membership capability передаётся явно. DBTX отражает реальные pgx Query/Exec, а не абстрактный IRepository на всё.

## К–Л. Карта миграции и поэтапный план

Каждый этап — отдельное разрешение, работающий commit и возможность остановиться. До R3 запрещено параллельно менять механику. Новые тесты ниже — предложения, сейчас они не создавались.

| Этап | Файлы/функции и цель | Зависимости / сложность | Проверка и остановка/откат |
|---|---|---|---|
| R1 | сохранить маршруты, DTO, сообщения ошибок, SQL locks и fixtures поведения | без переносов; низкая-средняя | разрешённые новые characterization tests; исходный binary/DB backup; отдельно согласовать q/search bugfix |
| R2 | выделить чистые planetRadius/planetSize/generateSurface/sample/smoothRiver и geometry types из planet_surface.go | только math/hash; низкая-средняя | stable seeds, 50/100/160/200, соседи, берега и Go/JS fixtures; можно остановиться с прежними adapters |
| R3 | вынести resource catalog, hull calculations, priceShipClasses и fleetLimit | resources → consumers, без HTTP/DB; средняя | старые цены/размеры/стаж и cargo limits; компиляция всех платформ; без изменения чисел |
| R4 | аккуратно вынести reply/decode/gzip/proxy/deadlines и principal, затем identity | server.go/request_security/compression; средняя | login, cookies, origin, slow-body, role changes, error codes; старые маршруты сохраняются |
| R5 | разделить databaseConfig, migration runner, world bootstrap | database.go/SQL; высокая, нужен отдельный rollout | fresh DB + upgrade каждой поддерживаемой схемы, snapshot данных и повторный запуск; сначала baseline, потом version ledger, DDL не смешивать с gameplay |
| R6 | money/inventory adapters + receipt helper | economy, logistics, admin_resources, saveShip; высокая | concurrency, floor/ceil, fees, batch rollback, retries и ownership; не делить существующую tx; legacy adapters можно оставить |
| R7 | corporations membership/role workflows | companyAccess/corporationCommand/terminal companyAction; высокая | владелец, выход, revoke, 7 ролей, распускание с history; ports вместо импортного цикла |
| R8 | world/navigation/research/extraction | galaxy/tasks/expedition/workbench/terminal; высокая | replay routes, scanner order, quotas, fuel, RNG, world revision and cold-pool tests; сохранять старую оркестрацию |
| R9 | colonization service/HTTP/repository отдельно | colonyAction/planetReceived/refunds/advanceColonies, вход money/inventory/permissions ports; высокая | все planet120 tests плюс last-fund/expiry/revoke/replay/source-unavailable fixtures; map остаётся space |
| R10 | scheduler/automation/freight/notifications | simulate/advanceTick/activities/planner/advanceLogistics/recordEvent; высокая | один tick — один прогресс, reboot, forced tick, SKIP LOCKED лишь после семантических тестов; не дробить глобальную tx без нового согласованного протокола |
| R11 | клиент по фичам и явным API/event contracts | expedition.js, planet.js, operations070.js, styles; средняя-высокая | browser flows RU/EN, сохранение окон/раскрытий/selection, отсутствие дублей и auth races; по одному модулю |
| R12 | ограниченные DB guarantees/SQL optimizations из следующей таблицы | только доказанные узкие места; средняя-высокая | baseline old/new outputs, contention, rollback migrations; один источник writes, без бессрочного dual-write |
| R13 | подготовить и отдельно реализовать planet-server | contracts/sector/admission/inventory handoff; высокая | outage/replay/reconnect/lease/fencing tests до включения реальных персонажей |

R2 и R3 допустимо объединить только при небольшом diff. R5–R10 нельзя безопасно начать без тестовой базы поведения. Удаление старого adapter — отдельный завершающий шаг после переключения всех callers. Сначала expand-compatible изменения, потом переключение, и только потом contract/delete; несовместимую БД не откатывать запуском старого EXE наугад.

## М. Матрица риска

Наибольший риск изменения: finance/инвентари/tx, auth/права, migration sequencing, random seeds, порядок scanner reveal и tick. Возможные последствия: дублирование/потеря имущества, утечки закрытой карты, новые мировые координаты, double completion. Средний: cache invalidation, DTO и UI events. Низкий: только после проверки — удаление неиспользуемого adminUsers и косметические имена.

Для критических данных сначала зафиксировать правила и воспроизведение, затем менять организацию. Архитектурный перенос и согласованные изменения игровых правил должны быть разными задачами. Ни одна подтверждённая текущая эксплуатационная уязвимость уровня «получить кредиты через console» этим read-only аудитом не продемонстрирована.

## Н. Проверки

В этом аудите: env -u TEST_DATABASE_URL go test ./... — успешно, 16 верхнеуровневых тестов PASS, 53 SKIP; go vet ./... — успешно; go list — два пакета; git status чист. Интеграционные тесты не повторялись: их fixtures вызывают openDatabase и CREATE/DROP SCHEMA. Предыдущие запуски разработки не подменяют проверки этого аудита. Coverage всех сценариев и DB race нельзя вывести из одного зелёного запуска без TEST_DATABASE_URL.

Существуют tests по security081, schema isolation, fleet/progression, trade, planet120/concurrency/refunds/cold-pool, logistics batches и UI catalog/holograms. Недостающие будущие проверки: все пути cold-cache внутри tx; cross-process stale cache; handoff визита/корабля; запрет второго sector writer; права на уровне разных DB roles; baseline Go/SQL samplers; lifetime UI cancellation and admin resource search; migration-ledger upgrade chain; fault injection перед/после commit; полный saturation/lock-order graph. Результат проверки повторяется после каждого разрешённого этапа, с отдельными backup/restore и read-only сравнением старых сохранений.

## О. Два сервера и владельцы данных

| Данные/действие | Единственный ответственный writer | Что разрешено второму серверу |
|---|---|---|
| accounts, sessions, credits, markets, station stores | космический сервер / его авторитетные сервисы | подтверждённая identity, команды через контракт; не прямые UPDATE |
| ships: cargo/fuel/routes/position | космический сервер | проверять согласованный допуск/передачу, не saveShip из 2D |
| global geography, hexes, claims, colony projects/ownership | космический сервер | читать immutable geography с version/ID; не генерировать альтернативный берег |
| landing admission/planet_visits | космический admission service; планетарный потребитель | подтвердить одноразовый вход с актуальными правами и сроком/версией; нынешняя строка сама по себе не токен |
| characters, локальная добыча, buildings, interactions | будущий планетарный сервер | космический UI читает опубликованные результаты, не пишет напрямую |
| surface inventory персонажа | планетарный сервер | передача в ship/station через handoff, а не два writer одного cargo JSON |
| schema/migrations | один контролируемый deploy owner | runtime обоих серверов без DDL |

Нельзя разделить обработчики, сохранив обоим право произвольно записывать один JSON cargo: saveShip потеряет параллельные изменения. Передачи оформить согласованной durable операцией с operation ID, стадиями и квитанцией, например reserved → accepted → committed/refunded. Это предложение контракта, не созданная таблица/брокер. Повтор и падение каждого сервера проверяются до запуска. Outbox может понадобиться при асинхронной доставке, но сначала доказать необходимость; Redis/Kubernetes не требуются.

Активный сектор — отдельная локация с единственным runtime owner. Позиции/пути/ввод — в памяти, checkpoint по принятой политике. При горизонтальном запуске нужны lease и fencing, иначе два процесса изменят один сектор. Глобальную карту не переносить в runtime. Planet-server читает сохранённую версионную географию и координаты, его local детализация не вправе менять существующий берег.

## П. Мёртвый код и итоговые рекомендации

Подтверждённый в проверенном дереве кандидат: неэкспортируемый App.adminUsers server.go:449, нет callers, маршрут /api/admin/users зарегистрирован на userPages server.go:172. Это старая выдача до 200 пользователей; не удалена. Проверить дополнительные ветки/сборки перед будущим удалением. discoverySource.Int63 и floodQueue.Less/Swap выглядят как single-mention, но нужны интерфейсам math/rand.Source и heap.Interface — не мёртвые. CSS legacy classes — только кандидаты до DOM/visual coverage.

Начать с R1, затем небольших чистых компонентов R2/R3. Не начинать с полной нормализации inventory, процедур на каждый игровой шаг, удаления advisory lock или новой сети микросервисов. Доказательства важнее «красивых папок». Обнаруженный q/search дефект и риски БД оформлять отдельными разрешёнными задачами, не исправлять во время перемещения.

## Р. Оценка переноса части логики из Go в PostgreSQL

Приоритет решения: CHECK/FK/UNIQUE → один SQL/CTE или корректная Go-транзакция → узкая функция, если измерена польза → процедура/trigger только со специальным обоснованием. Существующие SQL-файлы уже используют CTE/UPDATE RETURNING (upgrade terminal.go:145–174); не вся атомарность требует PL/pgSQL.

| Операция | Файл/функция | Сейчас PostgreSQL | Проблема/возможность | Механизм | Причина | Польза | Риски | Сложность | Решение |
|---|---|---|---|---|---|---|---|---|---|
| credits debit/credit | economy.go debit/credit | conditional UPDATE + tx | direct SQL callers неодинаковы | единый Go primitive + UPDATE RETURNING; SQL function лишь после замеров | логика простая | единые overflow/errors | fee/ledger/retry changes | средняя | оставить orchestration Go |
| передача credits | economyCommand/order_buy, globalMarket | несколько guarded updates в tx | round trips, lock order | единая Go tx; optional узкий transfer SQL | нужен единый порядок locks, не новый game rule | меньше повторов | recipient cap, deadlocks, ledger reason | высокая | исследовать функцию после baseline |
| перемещение cargo | logistics/storageAccess | JSON read/modify/write и locks | отсутствие DB qty guarantees, целиком row JSON | CHECK/normalized quantity rows позже; batch SQL | constraints прежде procedural code | целостность и меньше round trips | ownership и миграция JSON | высокая | сначала контракт inventory |
| резерв сектора | colonyAction create | partial unique + INSERT ON CONFLICT | уже атомарно | оставить index/SQL | проще процедуры | надёжность независимо от клиента | смена predicate ломает аренду | низкая если без изменений | не переносить |
| внесение вклада | colonyAction/planetReceived | debit/store + contribution + project + receipt в tx | повтор много SQL | Go unit of work; optional узкая DB-функция | права/источник/стадия — один сценарий | меньше вызовов при измеренной цене | скрытый policy, обход roles | высокая | пока Go tx |
| возврат/отмена | refundContribution/cancelColony | tx, savepoint, refund claims | длинные циклы по contributors | set-based регистрация refund rows, затем guarded delivery | уменьшить work, сохранить pending policy | короче tx | rounding/core/partial source/overflow | высокая | исследовать дополнительно |
| завершение колонии | advanceColonies | locks, last_tick, unique colony, notification в tx | обработка всех проектов общим tick | constraints + guarded UPDATE/CTE, Go scheduling | цикл не принадлежит БД | меньше round trips | атомарность с tick и double events | высокая | Go, SQL узкие операции |
| корпоративная membership/role | companyAccess/corporationCommand | joins + owner/permissions в Go | company_role FK не гарантирует company_id сам по себе | composite constraints после анализа nullable semantics | структурное правило ближе к данным | защита от будущих writers | текущие SET NULL/cascade и founder | высокая | ограничения, policy Go |
| colony/project owner correspondence | schema120, advanceColonies | отдельные FK и XOR | нет DB равенства owner/project/sector | schema design/composite FK/единая authoritative source | убрать противоречивые копии | независимая целостность | FK nullable/update history | высокая | исследовать, не trigger по умолчанию |
| financial ledger | ledger_credit_change | AFTER UPDATE trigger | app может забыть explicit log | оставить узкий trigger | audit покрывает все writes | консистентная история | row overhead, reason convention | низкая | оставить текущий принцип |
| денежный диапазон | enforce_credit_ceiling | BEFORE triggers, Go guard | дублирование параметра | будущий CHECK после очистки grandfathered values | правило новой строки просто | прозрачная validation | текущая функция допускает понижение legacy >cap; CHECK изменит поведение | высокая | только после согласования совместимости |
| уведомления | event_notification и явные INSERT | trigger + приложения | два пути доставки | явная append/event mapping, сохранять нынешний набор видов | не side effects на каждый UPDATE | понятный lifecycle | duplicate/missing events | средняя | не переносить награды/колонии в triggers |
| сводки ресурсов/проектов | workbench/colonyList | queryMaps и subqueries | повторное формирование/GET fan-out | обычный SELECT; VIEW для действительно общего read model | view не кэш | меньше копий SQL | scopes/permissions, no speed guarantee | средняя | сначала профилировать |
| materialized summaries | будущие dashboards | отсутствуют | нет измеренной дорогой агрегации | matview пока не нужен | refresh/staleness лишние | пока не доказана | блокировки и актуальность | средняя | отложить |
| очередь workers | planner.advanceAutomation | predicate ORDER BY + global lock | масштабирование записи | SKIP LOCKED/lease только с per-ship ordering/fencing | queue correctness важнее скорости | будущий параллелизм | нарушенный приоритет и текущий груз | высокая | отдельное исследование |
| создание функций/миграций | openDatabase | повтор исполнения embedded SQL | нет migration ledger | versioned migrations + privileges | развёртывание не runtime gameplay | predictable upgrades | пропуск старого bootstrap | высокая | отдельный этап R5 |

Не переносить в PostgreSQL: HTTP, session transport, UI, WebSockets, input, пути/движение персонажей, physics/collision, активный sector loop, географический генератор, внешние сетевые вызовы, игровые timers. Сохранение результата и инварианты данных остаются задачей БД. Время scheduler не заменять pg_cron (он не установлен и не нужен первой нагрузке).

Новые triggers не рекомендуются по умолчанию. Процедура с самостоятельным COMMIT внутри общего сценария несовместима с ожиданием atomic rollback в Go. SQL/PLpgSQL functions могут уменьшить сетевые round trips, но всё равно выполняют внутренние SQL и locks; гарантия «один вызов быстрее» требует измерений. Не перегружать единственную PostgreSQL высокочастотной симуляцией вместо памяти Go.

Предлагаемое хранение SQL (только план): migrations/NNNN_description.sql с checksum; db/functions/economy_*.sql и db/functions/colonization_*.sql как исходники, из которых миграция фиксирует конкретную версию; db/views/ только по потребности; integration SQL-contract tests. Не выполнять latest function files независимо от версии миграции.

Оставить одну выбранную игровую схему и префиксы объектов: economy_, colony_, inventory_. Отдельные economy/colonization схемы усложняют нынешний DB_SCHEMA, search_path и права без доказанной пользы. Для новых функций — SECURITY INVOKER по умолчанию и EXECUTE только нужной серверной роли. SECURITY DEFINER допустим лишь с отдельной необходимостью, фиксированным доверенным search_path/полной квалификацией и запретом CREATE недоверенным ролям. Браузер не получает доступ к PostgreSQL.

Дополнительные индексы: не добавлять GIN на весь cargo без запросов, использующих такое условие. Оценить FK lookup/cascade пути и очереди на реальных объёмах. Для ships_identity выяснена реальная FK-зависимость; статистика scans на почти пустом экземпляре не основание удаления. Проверять EXPLAIN на репрезентативных копиях позже с отдельным разрешением, учитывая write amplification каждого индекса.

## Приложения

- files.csv — каждый Go-файл, строки и функции;
- functions.csv — все объявления с диапазонами, включая тесты;
- logical-call-edges.csv — лексическая карта кандидатов межфайловых вызовов, не compiler-exact callgraph;
- source-references.md — ссылки на рабочие функции, закреплённые на проверенном commit;
- postgresql-inventory.json — прочитанные таблицы, ограничения, индексы, функции и триггеры;
- diagnostics.txt — команды и итоги безопасных проверок.

Все предлагаемые деревья/миграции/индексы/тесты в отчёте — предложения. Реализация ни одного этапа не начата. Дальнейшие действия — только по отдельной команде пользователя.
