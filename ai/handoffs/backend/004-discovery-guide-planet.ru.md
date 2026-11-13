# Первооткрыватели, Original C CMS и Planet Mode — 0.25.0

**Дата:** 13 ноября 2026. **Статус реализации:** A/B/C реализованы; адресные проверки пройдены. Рабочая ветка `feature/discovery-guide-planet` создана от `main` `470667919c06a871abd909dcd49213b9c67ceec4`. Подтверждение публикации исходников и релиза добавляется в конце после фактического выпуска.

Исходное [задание](../../prompts/backend/004-discovery-guide-planet/prompt.md) первоначально запрещало implementation без одобрения и merge/release. Владелец затем отдельно одобрил реализацию и прямо потребовал релизы из `main`; [точные сообщения и источники](../../prompts/backend/004-discovery-guide-planet/README.md). Это разрешение применено по правилу AGENTS.md о прямой новой команде владельца. Deployment и ручные операции над рабочей БД не выполнялись.

## Источники и границы

- Принятая архитектура: [PR #7](https://github.com/yomezza/astrajourney/pull/7), коммит `b365ce0980cae51b887c68ea42bb021e8cefd058`; [сохранённые документы 01–08 и пять review SQL](../../../docs/ideas/discovery-guide-planet/README.md). Исторические формулировки review сохранены отдельно от этого отчёта.
- Утверждённый визуальный эталон — **Original C**, [коммит `169675447d15cea823541c54fe723ceba88ad6b5`](https://github.com/yomezza/astrajourney/tree/169675447d15cea823541c54fe723ceba88ad6b5/docs/ideas/pilot-guide-prototype/original-c). Из прототипа использованы композиция и ограниченные CSS-правила, без demo-window, fixtures, mock-статей или процедурного `hologram.js`.
- Сохранён стек Go/PostgreSQL/vanilla JS, прежние игровые ID, renderer, A*, WS control lease, fencing, штатные Modules и ChatClient. Нового обязательного сервиса нет. БД проверялась только во временных схемах отдельной базы с суффиксом `_test`; fixture удаляет их после проверки.
- Chat backend, реальные конфиги, NATS Server и production deployment не менялись. Изменение размещения общего чата относится к JS, встроенному в Cosmic.

## A. Открытия и имена

`internal/discovery` хранит реестр систем/планет и историю. `navigation.AdvancePlayer` в одной SQL-транзакции с личным сканом закрепляет только вновь раскрытые планеты; завершённый личный `survey(source='survey')` — систему. Условный UPDATE и строковые блокировки исключают двух победителей: откат не оставляет права. Боты, покупные сведения и видимость корпоративной станции не присваивают права.

На новой установке registry заполняется **после** генерации world JSON. На старой — консервативный `legacy_unattributed`, без предположений по времени получения сведений. Реальная привязка `Star.Founder` и сохранённый founder key защищают основателей; фиксированных ID компаний нет. Retired-записи и история не удаляются.

Правила `internal/platform/naming`: UTF-8, Unicode NFKC, casefold, нормализация пробелов, 3–40 графем для системы/планеты, 3–60 для колонии. Управляющие/направляющие символы, URL, строки без букв, неподдержанные символы и локальный список запрещённых слов отклоняются. Уникальность: система в галактике, планета в системе, колония на поверхности. Добавлены `uniseg` для графем и прямое использование `x/text`; WebP использует `x/image`.

Пользовательское имя меняется один раз, по сохранённому UUID/digest/receipt. Иной body с тем же UUID конфликтует. Админ исправляет отдельной командой с причиной, не восстанавливая право. Обновления metadata, `galaxies.data`, версии мира и `planet_surfaces.name` атомарны под world lock через `World.Commit`. Геометрия/seed/surface data не переписываются. В world JSON сохраняются исходные стабильные каталожные коды; имена не используются вместо ID.

Кэш мира не удерживает mutex во время ожидания свободного соединения PostgreSQL; транзакционный читатель использует свою connection. Между Cosmic-процессами версия мира сверяется с БД с интервалом до 2 секунд. Браузер сравнивает `universe_revision`; Planet labels обновляются чтением authoritative visit с интервалом 15 секунд. NATS для этого не обязателен.

В существующих «Исследованиях» добавлена секция открывателей, кодов и названий; скрытые планеты в неё не попадают. Имена колоний редактируются в существующем окне проекта. Текущий личный владелец либо актуальный корпоративный `colonize` ACL проверяется при записи; обычное переименование — раз в 24 часа, админ исправление отдельно. Смена владельца не меняет историю и не оставляет право прежнему владельцу.

**Уточнение к архитектурному draft:** в реальной `planet_projects` нет столбца `name`. Он не добавлялся и не менялся; до первого имени используется `Колония <colony_id>`, затем отдельная `aj_colony_names`. Удаление поверхности с именованной колонией возвращает понятный запрет: FK/история сохраняются, автоматического уничтожения истории нет.

## B. База знаний и Original C

Новый пакет `internal/knowledge` обслуживает CMS, публикации, безопасный AST и медиа. Категории: RU/EN, до трёх уровней, порядок, скрытие/архив, запрет циклов, RESTRICT непустой категории. Статьи: отдельные неизменяемые редакции RU/EN, draft/preview, отдельные publish/unpublish, hide/archive/restore, восстановление старой редакции **новой** записью, упорядоченные изображения/подписи и связанные статьи. Каталог/related/media не раскрывают скрытые или неопубликованные материалы. EN без публикации получает целую русскую статью с явной отметкой, без смешивания языков.

На admin endpoints проверяется действительная сессия и `users.role=admin`, в мутациях роль перечитывается под блокировкой. `expected_revision` предотвращает потерю параллельной правки; UUID/digest фиксирует результат операции. `admin_log` содержит actor/operation/target/action/version, успешные операции и отклонённые транзакционные правки, без тел статей/файлов. На 409 интерфейс оставляет введённый текст и предлагает явно загрузить актуальную версию.

Markdown допускает headings h2–h4, абзацы/списки, выделение, code и типизированные внутренние ссылки. Raw HTML, внешние URL, произвольные пути и исполняемые схемы запрещены. Публичное чтение и preview используют один Go parser и один DOM renderer, без `innerHTML` на пользовательском контенте. Ссылки сохраняют UUID/stable game keys.

Растровые uploads: decoder/sniff, 8 MiB, 4096², две параллельные обработки, перекодирование в PNG с удалением исходных метаданных, UUID storage вне webroot, `pending → ready`, восстановление/карантин, ограниченный hourly scanner и отложенный GC. Загруженный SVG — **415** до появления проверенного sandbox rasterizer, как разрешено спецификацией. Встроенные игровые SVG выбираются по стабильному key/hash в `builtin-assets.json`; utility `cmd/astra-guide-assets` и release script проверяют manifest перед сборкой. Удалённый/повреждённый asset даёт заглушку, а не произвольный fallback path. [Backup/GC и фактические лимиты](../../../docs/operations/discovery-guide-planet.ru.md).

Reader остаётся `Modules.open('guide')` со штатной titlebar/move/resize. Реализованы Original C, 810×550 и 420×580, поиск включая опубликованный body CMS, категории, вкладки, избранное/недавние **по аккаунту в Preferences**, related/game links и сохранение прокрутки. Ресурсы, корабли, объекты и стаж продолжают брать действующие данные из кода/API; CMS не хранит копии изменяемых характеристик. Начальных фиктивных статей в БД нет: редактор публикует свои материалы. Игровые SVG и прежняя поддержка непрерывного 360°/reduce-motion сохранены.

## C. Планетарный режим

`features/planet/mode.js` открывает полноэкранный `#planet-host`. Окна космоса скрываются/inert без уничтожения DOM и изменения геометрии; тяжёлый workspace render приостанавливается. Общий ChatClient не дублируется. Если чат был на главной, тот же DOM/черновик временно переезжает в его существующее окно и возвращается обратно с прежней dock preference.

`ui/startup.js` координирует **существующие** `#startup-screen/title/message/retry` по owner/token/priority. Старый завершившийся запрос не скрывает новую заставку. Переходы имеют ограниченные ожидания и retry; browser Fullscreen API опционален, ESC сам по себе не завершает визит. F5 восстанавливает серверный визит, повторная вкладка использует существующий контроль epoch, старый контроллер не вступает в цикл перехватов.

Штатный возврат доступен в контекстном меню настоящего посадочного объекта. Planet фиксирует остановленную server-owned позицию и `position_version`, проверяет исходный surface/sector/landing, текущие visit/generation/epoch/fence и расстояние ≤5 единиц от входа. Выдаёт 30-секундное разрешение `return.prepare → return.authorized`, замораживая дальнейшее движение на срок разрешения. Cosmic принимает `/api/planet/return` только с совпавшими UUID/visit и действительным DB proof, атомарно потребляет его и запускает прежнюю безопасную reconciliation. Повтор не освобождает корабль дважды. Старый WS `leave`/прямой HTTP return без proof запрещены.

`/api/planet/recover` — отдельная диагностированная операция с аудитом. Причины определяет сервер: legacy, просроченная runtime lease, исчезнувшая исходная посадка, короткоживущая typed scene diagnosis. Транзиентная ошибка сети/БД не считается доказательством неисправной сцены. Закрытие вкладки, потеря сессии и logout оставляют визит disconnected и корабль зарезервированным до продолжения высадки/законного завершения.

## Схемы и API

Все пять production SQL подключены к **Cosmic migration runner**, не к Planet runtime:

| Файл | Назначение |
| --- | --- |
| `schema250.sql` | Discovery registry, uniqueness, immutable entitlement/name history |
| `schema251.sql` | Категории, статьи, immutable revisions/publication history, CAS versions, операции |
| `schema252.sql` | Media metadata/revision refs, состояния/hash/проверки storage |
| `schema253.sql` | Return authorizations/position version, typed recovery diagnostics, Planet schema 8 |
| `schema254.sql` | Colony names, composite colony FK, per-surface uniqueness, immutable history |

| Контракт | Поведение |
| --- | --- |
| `GET /api/system?id=…` | Прежние sites и metadata открытых объектов в `discoveries` |
| `POST /api/discovery/name` | `operation`, `object:{kind,star_id,planet_id}`, `name`; одно право открывателя |
| `POST /api/admin/discovery/name` | Те же поля плюс обязательная `reason`, отдельный admin correction |
| `POST /api/planet/action` | `action=rename_colony`, UUID `request`, colony `id`, `name`, `expected_revision`; admin `correction/reason` отдельно |
| `GET /api/guide/catalog` | `lang/q/category/cursor`, страницы 40, ETag, только доступные публикации + game refs |
| `GET /api/guide/articles/{uuid}` | Safe blocks, whole-locale fallback, доступные related/media |
| `GET /api/guide/assets`, `/api/guide/media/{uuid}` | Whitelist встроенных SVG / проверяемые PNG |
| `/api/admin/guide/categories`, `/articles` | GET metadata, POST actions; общий UUID/CAS contract |
| `POST /api/admin/guide/publish` | `publish/unpublish`, точные locale/revision/expected_revision |
| `POST /api/admin/guide/preview` | Тот же безопасный AST, без публикации |
| `GET /api/admin/guide/revisions?article_id=…` | История, публикации, изображения и related |
| `/api/admin/guide/assets`, `/media`, `/media/delete` | Каталог, GET/POST multipart uploads, soft-delete по UUID |
| Planet WS `return.prepare` | Одноразовое разрешение от исходной посадки |
| `POST /api/planet/return` | Потребление proof, `operation/visit`, идемпотентное завершение |
| `POST /api/planet/recover` | Диагностированное восстановление с отдельным аудитом |

Нет новых свободных параметров координат в HTTP return и нет второго Planet WS. HTTP origin/CSP не ослаблялись; multipart-разрешение ограничено точным admin media route, увеличенный JSON body — редактором/preview.

## Фактически выполненные проверки

1. `go test` для `internal/platform/naming`, `internal/knowledge`, `internal/worldstore` и адресного списка `internal/server/integration` — **PASS**: `TestUnicodeNames`, `TestSafeMarkdown`, `TestColdCacheSaturatedPool`, `TestDiscoveryClaimsAndNaming250`, `TestDiscoveryPersonalCompletion250`, `TestKnowledgePublishing251`, `TestKnowledgeMedia251`, `TestColonyNamingOwnerAndCooldown254`, `TestPlanetReturnProof253`, `TestPlanetResetVisits124`.
   - Реальная тестовая PostgreSQL; новая установка/post-seed, конкурирующий claim с rollback, два одновременных rename, чужое/повторное право/иной body UUID, founder/legacy, unchanged seed/cache.
   - Публикации RU/EN/fallback, body search, stale CAS, неизменяемость истории, цикл категорий в двух одновременных запросах, RESTRICT/archive, ACL, XSS/URL/SVG, sniff/re-encode, pending repair и missing-ready quarantine.
   - Near/far proof, прямой bypass, stale epoch, повтор/истечение, healthy recovery denied, logout/login без телепортации, release reservation один раз, сохранённые owner ACL/cooldown/история колонии.
2. Адресные прежние `TestPlanetHandoff180`, `TestPlanetTicketRecovery180`, `TestPlanetRestrictedRole180`, `TestPlanetMovement190`, `TestPlanetTransitions210`, `TestPlanetAccessRevocation180`, `TestPlanetPresence200` — **PASS** после обновления ожиданий нового защищённого протокола. Ограниченная роль Planet проверена с новыми grant, а не с DDL-привилегиями.
3. Настоящий браузер + Cosmic/Planet HTTP/WS + PostgreSQL: `TestPlanetBrowser180`, `TestPlanetTransitionsBrowser210`, `TestDiscoveryGuidePlanetBrowser250` — **3 PASS**. Два аккаунта/видимость движения, forced disconnect, перехват второй вкладкой, масштаб, переход/cancel/scene swap, F5 в соседнем секторе, отказ произвольного возврата; Original C 810/420, scroll/favorites/body search/fallback/admin preview, единственная заставка, сохранение космических окон и возврат через посадку. Ошибок JS в этих сценариях нет.
4. `node --test --test-concurrency=1 architecture.test.mjs lifecycle.test.mjs planet.test.mjs planet-art.test.mjs catalogs.test.mjs uuid.test.mjs` — **19 PASS**. Тестовый harness получил явный `planet/visit: none` и пустую CMS metadata; это не fake production API.
5. `node --test --test-concurrency=1 chat-layout.test.mjs architecture.test.mjs` — **3 PASS**, включая новый сценарий одного DOM/черновика/соединения докированного чата при переходе между режимами.
6. Повторены только изменённые после ранних проверок проверки: конкуренция naming/category + media repair, затем `TestKnowledgeMedia251` и `TestDiscoveryGuidePlanetBrowser250` — **PASS**. Не выдаём эти повторения за дополнительные независимые функции.
7. `go vet` затронутых Go-пакетов, `go run ./cmd/astra-guide-assets -check`, `git diff --check` — **PASS**. Эталонный автономный `test_prototype.py` — **PASS**, это отдельно от настоящих игровых проверок.

[Скриншоты и происхождение](../../../docs/design/discovery-guide-planet/README.md).

Полный многократный прогон всех Go/UI тестов, настоящий Windows runtime, полноценный нагрузочный эксперимент 50 Planet-клиентов, физическое backup/restore рабочей БД и запуск NATS в этом изменении **не выполнялись**. Старый opt-in `TestPlanetLoad230` приведён к fixture-only административной очистке между популяциями: она больше не выдаётся за нормальный player return. N6 lifecycle fixture использует новый proof; прежний N6 стенд в этой задаче не запускался. Полный CI и его актуальный статус — отдельны от перечисленных локальных PASS.

## Совместимость и эксплуатация

Выпуск содержит **Cosmic/Planet 0.25.0 для Windows/Linux amd64**; Chat 0.24.14 не пересобирается. Старые downloads и SHA256SUMS сохраняются. Сборки делаются из точного закоммиченного source ref с `source_dirty=false`; manifest включается в релиз.

Обновлять Cosmic и Planet вместе; сначала остановить обе службы и сделать backup, затем дать Cosmic выполнить миграции, выдать дополнительные права отдельной Planet-роли и запустить новый Planet. Новые media хранятся вне бинарника; каталог нужно включить в согласованный backup. Без SVG rasterizer пользовательский SVG остаётся недоступен. Политика retention имён не позволяет автоматически удалять именованные колонии вместе с поверхностью.

[Пошаговая инструкция](../../../docs/operations/discovery-guide-planet.ru.md). Следующему разработчику: сверять дальнейшие изменения с фактическими SQL/API и этой инструкцией, а не выполнять review SQL повторно; новые статьи создаются в admin UI, без переноса фиктивных материалов прототипа.

## Сохранённые сборки

Точная основа всех четырёх бинарников — [`0f4accd352abfac944f637c25776b3c6f10a39c2`](https://github.com/yomezza/astrajourney/commit/0f4accd352abfac944f637c25776b3c6f10a39c2), `source_dirty=false`, Go 1.27.3, amd64 v1, CGO отключён. [Build manifest](../../../downloads/astra-journey-v0.25.0-build.json) содержит полный source SHA и SHA-256 каждого файла.

Проверены Windows PE amd64 и статические Linux ELF amd64; обе Linux-сборки запущены в пустом временном каталоге без конфигурации и корректно отказали до подключения к БД/открытия игрового порта. Это проверка запуска executable, не отдельный нагрузочный стенд. `sha256sum -c downloads/SHA256SUMS` — **PASS**, включая все исторические сборки. Старые бинарники/суммы сохранены.

Перед интеграцией повторно получен `origin/main`: новых входящих коммитов относительно `4706679` нет. [Подготовленное описание PR](004-discovery-guide-planet.pr.md) отдельно от подтверждения его создания; номер PR не выдумывается.

## Публикация исходников и интеграция

Исходный коммит `0f4accd352abfac944f637c25776b3c6f10a39c2` и [коммит сборок `ce3f849d59af19da542eeaae04a1887a0d1d101b`](https://github.com/yomezza/astrajourney/commit/ce3f849d59af19da542eeaae04a1887a0d1d101b) отправлены в `origin/feature/discovery-guide-planet`. Создание PR через `gh pr create --body-file` отклонено сетевой политикой среды: `Post "https://api.github.com/graphql": Forbidden`. PR не создан; его подготовленное описание сохранено выше. Git fetch/push доступны. Прямое обычное слияние в main и релиз разрешены последней командой владельца, поэтому недоступность PR API не требует повторного согласования и не отменяет выпуск.
