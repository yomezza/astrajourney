# NATS Event Bus — 05. Два измеримых пилота: P-NATS-01 и P-NATS-02

Дата: 2026-11-09. **Спецификации экспериментов. Ничего из этого пока не реализовано/не измерено.** Пилоты выполняются только после отдельного разрешения владельца. В обоих действуют PostgreSQL ACL, idempotency, единственный Chat WS, NATS необязателен.

## P-NATS-01. Shared Chat Dispatcher и три источника пробуждения

### 1. Цель и границы

Измерить снижение нагрузки на PostgreSQL и задержки между PostgreSQL COMMIT сообщения и delivery generic WS `sync`. Изменять только **обнаружение изменений и fanout**, не формат сообщений, правила read receipts, SQL-доступ, действующие WS/HTTP-контракты или срок хранения.

Три режима, все используют **одни и те же** Dispatcher / RecipientResolver / WSRegistry / ACL / scope-reconcile / SQL batch reader:

- **legacy** — контрольная группа: нынешний `Changes()` раз в 2s на каждое WS; необходим для отката, не один из новых вариантов.
- **A1** — postcommit local Go wake + shared DB safety polling.
- **A2** — PostgreSQL LISTEN/NOTIFY после транзакционного INSERT chat_outbox + тот же shared polling.
- **B1** — Core NATS postcommit publish + одно subscription + shared polling. Для измерения пути B1 **не посылать обходной мгновенный local wake** на нормальном пути, иначе Core NATS вообще не участвует в измеренной latency.

При ошибке подключения source система переходит в degraded polling без влияния на отправку сообщения.

### 2. Точные этапы изменения Go

1. `internal/chatserver/server.go`: выделить общий lifecycle dispatcher; toggle legacy/A1/A2/B1 до приема WS; старый путь не удалить до прохождения тестов; EventSource health независим от Chat readiness.
2. `internal/chatserver/ws_registry.go` (новый, проект): register/unregister с проверенным lease generation; mailbox capacity=1 для sync и общий single writer.
3. `internal/chatserver/dispatcher.go` (новый): wake/dedup/debounce/scan/fanout/reconcile/telemetry.
4. `internal/chatserver/signals.go` (новый): интерфейс WakeSource и три реализации. Для NATS adapter дополнительный файл `internal/chatserver/nats_signals.go` либо отдельный узкий platform/eventbus, без циклических импортов.
5. `internal/chat/dispatch_reader.go` и `dispatch_acl.go` (новые или точечное расширение changes.go): читать committed outbox и разрешать аудиторию пачками. Существующий `Changes()` используется для проверки regression и fallback, пока safe.
6. `internal/chat/model.go`, `messages.go`, `actions.go`, `private.go`, `maintenance.go` — минимальные postcommit hooks там, где после COMMIT меняется outbox. Нельзя публиковать из общей функции `event(tx)` ДО COMMIT. Для A2 лучше общий маленький PostgreSQL trigger, если DBA подтвердит.
7. `internal/platform/configuration/document.go`, `configuration.go`, `internal/chatserver/server.go`, `configs/chat.example.toml`, `docs/configuration.ru.md`: новая строго валидируемая секция/режим (при **будущей** реализации). Прежние конфигурации должны сохранять поведение.
8. Test packages `internal/chatserver/*_test.go`, `internal/chat/*_test.go`, SQL integration `*_test.go` в существующем подходе; проверять под test DB, не на production schema.

**DBA, отдельно:** A2 — проект `NOTIFY chat_events` в одной транзакции с INSERT chat_outbox; предлагаемый STATEMENT trigger либо явная точечная SQL-функция (выбрать после PostgreSQL теста), GRANT/ownership, rollback, проверка эффектов при COMMIT/ROLLBACK. Index для outbox уже есть по (created_at,id), повторная проверка EXPLAIN; дополнительных persistent chat event tables не нужно.

### 3. Старт/остановка/переподключение

- Start: DB schema compatible → Claim/ready не меняется → source Subscribe/LISTEN сначала → подтверждение регистрации (NATS Flush; SQL LISTEN COMMIT) → initial scan текущего outbox + scope reconciliation → принимать WS.
- Если NATS при старте не отвечает, B1 переводится в degraded polling и **не держит открытие игрового Chat в ожидании**; TLS/bad config вызывает ошибку конфигурации до запуска.
- Анонимные/неавторизованные WS не зарегистрированы. При новой зарегистрированной сессии принудительно `sync`/bootstrap, чтобы избежать race между моментом initial scan и появлением клиента.
- Reconnect source: re-subscribe, flush, force full scan и refresh всех local scope; ошибки SQL не продвигают cursor.
- Stop: перестать регистрировать WS, drain source bounded, закрыть goroutines, вернуть существующий lifecycle, не терять already committed message.
- Fallback: 10s outbox polling в healthy; 2s при source unavailable/подозрении на пропуск; 60s overlap (с уточнением по измеренной длине tx); auth и scope независимо как минимум с текущей 2s периодичностью в пилоте. 30s не считать эквивалентом прежней задержки.

### 4. Гарантии и тестовые случаи

| ID | Сценарий | Ожидаемый результат |
|---|---|---|
| P01-01 | Два WS от одного аккаунта | Новый WS 409 пока прежний DB lease активен |
| P01-02 | Message COMMIT затем process crash до signal | Сохранён; fallback/next bootstrap обнаруживают |
| P01-03 | Transaction ROLLBACK после записи outbox | Нет signal о committed изменении и нет истории |
| P01-04 | Lower outbox ID COMMIT после higher ID | Lower ID обнаружен overlap и не пропадает |
| P01-05 | 20 событий Global подряд | bounded SQL + coalesced sync; без 20 самостоятельных SQL по каждому клиенту |
| P01-06 | Новый Private invite и channel_id=NULL | Только sender/recipient и допустимые current members; никакой фан-аут всем |
| P01-07 | Игрок вышел из corporation в момент fanout | Generic signal не содержит данных; history reject по актуальным правам, никакого чужого Private/Corp ID |
| P01-08 | Неполная Planet presence или смена сектора | Space ACL fail closed; другие вкладки работают |
| P01-09 | Broker down 5 minutes | Продолжается отправка, delayed sync через fallback, resubscribe/full reconcile |
| P01-10 | WS медленно читает | Только его разрыв, остальные WS/dispatcher без head-of-line blocking |
| P01-11 | Reconnect или F5 | Новое состояние из БД, не ждать старого Core event |
| P01-12 | Logout/expired session | Прекратить выдачу generic sync, не продлевать старый lease |

**Справедливость сравнения:** одни и те же DB, seed data, accounts, rate policy, message workload, WS reconnect policy, coalescing, scope-checks, start point и instrumentation; менять только WakeSource. A2 SQL trigger следует учитывать в SQL стоимости транзакции, B1 — в сетевых/CPU затратах брокера; сравнивать **total** costs.

### 5. Измерения и решения

На каждом режиме: idle + controlled 1/10/100 message per second (если игровой rate policy позволяет, распределять по достаточному числу разных аккаунтов), profiles 50/100/200/256 активных WS. Нагрузка на одну Global не должна случайно ограничиваться rate per user; измерять и фиксировать участки подготовленных запросов.

- Времена `after SQL COMMIT → dispatcher wake → outbox scan done → sync write → browser receives sync → authorized GET done`; UTC timestamps в БД и time.monotonic process не смешивать без измерительного clock sync. Для end-to-end latency использовать monotonic client/server probe с синхронизированными метками либо измерять участки отдельно.
- Пять повторов после warmup (проект), фиксировать p50/p95/p99, max, ошибки, loss, SQL statements/s, DB CPU, pg_stat_statements total_exec_time, buffer reads, Go CPU/RAM, connections, goroutines, WS queue, NATS send/recv/reconnect.
- Условный gate: >=90% reduction SQL **опросов обнаружения outbox** на 200/256 WS, без обещания 90% total DB reduction; healthy LAN p95 COMMIT→WS sync <=300ms, без деградации send p95, ноль ACL leakage, ноль data loss в отказных тестах. Все пороги **предварительные**.
- Бенчмарки не считаются, если нет результатов из PostgreSQL и реального browser/WS. Отчёт сравнивает A1/A2/B1 и может обоснованно выбрать A2, даже если целевая межсервисная архитектура B.
- Rollback: `chat.dispatcher_mode='legacy'` с перезапуском, затем удалить NATS subscription/trigger **после безопасного переключения**; данные остаются.

## P-NATS-02. Planet → Core NATS → Chat при смене сектора

### 1. Предпосылки и зона эксперимента

Запускать только после тестов P-NATS-01, выбирая его общий Chat Dispatcher и RecipientResolver, с теми же fallback правами. Эксперимент **не** изменяет серверное движение, порталы, A*, подтверждение перехода, planet_visits, legacy Planet WS или возврат корабля.

Сценарий: игрок завершает `transition.confirm`; Planet подтверждает SQL UPDATE planet_visits + INSERT planet_transition_receipts; Chat получает Core signal, обновляет только состояние доступа к spatial chat. Никаких данных из старого сектора не высылает новым подписчикам без SQL проверки.

### 2. Конкретное место публикации

- Факт перехода: `internal/planet/visits/transition.go:Transition`: проверяет operation UUID, scope/epoch, source/destination, в одной SQL tx делает UPDATE planet_visits.current_sector и connection_epoch, INSERT planet_transition_receipts и COMMIT; только **после** успешного COMMIT возвращает Welcome нового сектора.
- Вызывающий `internal/planetserver/transitions.go:commitCrossing` получает успешный `visits.Transition(...)`; отправка сигнала выполняется **после этого возврата**, в не блокирующей postcommit notification ветке. Альтернативно postcommit callback внутри Transition с явно зафиксированным return success — без SQL transaction в момент publish. Предпочтительно event emitter на границе successful transition с корректными from/to captured keys и operation.
- `transition.offer`, незавершённый `transition.confirm`, ошибка commit, ошибка joining RAM после подтверждения — **не являются новыми SQL переходами**. При SQL COMMIT и последующей ошибке RAM новый визит уже persisted: Core hint допустим, Chat смотрит DB; не обещать rollback после COMMIT.
- После commit Publish NATS может потеряться/повториться: это лишь latency optimization, state остаётся новым. Не блокировать transition.complete или движение из-за NATS.

### 3. Subject, payload, identity

Subject: `cosmos.v1.signal.planet.sector.changed`, Core. Пример:

```json
{
  "schema_version": 1,
  "signal_id": "transition-operation-uuid",
  "kind": "planet.sector.changed",
  "source": "planet",
  "occurred_at": "2026-11-09T12:00:00Z",
  "data": {
    "user_id": "152",
    "visit_id": "visit-uuid",
    "generation": "4",
    "connection_epoch": "19",
    "runtime_fence": "31",
    "surface_id": "42",
    "from_sector": 6,
    "to_sector": 7,
    "geography_revision": "8",
    "transition_operation_id": "transition-operation-uuid"
  }
}
```

Схема только для уведомления, не credential. `transition_operation_id` уже сохранён в planet_transition_receipts; его можно использовать как stable signal_id. Отдельный `presence_revision` **не нужен в P-NATS-02**: не применять события как replayable последовательность состояний. connection_epoch полезен для диагностики и отбрасывания заведомо старых hints, но это не универсальная последовательность disconnect/end.

### 4. Consumer Chat: fail-closed

1. NATS callback проверяет subject/schema/payload size/version, не создаёт chat state и кладёт user_id в bounded spatial dirty set (coalescing за 50ms).
2. Dispatcher на короткой SQL read tx вызывает **existing** `chat_lock_planet_presence(user_id)` либо текущий строго эквивалентный авторитетный механизм. Он проверяет состояние, runtime fence/lease, current surface/sector и fresh checked_at.
3. Сравнивает действующий доступ **с текущим разрешённым view** этого пользователя и, если изменился, помечает stale старый Sector/Planet и отправляет только generic sync через WSRegistry. Старые queued per-channel event hints удаляются или перед отправкой проверяются текущим ACL.
4. Смена сектора не создаёт доступ к истории старого сектора; новый сектор разрешён только после проверки SQL. Клики/отправка из старой вкладки отвергаются настоящей SQL send-check, не из NATS cache.
5. Duplicates/out-of-order/old epoch только запускают повторную проверку или игнорируются как уже выполненная; событие не может переустановить старое местоположение.
6. Если NATS отсутствует, запускается обычный periodic scopeStamp и SQL validation. Если Planet unavailable, разрешённые Planet/Sector tabs disabled; другие каналы продолжают работать при healthy Cosmic/Chat.
7. Если Chat перезапущен, нет need for Core replay: current visit authoritative в PostgreSQL; каждый новый Chat WS bootstrap + periodic revalidate.
8. Прогон нескольких пользователей по двум секторам: никто не читает чужую историю; Planet publisher не отправляет location broadcast всему клиентскому WS, только служебный signal и Generic sync своим разрешённым Chat пользователям.

### 5. Что придётся менять

**Go (после разрешения):**
- `internal/planetserver/transitions.go` — postcommit publishing hook;
- `internal/planetserver/app.go` и `run.go` — optional eventbus lifecycle;
- новый узкий `internal/platform/eventbus/` либо package adapter на уровне Planet; runtime option;
- `internal/chatserver/dispatcher.go` и eventbus source/consumer — принять spatial dirty;
- `internal/chat/dispatch_acl.go` — повторная проверка Planet state, но не новая permission policy;
- `internal/platform/configuration/document.go` и `configs/planet.example.toml` — предложения новых секций;
- Go tests/integration — commit-before-publish, duplicates, old epoch, runtime down.

**DBA:** для P-NATS-02 не требуется новая таблица, поскольку есть planet_visits и planet_transition_receipts. Только если тесты обнаружат отсутствующие индексы/нужный новый event revision — DBA отдельно оценивает. Никаких миграций автоматически.

### 6. Испытания P02

- P02-01: отменённый offer/ошибка SQL commit → нет signal;
- P02-02: подтверждённый переход: event только после COMMIT, Chat переходит в новый разрешённый сектор;
- P02-03: старый sector message пытается поступить после COMMIT → HTTP/WS ACL reject;
- P02-04: duplicate signal → один effective refresh, без двойной новой подписки/сброса;
- P02-05: old `from 6→7` приходит после нового `7→8` → current DB sector=8, не 7;
- P02-06: NATS down до/после commit 5min → movement/transition рабочие, spatial Chat updated fallback;
- P02-07: Planet down + NATS down → только Planet/Sector Chat недоступны (весь Cosmic-dependent Chat работает при здоровом Cosmic);
- P02-08: Chat restart после missed signals → first bootstrap correctly resolves scope;
- P02-09: transition COMMIT есть, RAM join failed → signal о DB переходе не вводит Chat в ложный сектор;
- P02-10: неизвестный schema_version/payload oversized → discard + metric + fallback.

### 7. Отчёт после пилота

Сравнить baseline periodic spatial detection vs Core NATS: COMMIT→Chat scope refresh p50/p95/p99; number SQL calls per transition, degraded max delay, при 50/100/200/256 WS; Postgres CPU, NATS reconnect, packet loss, error count. **Результат — отдельный отчёт после реального эксперимента**, не утверждение о достигнутой производительности. Пройти P-NATS-01 acceptance до P-NATS-02 rollout.

## Решение об эксперименте и границы

P-NATS-01 и P-NATS-02 **приняты только как направление**. Они не разрешают внедрять код/DDL/конфиги в текущем этапе. После результатов разработчик и архитектор выбирают A2/B1 для Chat и подтверждают необходимость дальнейших Core signals.