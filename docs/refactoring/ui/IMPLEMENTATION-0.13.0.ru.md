# Завершение рефакторинга M1–M14 · Astra Journey 0.13.0

Дата: 29 октября 2026. Основание — M0 (`225f5c5087aa93ae519a7cd7dfe05e3380994372`) и сохранённый WIP (`2e47e993005b23377e0050b0a18e3b40480a57f3`). После возобновления пользователь поручил закончить весь план. Прежние промежуточные остановки из исторического плана к этой работе не применялись.

Клиент переведён на native ES modules с разделением инфраструктуры, общих компонентов и игровых экранов. Старые плоские JS-файлы заменены соответствующими модулями; второй набор реализаций не оставлен. Сохранены терминальный стиль, HTTP API, серверная авторитетность, игровая экономика, исходная география и прежние ключи пользовательских настроек. Новой игровой SQL-миграции в выпуске нет.

## Результат по этапам

| Этап | Выполненное изменение | Подтверждение / граница |
|---|---|---|
| M1 · async/session | `RequestScope`, epoch/generation, scoped view; Users, Gameplay, Benchmark, Corporations и выдача ресурсов проверяют актуальность до DOM. Поиск Users инвалидирует поколение сразу при вводе. | Обратный порядок поиска, поздняя ошибка, закрытие, новый аккаунт, потеря admin-роли и user editors проверены браузером. Abort не считается отменой уже принятой сервером мутации. |
| M2 · HTTP/data | Общий ApiClient/ApiError, один accepted snapshot из семи endpoint; world metadata с индексом и явным in-flight dedup; personal station names вынесены из транспорта. | Проверены не-JSON HTTP failure/status, отсутствие retry мутаций, частичный snapshot failure и неизменность raw station DTO. Общего TTL-кеша всех GET нет. |
| M3 · окна | Registry получил явный destroy/lifetime, transient policy, освобождение observer/listener/timer, обновление title. Ship/cargo layout принадлежит Fleet. | Проверены повторный confirm, освобождение transient DOM/resize listeners, dirty editor, logout, drag/resize/pin/geometry/Escape и узкий viewport. |
| M4 · overlays | Общая геометрия/host для menu, picker, tooltip; resize/scroll/visualViewport, modal top layer и async callback. Один tooltip controller для plain/rich помощи. | Проверены вложенное меню, клавиатура, picker после shrink, popup внутри native modal и ошибка async callback. Policies разных overlay не слиты. |
| M5 · actions/forms | Один action wrapper разделяет pending и desired disabled; защищены auth submit и header actions. Feature-owned FormState восстанавливает draft без команды. | Проверены disabled после callback, ошибки popup action, сохранение ship rename при refresh без mutation. Payload и суммы проверяет Go. |
| M6 · preferences | Централизованы read/write/remove, plain-string compatibility, fallback при denied/corrupt storage. Сохранены defaults и области старых ключей. | Проверены reload «Карт», очереди кораблей, разные аккаунты и фильтры режимов, старые строки и недоступный storage. Не вводилась произвольная массовая смена scope. |
| M7 · RU/EN | Общие number/quantity/credits/scientific/date/percent helpers, параметры сообщений, локализованные accessibility labels. | Проверены 0/negative/unknown, малые научные значения, RU/EN и отсутствие округления DTO. Имена игроков, корпораций и заметки не переводятся. |
| M8 · CSS | `style.css`/`terminal033.css` объединены в `styles/terminal.css`, переменные — в `styles/tokens.css`; убраны 49 перекрытых деклараций в проверенных повторных component buckets и неиспользуемые hologram-window selectors. | Вычисленные стили контрольных desktop/narrow сцен до/после совпали; браузер проверяет тонкие кнопки и квадратный ID. Существование нескольких media/state rules не объявлено дефектом. |
| M9 · составные UI | Общие Tabs/Table/Pager, отдельный cursor helper; ResourceTile применяется в cargo/logistics. | Страницы Users/Research/Ideas перенесены, рынок имеет associated panel/keyboard. Source, selection, стоимость и физическая доставка остались доменными. |
| M10 · ESM/Fleet | Один module entry; явные импорты, Fleet вынесен вместе со stats/activity/presentation/layout. | Проверяются граф импортов, отсутствие неявных globals и обратных зависимостей. Inline модель и отдельный трюм проверены. |
| M11 · рынки | Отдельные local/global/orders/data/delivery/trade-form controllers, общие tab/state/scope; верфи подключены адаптером. | Все вкладки, один panel, reservation без второй таблицы, ожидаемый payload, stale tab response, keyboard. Серверные reserve/collect/price/docking contracts не изменялись. |
| M12 · корпорации/admin | Самостоятельные контроллеры пользователей, gameplay, benchmark, galaxies, grant, корпораций/flags/permissions, идей. | Порядок founders/academy/player corps; потеря роли и приватный DOM. Отдельный bugfix: поиск grant отправляет `q`, соответствующий Go API, вместо ошибочного `search`. |
| M13 · сцены | Visibility-aware galaxy loop с прежними 100 ms; read indexes; private caches очищаются при logout. Lazy planetary loader, terrain renderer, bounded public PNG/SVG cache. | Проверены независимые map filters, пять типов поверхности, lazy six PNG, failure/retry и поздние картинки после выхода. Канонический sampler, география, радиус подписей 160 и максимум 48 сохранены; полноценный нагрузочный 10k benchmark здесь не проводился. |
| M14 · очистка/CI | Удалены заменённые script files, domain adapters из generic UI, unused imports и старый TODO wrapper. Добавлены текущий каталог/guide, архитектурная проверка, CI с disposable PostgreSQL, версионная Windows-сборка. | 45 browser/core/architecture проверок без TODO; полный обычный Go integration и отдельные race-сценарии. Исторические M0-документы сохранены. |

## Что это даёт при дальнейшей разработке

Общий код UI не знает о цене корабля, ролях корпорации, preview галактики или выдаче ресурсов. Новый игровой раздел использует существующие windows/actions/pickers и хранит свою форму/запросы отдельно. Изменение одного рынка больше не требует редактирования большого общего `renderMarket` со всеми вкладками. Форматирование не меняет данные, а обновление страницы не воспроизводит пользовательскую команду.

Регистры `ui/runtime.js` и `features/runtime.js` сохранены как однократная сборка действующих компонентов/портов. Это не утверждение о полном устранении любой связности приложения. Актуальные механизмы и их владельцы перечислены в [CATALOG.ru.md](CATALOG.ru.md).

## Подготовка к двум Go-службам

Добавлен `internal/platform/postgres`: проверка/quoting `DB_SCHEMA`, pool-wide search_path, подключение/ping/закрытие при ошибке и узкий `DBTX` contract для существующей транзакции вызывающего. Пакет не импортирует HTTP, frontend, игровой server или simulation.

`postgres.Open` **не создаёт схему, таблицы, мир, миграции или tick**. Космический `internal/server/openDatabase` вызывает его и затем выполняет прежнюю инициализацию в том же порядке и с прежними блокировками. Проверены два физических соединения с точным search_path и отсутствие создания даже запрошенной отсутствующей схемы обычным подключением.

Это подготовка общей инфраструктуры. Полноценная вторая служба, handoff высадки/инвентаря, отдельные DB roles и дальнейшее разделение Go-domain пакетов этим выпуском не объявляются реализованными. Почти весь существующий игровой Go-код пока остаётся в `internal/server`; будущие пакеты выделяются по конкретным ответственностям. [Single-writer контракт](../service-boundaries.ru.md) сохраняется: планетарная служба читает каноническую географию и не запускает вторую космическую генерацию/экономику.

## Выполненные проверки

| Проверка | Результат |
|---|---|
| Chromium, изолированные HTTP-фикстуры | **45 PASS, 0 FAIL, 0 SKIP, 0 TODO** |
| Полный `go test ./... -count=1` с одноразовой PostgreSQL 17 | **76 верхнеуровневых тестов PASS, 0 FAIL, 0 SKIP**; пакет server — 160.691 s |
| Целевые `-race` покупки последнего корабля, конкурентной/пакетной логистики и общего DB-пакета | **5 верхнеуровневых тестов PASS, 0 FAIL, 0 SKIP** |
| Go без DB env | Unit/embedded/API guards проходят; DB integration ожидаемо пропускается |
| `go vet ./...` | Прошёл |
| Контроль CSS | Вычисленные стили desktop/narrow совпали после завершения переходов |
| Визуальное ревью | [Планировщик](screenshots/desktop-planner.png), [корабль/трюм](screenshots/desktop-ship-cargo.png); синтетические данные, 1440×1000 |
| Windows amd64 | `CGO_ENABLED=0`, `-buildvcs=false -trimpath -ldflags='-s -w'`, embedded JS/CSS/PNG/SVG; новый EXE и SHA256 сохранены в downloads |
| Чистая копия | Отдельный detached worktree исходного commit `61717a4`, `npm ci`, повторные **45 PASS**; Windows EXE побайтно совпал с пакетом выпуска. Все прежние SHA256 прошли проверку |

Сохранённые журналы: [browser из чистой копии](../checks/refactor-0.13.0-browser.txt), [полный PostgreSQL](../checks/refactor-0.13.0-go-postgres.txt), [целевой race](../checks/refactor-0.13.0-race.txt). Финальная сборка отключает VCS stamp, чтобы промежуточный commit/dirty-флаг не менял EXE с теми же исходниками; исходный commit указан здесь, контрольная сумма — в downloads/SHA256SUMS.

Первый **полный** `go test -race ./...` не прошёл: достиг стандартного 10-минутного timeout, а сценарий 50 параллельных readers `TestPlanetConcurrencyPrivacy120` получил HTTP 500/403 при исчерпании контекстов под race instrumentation. Его нельзя выдавать за успешный или считать доказанно прежней проблемой без сравнения. Обычный полный прогон прошёл; затем отдельно прошли указанные конкурентные race-сценарии. Production deadlines не ослаблялись ради результата. CI воспроизводит полный обычный прогон и целевой race; это **не** полное race-покрытие планетарной нагрузки.

Browser fixtures не доказывают безопасность Go API или транзакционные инварианты. Реальные DB tests проверяют сервер отдельно. Покрытие не включает все игровые комбинации, реальные планшеты, Firefox/Safari, screen reader, долговременную нагрузку и промышленный стресс-тест 10k-систем. Снимки служат для ревью, а не pixel-golden тестом.

## Обновление и откат

Остановить прежнюю службу и запустить новый версионный EXE с прежними `DATABASE_URL`, `DB_SCHEMA`, `HTTP_ADDR`. Node/npm на Windows не нужны. Старые EXE и контрольные суммы сохранены. Сброс настроек браузера или перегенерация поверхности не требуются.

Откат на 0.12.5 — замена исполняемого файла при остановленной службе; выпуск не добавляет новой SQL-миграции. Существующий механизм bootstrap старой версии действует как прежде, поэтому обычная резервная копия БД перед обновлением остаётся разумной эксплуатационной процедурой. Исторический [WIP resume](../RESUME.ru.md) описывает состояние паузы, не текущую незавершённость выпуска.
