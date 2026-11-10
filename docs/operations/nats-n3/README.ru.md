# NATS N3 — Core NATS B1 для Chat

**Дата:** 10 ноября 2026, Екатеринбург (UTC+5). Перед изменениями выполнен fetch
`main`, база — `c2bde9e` (Chat 0.24.8, N2). Выпуск: **Chat 0.24.9**;
Cosmic **0.24.6** и Planet **0.24.4** не пересобирались.

Реализован четвёртый режим `b1`. `legacy` остаётся default; A1/A2 сохранены.
Разработка и проверки выполнены на изолированных PostgreSQL и TLS NATS.
Рабочие PostgreSQL, `cosmoNATS`, Windows Services, пользовательские TOML,
существующие store/streams и HTTP/WS не менялись. Подключение к
`192.168.111.44:4222` **не выполнялось**. N4/N5 не начинались.

## Результат и изменённые файлы

| Файл | Изменение |
| --- | --- |
| `internal/chatserver/nats_signals.go` | Core WakeSource, postcommit queue, подключение/переподписка, bounded buffers, отмена, диагностика |
| `nats_config.go` в том же пакете | Валидация TLS-профиля Chat и чтение ограниченных файлов CA/пароля |
| `nats_envelope.go` | Только chat.outbox.changed, envelope v1 ≤2 KiB и строгая проверка |
| `nats_secret_unix.go`, `nats_secret_windows.go` | Проверка доступа к password_file для соответствующей ОС |
| `server.go`, `dispatcher_config.go`, `signals.go` | Выбор B1, preflight конфигурации, версия 0.24.9 и сохранённый общий контракт |
| `internal/platform/configuration/{document,configuration}.go` | `[nats]` только для Chat, ENV mapping, относительные пути, strict/exclusive источник |
| `internal/chatserver/nats_test.go`, configuration/dispatcher tests | TLS/auth/ACL, malformed/duplicate, bounded queues, config, отмена handshake |
| `internal/server/integration/chat_n3_test.go` | COMMIT/rollback/UUID/SQL failure, WS, broker down/restart/loss, игровые ACL и rollback |
| `chat_n1_test.go`, `chat_n2_test.go` в integration | Общие test helpers для сохранённых проверок A1/A2 и новых B1 сценариев |
| `internal/testkit/natsfixture/fixture.go` | Только тесты: временный loopback NATS, случайные секреты, TLS CA/SAN, точный Chat ACL |
| `.github/workflows/chat-n3.yml` | Изолированный функциональный CI, закреплённый NATS binary с SHA256; без benchmarks |
| `go.mod`, `go.sum` | Официальный `nats.go v1.55.0`, его зависимости |
| configs, ENV example, README, инструкции, release-notes, downloads | Описание включения/отката, только новый Chat EXE и checksum |

**Не изменены:** EventDispatcher, WSRegistry, RecipientResolver, SQL metadata
reader, `internal/chat` и его ACL/transaction/idempotency/journal правила,
Cosmic/Planet runtime, SQL schema/migrations/roles и browser code.
Новых SQL-миграций, таблиц, индексов и GRANT нет. A2 UP для B1 не нужен.

Минимальные версии зависимостей, выбранные Go MVS для nats.go: compress 1.20.0,
nkeys 0.4.16, nuid 1.0.1; x/crypto 0.57.0, x/sys 0.48.0, x/sync 0.23.0,
x/text 0.42.0. nkeys — транзитивная зависимость официального клиента, **NKey/JWT
авторизация адаптером не используется и не настраивается**. `.creds` не читается.
Go directive остаётся 1.26. Старые Cosmic/Planet EXE не меняются от go.mod.

## Путь сигнала и жизненный цикл

1. Прежний `Service.ObserveCommits` вызывается после успешного SQL COMMIT.
   При rollback/ошибке и cached UUID replay нового postcommit вызова нет.
2. `AfterCommit()` не делает сетевого I/O и **не будит локальный dispatcher**.
   Он кладёт token в очередь публикации capacity=1, объединяя повторы.
   Если источник offline/closed, hint отбрасывается, данные остаются в SQL.
3. Один supervisor публикует Core NATS message только в
   `cosmos.v1.signal.chat.outbox.changed`. Subscribe обычный, без queue group,
   request/reply, JetStream и сохранения stream/consumer.
4. Принимающий callback проверяет subject, размер, JSON/envelope и reply.
   Только валидный сигнал ставит wake в очередь capacity=1.
5. Прежний общий EventDispatcher читает PostgreSQL, определяет адресатов через
   прежний SQL RecipientResolver и ставит обычный generic `sync` в WS mailbox.
   Никакое поле NATS не становится игровыми полномочиями.

Профиль исходящего envelope:

```json
{
  "schema_version": 1,
  "signal_id": "UUID",
  "source": "chat",
  "kind": "chat.outbox.changed",
  "occurred_at": "2026-11-10T06:00:00Z",
  "data": { "hint": "outbox" }
}
```

Нет тела/части сообщения, пользователя, токена, DSN, причины наказания или ID
Private. Время исходящего сообщения — UTC. Для входящего проверяются version,
source, kind, UTC time, data.hint, неизвестные поля, хвост JSON и размер ≤2048
bytes. UUID signal_id проверяется, если присутствует; по общему контракту он
не обязателен для этого hint, исходящий адаптер всегда его создаёт. Непустой
reply отклоняется, отвечать адаптер не умеет. Невалидные сообщения отбрасываются
со счётчиком; игры/SQL они не изменяют. Периодическая сверка остаётся fallback.

Недавние signal_id дедуплицируются в кольце до 1024 элементов; отсутствие/сброс
кеша допускает лишний generic sync, но не повтор игрового действия. Очередь
подписки ограничена 64 сообщениями / 128 KiB. Все callback/commit очереди bounded
и nonblocking, dispatcher не пишет в WS напрямую.

Официальный клиент работает с `NoReconnect`: **автоматическим reconnect
управляет один supervisor адаптера**, создающий свежую connection/subscription,
а не два конкурирующих reconnect loop. Настроенные 1–3 TLS адреса пробуются
клиентом с ограниченным connect_timeout на попытку; discovery других серверов
отключён. После регистрации SUB выполняется Flush PING/PONG **до** initial
snapshot. Если первая попытка неудачна, Start сообщает degraded и включается
SQL fallback; supervisor продолжает попытки с reconnect_wait и jitter до 20%.
Игровой HTTP/WS сервер не ждёт подключения источника и не теряет readiness.

После повторного Subscribe+Flush источник запрашивает независимый reconcile;
общий диспетчер заново сверяет текущее состояние SQL. Reconnect buffer клиента
отключён: адаптер не копит бесконечный backlog. `Publish` success означает
принятие в клиентский буфер, **не** receipt и не гарантию доставки Core NATS.
Счётчик `published_hints` имеет именно такую семантику.

При остановке отменяется supervisor, подписка и connection закрываются. Dialer
привязывает и начальный INFO/TLS/auth socket к context: незавершённый handshake
отменяется, а не удерживает остановку до большого timeout. Старые callbacks
передают fault только в mailbox своей connection generation и не меняют health
нового подключения. Drain долговечных сообщений отсутствует: это только hints,
уже подтверждённые данные остаются в PostgreSQL.

## Безопасность, fallback и конфигурация

TLS всегда включён, минимум 1.2, RootCAs из указанного CA PEM; имя/IP берётся из
TLS URL и проверяется по сертификату. `InsecureSkipVerify`/permissive callbacks
не используются. Static username и service_identity на N3 — **chat**.
Credential не передаётся в URL, CLI args, inline TOML или ENV password.

`password_file`: обычный файл ≤4 KiB, UTF-8 (поддерживается BOM и один конечный
CRLF/LF), без пустого пароля/NUL/внутренних переводов строк. CA bundle ≤1 MiB,
PEM CA certificates с проверкой BasicConstraints и срока. На Unix парольный
файл не может давать group/other доступ (0400/0600). На Windows проверяется
DACL: запрещён null DACL, доступ Everyone/Users/Authenticated Users/Anonymous
и неподдерживаемые ACE. Это **не полный аудит** пользовательских/domain групп,
владельца, родительских каталогов и всех эффективных прав; нужен `icacls` review.
Файлы читаются при запуске B1, горячая ротация не реализована.

Ошибочные локальные параметры/нечитаемые secret/CA/плохой PEM — безопасная ошибка
конфигурации до старта службы. Ошибки удалённого broker диагностируются отдельно:
`tls_verification_failed`, `authentication_failed`, `authorization_failed`,
`slow_consumer`, `network_unavailable`. В логах нет raw error, паролей, тела
сигналов, private ID или credential-bearing URL. `last_error_code` относится
к текущему состоянию и очищается после регистрации; накопительные
`tls_failures`, `authentication_failures`, `authorization_failures`,
`network_failures` сохраняют историю отказов.

Source health не заменяет Cosmic/Planet/DB readiness. При временном отказе
broker/сети, неверном remote auth/ACL или ошибке публикации SQL COMMIT не
отменяется; Chat работает через прежний shared polling 10s / degraded 2s.
Момент обнаружения тихой потери TCP зависит от ping/сети (ping 5s, 2 outstanding),
поэтому это не жёсткий end-to-end SLA. Сканирование и state refresh независимы
от получения Core сообщения.

Сохранён независимый SQL refresh каждые 30s из N2: поздний smaller-ID COMMIT
за пределами overlap 60s не оставляет текущее UI состояние навсегда устаревшим.
Это eventual current state, не replay каждого hint и не финансовая доставка.
SQL auth/scope ~2s, exact WS lease generation, Private, company fences и
fresh Planet presence продолжают действовать независимо от источника.

Добавлять только в **отдельный Chat TOML**, не в конфиги Cosmic/Planet:

```toml
[chat]
# Добавить в существующую секцию, не создавать второй [chat].
dispatcher_mode = 'b1'

[nats]
enabled = true
service_identity = 'chat'
servers = ['tls://127.0.0.1:14222'] # исключительно изолированный тест
username = 'chat'
password_file = '../secrets/chat.password'
ca_file = '../certs/ca.pem'
connect_timeout = '2s'
reconnect_wait = '1s'
```

Существующие PostgreSQL/HTTP/Origins/erasure_journal сохранить. TOML пути
считаются от CONFIG_FILE, в ENV нужны абсолютные. ENV: NATS_ENABLED,
NATS_SERVICE_IDENTITY, NATS_SERVERS (через запятую), NATS_USERNAME,
NATS_PASSWORD_FILE, NATS_CA_FILE, NATS_CONNECT_TIMEOUT, NATS_RECONNECT_WAIT плюс
CHAT_DISPATCHER_MODE=b1. С выбранным CONFIG_FILE ENV **не подмешивается**.

Default — legacy, NATS disabled. В legacy/A1/A2 нет соединения NATS или чтения
его secret files. B1 требует enabled=true и полный корректный профиль. Старые
TOML без секции работают; неизвестные поля отвергаются. Новая `[nats]` намеренно
разрешена только Chat и отклоняется даже общей библиотекой для Cosmos/Planet.

## Фактически выполненные проверки

Стенд: Go **1.27.3 linux/amd64**, PostgreSQL **17.11**, отдельная `_test` БД
`cosmos_n1_test`, полные временные `update033_*` схемы, штатный GrantRuntime и
restricted NOLOGIN роли через SET ROLE. NATS Server **2.15.2**, официальный
Linux amd64 binary из N0, архив проверен SHA256. Клиент **nats.go 1.55.0**.
В транспортных тестах polling ускорен до 1s/500ms, NATS connect/reconnect до
500ms/250ms; production defaults остаются 10s/2s и 2s/1s. Период независимой
сверки 30s не уменьшался. Это ускорение функциональной проверки, не benchmark.
Каждый fixture создаёт свой loopback process, ephemeral port, случайные пароли,
временные CA/SAN/key и bcrypt test hashes. JetStream не включается.

У Chat только точный publish/subscribe subject. В автоматическом стенде есть
отдельный временный `n3_test_operator` для инъекции сигналов; его широкие права
не входят в профиль Chat или опубликованный auth template. Никаких секретов
стенда в Git/артефакте нет. Cosmic health для транспортных тестов локальный stub;
игровая схема настоящая, Spatial использует настоящий отдельный Planet process.

| Функциональная проверка | Результат |
| --- | --- |
| COMMIT → NATS subscriber → общий dispatcher → реальные WS | PASS |
| ROLLBACK и SQL INSERT failure → нет публикации и частичных данных | PASS |
| UUID replay → одна операция и один postcommit publish | PASS |
| Broker отсутствует до старта / выключен во время работы | PASS: HTTP ready, команды ACK/COMMIT и sync через SQL |
| Broker restart / новая подписка / forced reconcile | PASS |
| Core publish пропущен после COMMIT при здоровом subscriber | PASS: отдельный Service без hook имитирует crash-before-publish, SQL polling восстанавливает |
| PUB запрещён серверным ACL | PASS: диагностический счётчик, сообщение сохранено, другая WS видит его через fallback |
| Неизвестный kind/source/version/subject, oversized/unknown fields | PASS: discard, без state mutation; subject валидируется также unit тестом |
| Повтор сигнала, bounded queues и bounded ID cache | PASS |
| Private/NULL-channel invitation/expiry и outsider isolation | PASS: реальные B1 WS под restricted role |
| Corporation revoke/delete и durable cleanup receipt | PASS |
| Planet/Sector, реальный переход 1→2, stale presence | PASS; Planet ничего в NATS не публикует |
| Expired session, duplicate WS 409, stale/replacement generation | PASS |
| Slow WS и SQL read failure/recovery | PASS |
| Поздний COMMIT вне overlap | PASS: backdated lower-ID, реальный Core probe, независимый refresh и History; 65s hold уже проверен N2, здесь повторно не ожидался |
| TLS CA и SAN, неверный password, denied subscription | PASS, настоящий TLS/auth broker |
| Chat не может PUB/SUB Cosmic, Planet, unrelated и `$JS.API.INFO` | PASS; только проверка запрета, никаких JS API объектов/requests |
| Shutdown, включая остановку на зависшем initial INFO handshake | PASS |
| Restart B1 с прежним erasure journal; B1→A1 и B1→legacy при выключенном broker | PASS, история и lease-инварианты сохранены |
| A1/A2 и прежние Chat240 restricted/UUID/moderation/journal регрессии | PASS |

Полные результаты по командам:

- `go test -race ./internal/chatserver ./internal/platform/configuration`
  с TEST_NATS_SERVER_BINARY: PASS, финальный прогон **1.773s / 1.017s**.
- `go test -race ./internal/server/integration -run '^TestChatN3' ...`:
  основной набор 9 top-level сценариев PASS, **129.115s**.
- Focused publish-denied + A1/A2/Chat240 regression race: PASS, **63.899s**.
- `env -u TEST_DATABASE_URL -u TEST_NATS_SERVER_BINARY go test ./...`: PASS;
  это отдельный unit/compile запуск, DB сценарии в нём не считаются выполненными.
- `go vet` Chat/config/integration/testkit: PASS.
- Windows amd64 cross-build и PE/MZ: PASS. Реальный Windows runtime/service smoke
  и DACL под учётной записью владельца **не выполнялись**.

Длительности — время функциональных прогонов, не показатели скорости игры.
Тесты запускались необходимыми группами; не объявляется успешным benchmark
или полный production E2E. [CI для воспроизведения](../../../.github/workflows/chat-n3.yml)
содержит тот же функциональный набор и отдельную service PostgreSQL.

## Безопасный тестовый запуск

Самый воспроизводимый вариант — автоматические fixtures. На машине со своим
**disposable** PostgreSQL задайте TEST_DATABASE_URL базы `*_test` и
TEST_NATS_SERVER_BINARY (путь к отдельному официальному binary). Fixture owner
создаёт/удаляет временные полные схемы и restricted роли; не используйте рабочий
DSN. URL действующего брокера тестам не передаётся: они всегда поднимают свой.

```sh
go test -race ./internal/chatserver ./internal/platform/configuration -count=1 -timeout=2m
go test -race ./internal/server/integration -run '^TestChatN3' -count=1 -timeout=5m -v
```

Для ручного изолированного стенда подготовлены три **шаблона**, не установка:
[Core server](templates/server-core.test.config.example),
[точный auth ACL](templates/auth.test.config.example),
[отдельный Chat TOML](templates/chat.test.example.toml).
Нужна отдельная полная игровая `_test` база/схема, Cosmic/Planet с собственными
тестовыми портами, отдельный журнал, TLS CA/server key с SAN IP:127.0.0.1,
локальный секрет и его bcrypt hash. Не использовать production store/порты.
PKI/password подготовка описана в [N0](../nats-n0/SECURITY-ROLLOUT.ru.md);
для теста заменить SAN на loopback и все каталоги на отдельные тестовые.
Проверить `nats-server -t -c <test-config>`, затем запустить binary с этим
тестовым config. Клиент использует tls://127.0.0.1:14222, не рабочий 4222.
Не подменять этим template реальный `cosmoNATS`.

## Откат

Выбрать `dispatcher_mode='a1'` либо `'legacy'` в прежнем Chat TOML и
перезапустить **только Chat**, сохраняя DB_SCHEMA/DSN и erasure journal.
Можно дополнительно поставить `[nats] enabled=false`. Даже если enabled=true
оставлен с корректными параметрами, эти режимы не создают NATS connection и
не читают пароль. Отдельное изменение ENV не действует при выбранном TOML.

Никаких SQL DOWN, удаления сообщений, таблиц, NATS streams/store и повторной
выдачи прав для B1 нет. Если от теста A2 отдельно остался trigger, B1 не
устанавливает и не удаляет его; судьба A2 DDL — прежняя процедура DBA N2 после
выключения соответствующего listener и отдельного разрешения. Откат именно
на старый Chat 0.24.8 EXE также требует убрать новую `[nats]` секцию/выбрать
поддерживаемый им режим, иначе strict parser правильно откажет в старте.

## Перед первым подключением к Windows cosmoNATS — ещё не выполнено

1. Отдельно согласовать окно и конкретные правки существующего NATS. Узнать
   его версию, реальный config/include, service account и способ запуска через
   [N0 Windows inventory](../nats-n0/WINDOWS-CHECKS.ru.md). Сохранить резервные
   копии config/auth, существующий `D:/cosmosimulator/nats/data` и проверить
   восстановление. Не создавать второй production broker/SCM service/store.
2. Подтвердить настоящий server TLS: CA доверен клиенту, server cert действителен
   и имеет SAN IP:192.168.111.44 либо используемый DNS. Текущий plaintext порт
   не становится TLS от изменения URL. Проверить синтаксис config своей версией
   NATS и права service account на key/cert. Не отключать проверку сертификата.
3. Подготовить отдельный static login **chat** и protected password_file для
   реальной Chat service account; на сервере хранить bcrypt, клиенту дать
   исходный пароль. Воспользоваться интерактивной утилитой/скриптом N0, без
   пароля в командной строке, Git или чате. Учётная запись Cosmic остаётся
   **cosmos**; на N3 она не используется и не переименовывается.
4. Согласовать точный Chat ACL: PUB и SUB только
   `cosmos.v1.signal.chat.outbox.changed`; без прав Cosmic/Planet/JS/SYS/inbox.
   Существующих пользователей и их config не затирать тестовым auth template.
5. Проверить `icacls` секретного файла/каталогов под действительной учётной
   записью Chat: Read только ей, SYSTEM и администраторам; убрать broad grants,
   проверить custom/domain группы. CA доступен на чтение; private key брокера
   не раздаётся Chat. `.creds`, JWT/NKey не нужны.
6. Проверить firewall для client TLS порта и закрытый monitoring; не отключать
   firewall целиком. N0 содержит read-only login/TLS smoke без игровых изменений.
7. Только после подтверждения TLS/auth/ACL и отдельного разрешения подготовить
   **Chat-only** TOML с B1 и `tls://192.168.111.44:4222`, правильными absolute
   путями Windows и прежними PostgreSQL/Origins/journal. Cosmic/Planet TOML не
   дополнять `[nats]`, их EXE не обновлять ради этого этапа.
8. В согласованном запуске проверить source registrations/received_signals,
   TLS/auth/permission counters и игровую доставку, затем контролируемый отказ
   и откат. До такого smoke локальные результаты не подтверждают готовность
   Windows-инсталляции. Ни один пункт этого списка из облака не выполнялся.

## Отложено и граница следующего этапа

Не выполнялись пять минут broker outage (заменены коротким реальным stop/restart),
50/100/200/256 WS, p50/p95/p99, throughput, миллионы сообщений, CPU/RAM profiling,
сравнение A1/A2/B1, пределы брокера, production network/Windows smoke, HA.
Это **отложено**, а не PASS. Нет JetStream, Request/Reply, Planet→Chat или
Cosmic publishers, новых subjects/экономического протокола и multi-instance Chat.

N3 предоставляет безопасный опциональный транспорт и проверенный fallback.
Выбор механизма по производительности относится к отдельно разрешаемому N4,
дальнейшая интеграция Planet — к отдельно разрешаемому N5. Самостоятельного
перехода к этим этапам нет.
