# N6 — межсервисные Core NATS signals · Cosmic / Planet / Chat 0.24.11

Работа выполнена по разрешению только на изолированном стенде. Исходная база —
main после N5, commit a64ec62. Перед публикацией сохранены поступившие в main
из другого чата изменения исследований f9dd407 / 73dc4fe; Cosmic EXE пересобран
с актуальными embedded web assets. Игровые изменения исследований принадлежат
этим коммитам и не являются отдельным механизмом N6. Рабочие Windows службы владельца, PostgreSQL,
cosmoNATS, его конфиги/auth/store/SCM и LAN адреса не изменялись и не использовались.

N6 добавляет подсказки «перечитать PostgreSQL». Они не содержат полномочий,
текста чата, паролей, сессий или причин модерации. Core delivery остаётся
best effort / at-most-once. Получение сигнала не подтверждает игровую операцию.
Старые HTTP/WS API, reverse proxy, адреса служб и readiness сохранены.
Экономические действия не вынесены в NATS. Новых SQL таблиц, миграций или
производственных GRANT для N6 нет.

## Что изменено

- `internal/platform/natscore`: прежние N3 TLS/CA/SAN, protected password_file,
  отменяемый initial socket и классификация ошибок используются без ослабления.
  Добавлен bounded SignalClient для Cosmic; допустима отдельная identity `cosmos`.
- `internal/platform/gamesignal/lifecycle.go`: строгие контракты N6 и metadata-only Emit.
- `internal/server/nats.go`, `run.go`, `app.go`: отдельный клиент Cosmic, lifecycle,
  reconnect reconciliation, безопасная диагностика. New по-прежнему не стартует
  горутины и не меняет БД; ConfigureNATS вызывается до HTTP/worker.
- `internal/corporations/signals.go`, directory/cleanup и `internal/terminal/workbench.go`:
  захват цели до удаления заявки, чтение metadata текущей транзакции и emit после
  её успешного COMMIT. Роли/права используют существующую company fence revision.
- `internal/administration/chat.go`: postcommit invalidation game moderator grants.
- `internal/chatserver/nats_signals.go`, dispatcher: новые точные subscriptions,
  coalesced dirty flags, актуальные SQL session/lease/ScopeStamp проверки.
- `internal/chat/maintenance.go`, `commit.go`: отдельный postcommit callback для
  подтверждённого company cleanup receipt; обычный outbox wake не заменён.
- `internal/planet/visits/{service,lifecycle,signals}.go`: postcommit lifecycle hooks,
  снимок последнего местоположения до End. `internal/planetserver` подключает
  callback к своему ограниченному publisher. Transition, движение, возврат,
  generation, epochs, fencing и игровые проверки не перепроектировались.
- Общий strict TOML/ENV parser теперь поддерживает `[nats]` также в Cosmic.
  Примеры трёх конфигов отключены по умолчанию. Секретов в репозитории нет.

## Subjects, отправители и получатели

Во всех строках prefix — `cosmos.v1.signal.`; таблица указывает полный suffix.
BIGINT/epoch/fence в JSON — канонические положительные десятичные строки.

| Subject suffix | Отправитель → получатель | data и точка публикации |
| --- | --- | --- |
| cosmic.company.membership.changed | Cosmic → Chat | `user_id`; после create/join/leave/accept COMMIT; после финального удаления — для снятых участников |
| cosmic.company.role.changed | Cosmic → Chat | `user_id, company_id, company_fence_revision`; после role_assign COMMIT |
| cosmic.company.permissions.changed | Cosmic → Chat | `company_id, company_fence_revision`; после role_save/delete или смены владельца COMMIT |
| cosmic.moderator.changed | Cosmic → Chat | `user_id`; после moderator grants + admin audit COMMIT |
| cosmic.company.deletion.requested | Cosmic → Chat | `company_id, operation_id`; после COMMIT существующего deletion request |
| chat.company.cleanup.completed | Chat → Cosmic | `company_id, operation_id`; после COMMIT cleanup + совпадающего durable receipt |
| planet.visit.started | Planet → Chat | lifecycle IDs; после Activate по handoff COMMIT, до RAM Join |
| planet.visit.resumed | Planet → Chat | lifecycle IDs; после Activate(resume) COMMIT, до RAM Join |
| planet.visit.disconnected | Planet → Chat | lifecycle IDs последней позиции; после действительно изменившего строку Disconnect COMMIT либо Sweep смены runtime fence |
| planet.visit.ended | Planet → Chat | lifecycle IDs последней позиции; после Checkpoint(finish)/Sweep End COMMIT |
| planet.sector.changed | Planet → Chat | прежний N5 payload, stable transition operation UUID; поведение сохранено |
| chat.outbox.changed | Chat → Chat | прежний N3 outbox hint; поведение сохранено |

Lifecycle IDs: `user_id, visit_id, generation, connection_epoch, runtime_fence,
surface_id, sector_number`. Перед End они читаются из той же транзакции, потому
что End очищает location. Проверки состояния остаются прежними. Legacy rows без
полной подтверждённой идентичности не публикуются как фиктивные посещения.
`planet.presence.stale` и `cosmic.transfer.return.completed` не добавлялись.

Envelope: `schema_version=1`, UUID `signal_id`, точные `source/kind`, UTC
`occurred_at`, строгий `data`. Source Cosmic — `cosmic`, хотя login — `cosmos`.
Новые signal_id создаются в Go; они не являются разрешениями или receipts.
Unknown subject/source/kind/version/fields, неканонические IDs, неверные UUID,
пустой/oversized payload или Reply отвергаются получателем. Максимум — 2048 bytes.
N5 географическая revision по-прежнему допускает 0; epoch/fence положительные.

## Авторитетность и работа после COMMIT

Корпоративные hints ставят один ограниченный dirty flag. Dispatcher перечитывает
SQL для текущих локальных WS, включая бывших участников: нельзя выбирать
аудиторию только по заявленной в NATS новой компании. Проверяются действующая
session, точные connection/generation/expiry lease и существующий ScopeStamp.
Роли, company fence и moderator state берутся из актуальной БД. Отправляется
только generic WS sync; HTTP read и каждая команда заново проверяют ACL.

Lifecycle и sector hints добавляют только user_id в bounded spatial dirty set.
Заявленные location/epoch/fence не применяются и не кэшируются как разрешения.
Existing ScopeStamp вызывает chat_lock_planet_presence: действующий visit,
state, surface/sector, epoch, runtime lease/fence, fresh checked_at и geography
control. Старый start/resume после End не восстанавливает пространственный доступ.
Disconnect/End не обязаны повышать epoch; упорядочивать права по NATS timestamp нельзя.

Deletion requested будит штатный Chat maintenance, который читает durable SQL
requests и выполняет прежнюю очистку ограниченными порциями. Большой channel
может потребовать нескольких проходов штатного таймера: hint не делает worker
неограниченным. Только после успешного COMMIT и существования совпадающего
company/operation receipt вызывается cleanup publisher.

Cosmic callback на cleanup.completed лишь будит существующий cleanup worker.
FinalizeChatDeletion перечитывает JOIN компании, deletion request и receipt по
**одновременно company_id и operation_id**, берёт прежние SQL locks и завершает
удаление/возврат в транзакции. Поддельный/старый hint без receipt не выполняет
удаление. Повторы не возвращают кредиты второй раз. После finalization отдельные
membership invalidations тоже выходят только после COMMIT.

На путях publisher нет сетевого ожидания в игровой транзакции. Неудача broker,
переполнение, offline/drop не меняют результат COMMIT. Метаданные для hint иногда
читаются внутри tx, но publish вызывается снаружи после success. При ROLLBACK,
ошибке deferred COMMIT и неопределённом commit outcome emit не вызывается.
Падение процесса между COMMIT и publish допустимо: SQL reconciliation остаётся.

## Очереди, безопасность и диагностика

Cosmic/Planet publisher: очередь 64 metadata сообщений (≤128 KiB payload),
nonblocking enqueue, drop offline/full, без durable RAM backlog. NATS reconnect
buffer выключен, discovery не используется; supervisor с ограниченным timeout
и jitter. При shutdown context закрывает также сокет initial INFO/TLS/auth.
Повторный Start/Close нового shared Cosmic client защищён lifecycle mutex.

У Chat один connection B1, bounded pending по 64 messages / 128 KiB на точный
subscription, spatial dirty ≤256, signal ID cache ≤1024. Corporate dirty и
maintenance wake coalesced; cleanup publication queue ≤64. При удалении большой
корпорации захватывается максимум 256 membership hints; избыточные восстановит
SQL poll. Signal dedupe — оптимизация работы, не гарантия корректности.

Новые subscriptions и обратный cleanup publish опциональны относительно прежнего
B1 outbox. Если старый ACL ещё не даёт их, считаются permission failures,
outbox продолжает работать. После исправления ACL нужен reconnect/restart для
повторной регистрации. Требуемый outbox SUB error по-прежнему переводит source
в degraded/reconnect. Успешный Publish означает клиентский buffer, не доставку.

Диагностика не логирует payload/user IDs/URLs с credentials. Cosmic даёт Go
NATSDiagnostics и раз в 30s пишет bounded counters/state/error code. Chat
добавляет corporate_signals, visit_signals, optional_permission_failures;
прежние N3/N5 счётчики сохранены. Planet использует прежнюю диагностику.
HTTP «Статус служб», service registry и сетевой heartbeat не добавлялись.

TLS минимум 1.2, CA и SAN проверяются. Static accounts: **cosmos, planet, chat**.
Пароль только из protected password_file, ≤4 KiB, CA bundle ≤1 MiB. Unix
0400/0600; Windows DACL checks прежние. Это не полный аудит custom/domain групп
и parent ACL: перед рабочим подключением нужен icacls review под service account.
CA не заменяет пароль, клиентам не нужны private key брокера, JWT/NKey или .creds.

## Fallback и rollback

NATS disabled по умолчанию у всех служб; старые TOML без секции работают.
Включение не создаёт обязательной зависимости SCM/readiness от NATS.
CONFIG_FILE задан — только TOML; иначе только ENV. У каждой службы свой файл
и свои credential paths; относительные пути TOML считаются от его каталога.
Identity и username должны совпадать с конкретной службой. Некорректный
локальный TLS/secret/profile — безопасная ошибка конфигурации; удалённый outage
не блокирует старт и SQL операции.

При потере сигнала остаются Chat SQL auth/scope ~2s, normal/degraded scan
10s/2s и independent current-state reconcile 30s; existing cleanup таймеры
Chat 10s / Cosmic 3s, Planet Sweep 1s. SQL ошибки fail closed; dirty IDs
сохраняются для повтора. На reconnect — forced reconcile; Cosmic тоже будит
штатный receipt worker. Потеря NATS не означает потерю request/receipt/task.

Для отката на новые EXE: Cosmic/Planet `[nats] enabled=false`, Chat
`dispatcher_mode='a1'` либо `'legacy'` и `enabled=false`; перезапустить только
соответствующие процессы с прежними DB/schema/journal/HTTP настройками.
A2 тоже сохранён; его DBA trigger обслуживается по прежней процедуре N2.
Никаких SQL DOWN, удаления чатов, очистки receipts, refund replay или новых GRANT.
При возврате именно к **старому Cosmic 0.24.6** убрать `[nats]` из его TOML,
даже disabled: старый strict parser её отвергнет. Planet/Chat 0.24.10 сохранят
N5 и polling, но не понимают новые hints. Не удалять внешний erasure journal.

## Проверки и воспроизведение

Стенд: Go 1.27.3 linux/amd64 (go.mod 1.26.0 без изменений), PostgreSQL 17.11
в собственной loopback `_test` БД, отдельные временные update033_* схемы.
NATS Server 2.15.2 из официального pinned archive N0, nats.go 1.55.0;
go.mod/go.sum не менялись. Никаких подключений к рабочему Windows/LAN broker.

Native fixture собирает **три настоящих cmd binary с -race** и запускает
Cosmic, Planet и Chat отдельными OS processes. Cosmic также даёт настоящий
health/gateway, не stub. Chat работает под прежней restricted runtime role;
тестовый fault injection ограничен временной schema/sequence/constraint trigger.
PKI с SAN 127.0.0.1, случайные независимые password files 0600, exact PUB/SUB ACL.
Широкий test operator нужен только для fault injection, не входит в шаблон ACL.
Нет production debug API/flags. После завершения свои процессы/схемы удаляются.

Тесты N6 проверяют три identities и разделение PUB/SUB, receipt roundtrip,
strict payload и duplicate/coalescing. Native сценарий проверяет membership,
roles/permissions и moderator, actual PostgreSQL deferred COMMIT rollback,
visit lifecycle и offline lifecycle, stale/duplicate hints, fake receipt hint,
confirmed durable receipt, повторный refund, broker stop/restart и disabled/A1
rollback. Регрессии охватывают Private/Corporation/Spatial, session/lease,
прежние A1/A2/B1 и P02-01—P02-10 N5. Итоговые результаты приведены ниже.

Для запуска нужен только собственный disposable TEST_DATABASE_URL базы `*_test`
и TEST_NATS_SERVER_BINARY официального локального binary. URL чужого брокера
fixture не принимает: он поднимает собственный loopback процесс с временным TLS.

```sh
go test -race ./internal/chatserver ./internal/planetserver ./internal/platform/configuration ./internal/platform/natscore ./internal/platform/gamesignal -count=1 -timeout=2m
go test -race ./internal/server/integration -run '^(TestN6NativeThreeServices|TestN5NativePlanetNATSChat|TestChatN3(SocketScopeSessionAndLease|PrivateCorporationAndCleanup|SpatialTransition|RestartAndRollback)|TestChatN2SocketFallbackScopeAndReconnect|TestChatN1SocketFallbackScopeAndReconnect)$' -count=1 -timeout=5m -v
```

[CI воспроизведения](../../../.github/workflows/chat-n6.yml) использует отдельный
PostgreSQL 17 и NATS archive SHA256
`67cb8e10bc6f0c4b47f75a728529a20953dbae2a4f9a4a52ae8517d65ae303ff`.
Факт локального PASS не означает, что удалённый GitHub CI уже завершён.

## Итоговые результаты · PASS

| Проверка | Фактический результат |
| --- | --- |
| Strict contracts, source/identities/ACL/config, race | PASS: Chat 2.644s, Planet 4.039s, configuration 1.044s, gamesignal 1.034s |
| Три native -race процесса N6 | PASS: 33.55s; тот же restricted SQL runtime role после disabled/A1 rollback, без повторного GRANT |
| Полный выбранный функциональный набор с -race | PASS: 156.878s; N6 + N5/P02 + A1/A2/B1 session/lease, Private/Corporation/Spatial и legacy rollback |
| Role permissions deferred COMMIT failure | PASS: обновление и fence откатываются, нового Core hint нет; fault trigger действительно достигнут |
| Cleanup receipt deferred COMMIT failure | PASS: receipt/cleanup откатываются, completed hint нет; последовательность теста подтверждает достижение COMMIT |
| Fake cleanup hint без receipt / повтор завершения | PASS: компания не удаляется без совпадающего receipt; повтор не возвращает средства дважды |
| Broker offline/restart | PASS: SQL membership и Planet start/disconnect/resume/end, Global, recovery; outage короткий функциональный |
| Старые/duplicate и unseen старый company fence hint | PASS: текущие SQL permissions и visit state не восстанавливаются из payload |
| Старые конфиги и режимы без NATS | PASS: ENV/TOML tests; native all-service disabled/A1 и legacy/A1/A2 регрессии |
| go test ./... без DB/NATS env | PASS: compile/unit; DB cases здесь намеренно skipped и не подменяют native проверки выше |
| go vet затронутых пакетов и integration | PASS |
| Windows amd64 cross-build / PE MZ machine=0x8664 | PASS для Cosmic, Planet и Chat; CGO_ENABLED=0, trimpath, -s -w |

Новые EXE лежат в downloads с **v0.24.11** в имени; SHA256 добавлены в
общий SHA256SUMS, старые версии сохранены. Все три службы изменены и поэтому
собраны. Нативный Windows/SCM запуск не выполнялся. Релиз публикуется через
существующий tag workflow; это не утверждение о развёртывании у владельца.

## Будущие минимальные ACL для cosmoNATS — не применялись

[Exact auth template](templates/auth.test.config.example) перечисляет каждый
разрешённый subject. Cosmic: PUB пять cosmic subjects, SUB только cleanup.completed.
Planet: PUB прежний sector.changed и четыре visit subjects, SUB deny `>`.
Chat: PUB outbox.changed + cleanup.completed, SUB outbox + пять Cosmic + пять Planet.
Нет allow wildcard, JS/SYS/inbox и возможности публиковать от имени другой службы.
Не заменять рабочий auth.config целиком: сохранить текущие accounts и согласовать
точечное расширение только после TLS/password/ACL проверки по N0/N3.

[Core server test template](templates/server-core.test.config.example) предназначен
только для отдельного TLS loopback стенда; нужны собственные CA/server cert/key,
исходные password files и bcrypt hashes на стороне сервера. Не переносить test
operator, test пароли/PKI/порты в production. Автотесты уже проверяют реальные
валидные конфиги с такими exact ACL; placeholders шаблона сами по себе не запуск.

Перед рабочим rollout отдельно подтвердить версию реального NATS и auth include,
backup/restore, SAN нужного IP/DNS, CA trust, service-account read ACL, firewall,
переподключение subscriptions и Windows runtime/service smoke. Здесь проверяется
cross-build; native Windows/SCM и эффективные DACL владельца из Linux не проверены.

Не выполнялись benchmarks, mass/stress, 50–256 WS, profiling, latency percentiles,
пятиминутный outage или сравнение транспортов. Они не объявляются PASS.
JetStream, Economic Service, Request/Reply, Dynamic Registry, heartbeat всех
служб, новый status UI, замена HTTP адресов через NATS и N7 не реализованы.
