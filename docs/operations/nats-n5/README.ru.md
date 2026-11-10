# NATS N5 — Planet → Core NATS → Chat

Дата: **10 ноября 2026**, Екатеринбург (UTC+5). Перед разработкой актуальный
`main` проверен через fetch: `c06af93`, Chat 0.24.9 после N3.
Выпуск **Planet 0.24.10 + Chat 0.24.10**. Cosmic остаётся **0.24.6**.

Реализован первый межсервисный сигнал — только
`cosmos.v1.signal.planet.sector.changed`. Рабочие PostgreSQL, Windows Services,
`cosmoNATS`, auth/config/store владельца не изменялись; к
`192.168.111.44:4222` не подключались. N4 performance decision не выполнялся,
N6 и последующие этапы не начинались.

## Что изменилось

| Компонент | Файлы и результат |
| --- | --- |
| Общий безопасный транспорт | `internal/platform/natscore/{config,session,nats_secret_unix,nats_secret_windows}.go`: TLS/CA/SAN, password_file, проверка identity, отмена dial/handshake и безопасные error codes вынесены из N3 для обеих служб |
| Контракт | `internal/platform/gamesignal/planet.go`: строгий envelope v1, stable operation UUID, metadata, предел 2 KiB |
| Planet publisher | `internal/planetserver/nats.go`: один worker, очередь 64, reconnect, TLS/static `planet`, diagnostics и shutdown |
| Planet composition | `app.go`, `config.go`, `run.go`, `transitions.go`: опциональная конфигурация, lifecycle и post-COMMIT hook перед RAM Join |
| Chat adapter | `internal/chatserver/nats_signals.go`, `nats_config.go`: два независимых subject, прежний outbox и bounded spatial dirty set |
| Chat dispatcher | `dispatcher.go`, `spatial_dispatch.go`: адресная SQL-перепроверка dirty users через прежние session/lease/ScopeStamp функции |
| Config loader | `internal/platform/configuration/`: Chat/Planet принимают свои `[nats]`, Cosmos по-прежнему отвергает; ENV/TOML не смешиваются |
| Тесты | `gamesignal/planet_test.go`, `chatserver/{n5_signals,spatial_dispatch}_test.go`, `planetserver/n5_join_probe_test.go`, `integration/chat_n5_linux_test.go`, configuration tests и расширение disposable natsfixture |
| Поставка | `.github/workflows/chat-n5.yml`, examples/docs, release-notes и две Windows EXE в downloads с SHA256 |

Не менялись `internal/planet/visits`, movement/pathing, epoch/fencing правила,
SQL ACL/GRANT, schema*.sql, Cosmic runtime/gateway, browser HTTP/WS и erasure
journal. Новых зависимостей go.mod/go.sum нет. Общие компоненты N3 переиспользованы,
а не скопированы в новый самостоятельный TLS/auth стек.

## Planet: публикация строго после COMMIT

Hook находится в `App.commitCrossing()` непосредственно после успешного
`visits.Transition()` и **до** `motion.Join()`. Transition возвращает успех
после COMMIT обновлений visit/character и transition receipt. Неподтверждённый,
отменённый либо ошибочный переход не доходит до публикации. Cached duplicate
confirm не создаёт второй переход/receipt. Ошибка RAM Join после COMMIT не
отменяет уже подтверждённое состояние: сигнал публикуется о настоящем SQL факте.

Сетевой вызов не выполняется в транзакции или пользовательском gate: enqueue
кодирует ограниченные metadata и делает nonblocking запись в очередь capacity
64. Если источник offline/closed или очередь полна, hint отбрасывается со
счётчиком. Ни drop, ни ошибка Publish, ни ошибка TLS/auth/сети не меняют
успешный результат перехода. Worker автоматически восстанавливает соединение,
но не обещает durable replay Core NATS и не копит бесконечный backlog.

TLS не ниже 1.2 с проверкой CA/SAN, отдельные static login/identity `planet`,
собственный защищённый password_file. Подписок у Planet нет. При NATS disabled
нет подключения, чтения его секретов или новых goroutines. HTTP readiness и
движение не зависят от брокера. При shutdown отменяются и начальный handshake,
и worker, соединение закрывается до завершения App. Диагностика — существующие
структурированные логи и Go probe `NATSDiagnostics()`, без нового HTTP endpoint.

## Контракт

```json
{
  "schema_version": 1,
  "signal_id": "transition-operation-uuid",
  "source": "planet",
  "kind": "planet.sector.changed",
  "occurred_at": "2026-11-10T07:00:00Z",
  "data": {
    "user_id": "42",
    "visit_id": "visit-uuid",
    "generation": "1",
    "connection_epoch": "9",
    "runtime_fence": "3",
    "surface_id": "7",
    "from_sector": 1,
    "to_sector": 2,
    "geography_revision": "0",
    "transition_operation_id": "transition-operation-uuid"
  }
}
```

UUID в примере обозначают настоящие canonical UUID. `signal_id` равен operation,
стабилен для подтверждённого перехода. BIGINT — канонические десятичные строки,
сектора — разные положительные int, timestamp UTC, payload ≤2048 bytes.
Неизвестные поля, версии/типы/subject, некорректные UUID, числа/переполнение,
не-UTC время и JSON tail отвергаются. Geography revision **может быть 0** по
существующей схеме: это обнаружено полным native тестом и учтено в кодеке.
Остальные перечисленные идентификаторы/epoch/fence положительные.

В payload нет session/token/hash, nickname, сообщения чата, credentials или
прав. Location metadata используется только внутри сервисного канала.
В диагностике нет user/visit/private IDs или raw payload/ошибок с секретами.

## Chat: текущая SQL authority вместо replay состояния

B1 подписывается на два точных subject. Старый `chat.outbox.changed` по-прежнему
будит общий outbox reader. Новый Planet envelope после валидации оставляет в
bounded dirty set **только user_id**, до 256 разных пользователей. Повторы
coalesce; при переполнении drop восстанавливается обычными scope checks.

На адресной обработке EventDispatcher:

1. Берёт только текущие handles отмеченных пользователей из WSRegistry.
2. Вызывает прежний RecipientResolver с targeted metadata: проверяет текущую
   SQL session, точный WS connection/generation и срок lease.
3. Для допустимых handles вызывает прежний `ScopeStamp()`. Он использует
   существующие `chat_scope_stamp` и `chat_lock_planet_presence`, включая
   active visit, current surface/sector/epoch, runtime lease/fence, свежесть
   checked_at и geography control. Новый SQL permission mechanism не создаётся.
4. Ставит только generic `sync` в mailbox действующего handle. Смена handle
   во время SQL check не позволяет старой регистрации получить уведомление.
   Ошибка SQL — fail closed, pending dirty users сохраняются для retry.

Поля sector/epoch/fence из NATS **не применяются** к БД, персонажу, permissions
или cached location. Старое событие может вызвать лишнюю перепроверку, но SQL
всё равно вернёт сегодняшнее состояние. Отсутствующая/stale presence означает
непространственный view, не разрешение из payload. Generic sync может сообщить
об отзыве view; каждый следующий HTTP/WS send/read повторно авторизуется.

Дедупликация до 1024 ключей различает subject и, для Planet, user_id: одинаковый
operation UUID разных игроков не подавляет чужую транзакцию. Dirty dispatcher
set также до 256, per-WS mailbox capacity 1. Повтор generic sync допустим;
exactly-once delivery или одно UI обновление не обещаются. Subscription buffers
ограничены по-прежнему 64 messages/128 KiB на subject.

Если новый Planet SUB запрещён старым ACL, это отмечается в
`spatial_subscription_failures`, но **не отключает** рабочий outbox B1. После
исправления ACL требуется reconnect/restart Chat для новой подписки.
`spatial_signals`, `spatial_dropped` и dispatcher `spatial_rechecks` — технические
счётчики без пользовательских labels; последний считает успешно поставленные
адресные sync после SQL проверки.

NATS не является readiness dependency. Прежние scope/session checks ~2s,
shared polling 10s/2s и независимый state reconcile 30s сохранены. После reconnect
B1 выполняет обычную полную SQL сверку. После restart WS получает bootstrap,
а не восстановление местоположения из последнего Core event. Global, Private,
Corporation продолжают работать при недоступных Planet и NATS.

## Реальные проверки P02-01—P02-10

Стенд: Go **1.27.3 linux/amd64**, PostgreSQL **17.11**, отдельная БД
`cosmos_n1_test` с полной временной игровой `update033_*` схемой; NATS Server
**2.15.2**, `nats.go 1.55.0`. Пароли/CA/server key случайные временные, loopback
порты, два разных NATS пользователя с точными ACL. Никаких production данных.

Главный тест компилирует **отдельные нативные Planet и Chat binaries с -race**,
запускает их как разные OS processes и соединяет настоящими TLS NATS/PG/WS.
Cosmic initialization/регистрация используют настоящий игровой fixture,
Cosmic health — локальный stub, Cosmic EXE не запускался и не менялся. Chat
SQL работает под существующей restricted NOLOGIN ролью через startup SET ROLE;
это не проверка production TCP LOGIN пользователя владельца.

| ID | Статус и фактически проверенный сценарий |
| --- | --- |
| P02-01 | **PASS**: cancel offer без события; настоящий rollback после visit/character UPDATE — незакоммиченный conflicting receipt блокирует INSERT до SQL deadline, нет частичного перехода или публикации |
| P02-02 | **PASS**: native Planet 1→2; перехвачен реальный Core envelope, сравнен с committed receipt/epoch; native Chat bootstrap показывает 2, sync адресован нужному игроку |
| P02-03 | **PASS**: HTTP history старого сектора и WS message.send из прежнего scope запрещены; второй игрок остаётся в секторе 1, история сектора 2 ему недоступна |
| P02-04 | **PASS**: cached confirm не создаёт второй receipt/event; duplicate signal coalesce/dedupe; unit проверяет отдельные пользователей и адресный SQL refresh |
| P02-05 | **PASS**: событие 1→2 после 2→3 не возвращает игрока назад; после restart Chat старый event с прежним epoch также не переопределяет текущее SQL состояние |
| P02-06 | **PASS** в ускоренном функциональном объёме: Planet стартует с выключенным broker; переход 5→6 подтверждается при NATS down; после restart broker обе службы переподключены, переход 6→7 снова даёт реальный Core event. Пять минут outage отложены |
| P02-07 | **PASS**: кратко приостановлен только собственный тестовый Planet process, checked_at сделан stale — Core hint не даёт пространственный доступ; затем процесс восстановлен. При совместном stop Planet+NATS Global/Private/Corporation работают, presence отсутствует |
| P02-08 | **PASS**: native Chat restart с сохранённым journal и новым WS; bootstrap из текущего SQL, после resume новый epoch и правильный сектор |
| P02-09 | **PASS**: отдельный Go TEST binary вызывает настоящий commitCrossing с преднамеренно закрытым приватным RAM Hub; SQL/receipt 4→5 подтверждены, RAM Join возвращает ошибку, реальный Core сигнал всё равно опубликован до Join |
| P02-10 | **PASS**: schema_version=9, oversized и malformed payload отвергаются кодеком/consumer со счётчиком, текущий scope не меняется, штатная сверка остаётся |

Дополнительно проверены:

- Реальная SQL операция 3→4 через тот же visits.Transition без composition hook
  имитирует crash между COMMIT и Publish при здоровом broker: Core event нет,
  Chat восстанавливает scope из SQL. Это инъекция пропуска публикации, не тест
  произвольной потери пакетов сетью.
- Planet может PUB только sector.changed, не может SUB или PUB Chat/Cosmic/JS;
  Chat не может PUB от имени Planet. Старый ACL без нового SUB не ломает outbox.
- SQL ошибка при адресной перепроверке не отправляет непроверенный sync,
  сохраняет retry; существующие N3 UUID/commit/rollback/ACL/restart и A1/A2
  session/lease сценарии пройдены после переноса общего транспорта.
- Проверены настоящие прежние PlanetTransitions210: сами переходы/epoch/fencing
  не переписаны. В частности, teardown старого epoch не должен переписывать
  новое committed состояние; native test учитывает это существующее правило.

P02-09 использует **только test binary**, без debug endpoint/флага в поставляемой
службе. Существующий Planet Sweep может сохранять валидный committed active
visit даже после RAM failure; Chat следует этим SQL правилам. N5 не меняет
семантику Sweep и не подменяет её предположением «RAM Join failed → SQL rollback».

Результаты команд: native race-сценарий **34.874s PASS**; source/config/codec/Planet
unit+races **PASS** (Chat 1.931s, Planet 4.017s, config 1.020s, codec 1.011s); отдельная интеграционная регрессия **109.862s PASS**;
`env -u TEST_DATABASE_URL -u TEST_NATS_SERVER_BINARY go test ./...` и `go vet`
**PASS**. В отдельном unit/compile запуске DB случаи не считаются выполненными.
Native/fixture transport интервалы ускорены (poll 1s/500ms, NATS 500ms/250ms),
30s state refresh и production defaults не менялись. Это функциональные тесты,
не измерения производительности. Эти durations относятся только к выполненным функциональным проверкам.

## Изолированный запуск и конфигурация

Самый простой воспроизводимый стенд —
[CI functional workflow](../../../.github/workflows/chat-n5.yml) или локальные
fixtures. Нужны собственная PostgreSQL БД `*_test`, TEST_DATABASE_URL и
TEST_NATS_SERVER_BINARY — путь к отдельному официальному NATS binary. Тесты
поднимают broker сами, не принимают адрес действующего cosmoNATS.

```sh
go test -race ./internal/chatserver ./internal/planetserver \
  ./internal/platform/configuration ./internal/platform/natscore \
  ./internal/platform/gamesignal -count=1 -timeout=2m

go test -race ./internal/server/integration \
  -run '^TestN5NativePlanetNATSChat$' -count=1 -timeout=4m -v
```

Native сценарий рассчитан на Linux, в том числе короткую SIGSTOP/SIGCONT инъекцию
только собственного процесса для свежести presence. Во временном test monitor
читается connz для подтверждения reconnect двух клиентов, не discovery служб.
Новых routes в игре нет. Результаты не являются Windows runtime smoke.

Для ручного стенда сохранены [Core server template](templates/server-core.test.config.example)
и [две точные роли](templates/auth.test.config.example). PKI и protected files
готовятся по [N0](../nats-n0/SECURITY-ROLLOUT.ru.md) с loopback SAN и отдельными
каталогами; production store/порты/конфиги не использовать. Два профиля:

```toml
# Отдельный planet.toml, после обновления Planet EXE. Остальные секции сохранить.
[nats]
enabled = true
service_identity = 'planet'
username = 'planet'
servers = ['tls://127.0.0.1:14222']
password_file = '../secrets/planet.password'
ca_file = '../certs/ca.pem'
connect_timeout = '2s'
reconnect_wait = '1s'
```

В **другом** Chat TOML — прежний B1, identity/username `chat` и его собственный
password_file. Общий TLS CA может совпадать, пароли — разные. Default NATS
disabled, старый Planet TOML без секции работает. CONFIG_FILE выбирает TOML
исключительно, ENV не дополняет его; относительные пути считаются от каждого
своего TOML. В ENV используются те же NATS_* ключи с собственными значениями
процесса и абсолютными путями. Нельзя назначать одной глобальной ENV identity
обеим Windows-службам. Cosmic секцию NATS не получает.

## Откат и совместимость

Planet: поставить `[nats] enabled=false`, перезапустить только Planet. Движение,
переходы, DB/current visit/receipt остаются прежними; Chat обнаружит изменения
штатным scope polling. Chat: выбрать A1 или legacy, сохранить journal и
перезапустить Chat. Эти режимы не требуют broker/его secret files. A2 также
сохранён, если его отдельно согласованный trigger установлен по процедуре N2.

Для N5 нет SQL UP/DOWN, новых GRANT, очистки сообщений или NATS store/streams.
Не удалять данные ради rollback. Возврат именно к старой Planet 0.24.4 EXE
требует убрать неизвестную ей `[nats]` секцию; возврат к Chat 0.24.9 сохранит
только outbox NATS, а пространственная актуализация останется через SQL polling.

В downloads и GitHub release только **Planet 0.24.10 и Chat 0.24.10**, прежние
версии сохранены. Cosmic EXE не собирался: schema/HTTP/gateway не менялись,
следовательно необходимости его обновления нет. PE/MZ и Windows amd64
cross-build проверены; реальные службы Windows владельца не запускались.

## Будущие Windows ACL и что осталось до рабочего подключения

Ничего ниже автоматически не применялось. Нужны отдельное согласование
владельца, проверенный TLS-ready broker, CA/SAN, файл каждого секрета и ACL
служебных учётных записей по N0/N3. Текущий LAN broker пока не подтверждён
как TLS-ready; смена URL не включает TLS на сервере.

Точные минимальные дополнения к существующему auth.config:

| NATS login | Publish allow | Subscribe allow/deny |
| --- | --- | --- |
| `planet` | только `cosmos.v1.signal.planet.sector.changed` | deny `>` |
| `chat` | сохранить только `cosmos.v1.signal.chat.outbox.changed` | сохранить outbox и добавить `cosmos.v1.signal.planet.sector.changed` |
| `cosmos` | не менять на N5 | не менять на N5 |

Не выдавать Chat право публиковать Planet/Cosmic, Planet — подписываться,
кому-либо — JS/SYS/inbox/произвольные wildcard subjects. Не заменять рабочий
файл целиком тестовым шаблоном и не менять существующий store. После применения
согласованных ACL переподключить Chat, проверить diagnostics и real transition
на отдельном стенде, затем Windows service smoke/rollback под настоящими
аккаунтами. Права файлов проверяются общими N3 helpers (Unix mode/Windows DACL),
но это не полный аудит custom/domain групп и parent directory ACL — нужен icacls.

До безопасного рабочего rollout остаются Windows TLS/login/permissions и
нативный service smoke, проверка резервных копий/rollback, отдельное решение о
производственной активации. Длительный outage 5min, нагрузка 50–256 WS,
p50/p95/p99, throughput, profiling/HA/предел брокера **отложены**, не PASS.
Request/Reply, JetStream, service discovery/registration, Cosmic events, прочие
Planet subjects, N6 и следующие этапы не реализовывались.
