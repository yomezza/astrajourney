# Проверки интерфейса Astra Journey

Тесты запускают production HTML/ES modules/CSS/SVG из `internal/server/web` на временном loopback-порту. API перехватываются; неожиданный endpoint, мутация, внешний URL или JavaScript exception проваливают сценарий. Адрес настоящего сервера не настраивается. Go, PostgreSQL и реальные аккаунты браузерным тестам не нужны.

Node 22+ и Playwright нужны **только разработчику и CI**. Windows-служба запускается одним EXE, без npm.

## Запуск с чистого checkout

```sh
cd tests/ui
npm ci --ignore-scripts --no-audit --no-fund
npx playwright install chromium
npm test
```

На Linux при отсутствии системных библиотек можно использовать `npx playwright install --with-deps chromium`; системные пакеты могут потребовать повышенных прав. CI выполняет это на одноразовом runner.

Если Chromium установлен:

```sh
CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium npm test
```

`CHROMIUM_NO_SANDBOX=1` нужен только в изолированной среде разработки, где Chromium иначе не запускается. По умолчанию sandbox включён. В тестах нет абсолютных путей среды Codex.

## Набор 0.16.0

| Файл | Число | Проверяемое поведение |
|---|---:|---|
| `baseline.test.mjs` | 16 | Startup/auth/retry, односимвольный пароль, ручные preferences, окна, planner per ship, карты/фильтры, корабль/трюм, руководство, picker/menu, RU/EN, toast, admin boundary, lazy planet assets, thin style/narrow viewport |
| `regressions.test.mjs` | 5 | Поздний Users response, повторный confirm title, picker resize, desired disabled после callback, HTTP status при не-JSON ответе |
| `core.test.mjs` | 5 | RequestScope, повреждённый/недоступный storage, ApiError/no mutation retry, public AssetCache/retry, научное форматирование без изменения DTO |
| `lifecycle.test.mjs` | 9 | Поиск A/B, close, другой аккаунт, потеря роли/user editors, bounded transient DOM/listeners, dirty/logout, modal popup/async errors, частичный snapshot, draft restore без команды |
| `markets.test.mjs` | 6 | Вкладки и один panel, reservation payload/отсутствие второй таблицы, stale tab, raw station snapshot, порядок корпораций, keyboard/ARIA |
| `planet.test.mjs` | 4 | Пять типов поверхности, lazy шесть PNG и cache, failure/retry, late assets после logout, неизменность географии, площадки/права и RU/EN |
| `architecture.test.mjs` | 1 | Импорты, browser globals, направления core/UI/features, один module entry |
| `logistics.test.mjs` | 3 | Склад → трюм без legacy globals, drag при polling, пакетный перенос ресурсов |
| `terminal-flows.test.mjs` | 11 | Отмена форм, станция доставки, обновление флота/picker, потеря прав и поздние ответы, отсчёт квот |
| `ideas.test.mjs` | 1 | Статусы идей: цвет только рамки, сохранение администратором и отображение игроку |

Итого **61 тест**, без TODO и скрытых ожидаемых ошибок. Пять диагностических требований M0 стали обычными регрессиями; прежнего `knownDefect` wrapper нет. `npm run test:baseline` и `npm run test:regressions` запускают соответствующие поднаборы.

Fixtures синтетические. Они повторяют необходимые DTO, но не реализуют игровую экономику и серверную авторизацию. Mock 403 не доказывает защиту Go API. Реальные anonymous guards, CSP/MIME и доставку всех embedded assets проверяет `internal/server/integration/ui_contracts_test.go`. Полномочия вошедшего игрока и транзакционные инварианты проверяются в DB integration.

## Настоящий терминал с Go и PostgreSQL

`live-scenarios.mjs` запускается только через `TestTerminalBrowser`, не входит в `npm test`. Go-тест создаёт собственную схему и пользователя, поднимает loopback httptest server и передаёт браузеру одноразовый nonce. Сценарий проверяет nonce перед входом и отклоняет нелокальный URL.

После установки зависимостей и создания **одноразовой** БД с настройкой `TEST_DATABASE_URL`, из корня:

```sh
RUN_TERMINAL_BROWSER=1 go test ./internal/server/integration -run '^TestTerminalBrowser$' -count=1 -timeout=5m -v
```

Проверяются 21 модуль на RU/EN и реальные действия: перенос, заправка, торговля, покупка корабля, планировщик, идеи, корпорация, выдача ресурсов, настройки колонизации, тик, уведомления. Без флага Go-тест явно пропускается. CI запускает его отдельно после npm test с PostgreSQL 17. Синтетический harness предоставляет только `window.__testApp` для инспекции и не создаёт старые `window.refresh`, `window.UI` и другие globals.

## Go и PostgreSQL

Из корня репозитория, без БД:

```sh
env -u TEST_DATABASE_URL go test ./... -count=1
go vet ./...
```

DB-тесты в этом режиме пропускаются. Полный прогон использует **отдельную одноразовую PostgreSQL-базу** с именем, заканчивающимся на `_test`. Тесты создают/удаляют схемы, некоторые обращаются к `public`; рабочую `DATABASE_URL` нельзя копировать в `TEST_DATABASE_URL`. Суффикс сам по себе не делает существующую базу безопасной.

После создания одноразовой базы и настройки `TEST_DATABASE_URL`:

```sh
go test ./... -count=1 -timeout=15m
go test -race ./... -count=1 -timeout=5m -run '^(TestConcurrentLogistics080|TestLastHullPurchase040|TestBatchLogistics100|TestOpenDoesNotInitializeGame|TestDatabaseConfig|TestColdCacheSaturatedPool|TestBenchmarkCompositionIsolation)$'
go vet ./...
```

Это те же команды, что использует CI с PostgreSQL 17. Второй запуск — целевой race, **не весь набор под race**. Ограничения полного race-прогона в этой среде записаны в [отчёте](../../docs/refactoring/ui/IMPLEMENTATION-0.13.0.ru.md#выполненные-проверки).

Общий production-пакет `internal/platform/postgres` уже используется космической службой. Его подключение не создаёт схему/мир; single-writer границы будущей планетарной службы описаны в [архитектурном контракте](../../docs/refactoring/service-boundaries.ru.md).

## Визуальная проверка и границы покрытия

`npm run capture` сохраняет синтетические сцены в игнорируемом `tests/ui/artifacts`. Проверенные изображения выпуска находятся в `docs/refactoring/ui/screenshots`. Это иллюстрации для ревью, а не pixel-golden assertions; анимация SVG и время footer могут отличаться.

Chromium не заменяет Firefox/Safari, реальные планшеты и screen reader. Проверены выбранные сценарии, а не все комбинации preferences и административных мутаций. При новой функции добавляются проверки её реального риска, без тестов, повторяющих каждую строку реализации.

Планетарный live-сценарий `TestPlanetBrowser180` / `planet-live.mjs` дополнен двумя реальными аккаунтами, presence/movement, принудительным WS reconnect и заменой вкладки. `services.test.mjs` проверяет окно администратора со статусами служб; `TestAdminServices200` проверяет API, доступ и соответствие runtime общей БД. Все live-тесты используют только nonce-marked сервер своего прогона и одноразовую `_test` БД.

`TestPlanetTransitionsBrowser210` / `planet-transition-live.mjs` проверяет предложение/отмену/подтверждение, смену сцены, reload и возврат из соседнего сектора без собственного модуля. Это тот же guarded disposable browser runner; fixture-only приближение к ingress не является игровым API телепортации.
