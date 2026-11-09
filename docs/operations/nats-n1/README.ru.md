# NATS N1 — общий Chat Dispatcher, A1

Дата: 2026-11-09 (Екатеринбург, UTC+5). База разработки:
`1c922ca` после N0. Выпуск **Chat 0.24.7**, Cosmic остаётся 0.24.6,
Planet — 0.24.4. N1 реализует локальные postcommit wakeups и PostgreSQL fallback.
**NATS, A2, B1, JetStream не подключены; N2 не начинался.**

## Результат

В `a1` один EventDispatcher на процесс читает metadata outbox, объединяет
повторы и определяет адресатов через актуальную PostgreSQL-авторизацию.
Индивидуального вызова `Changes()` каждым WS в этом режиме нет.
`legacy` сохранён и остаётся **режимом по умолчанию** старых и новых установок.
HTTP/WS routes и сообщения `hello`, `ack`, `health`, `sync` не изменены.

Реальная PostgreSQL и отдельный нативный процесс Chat использованы для
[сравнения legacy/A1](BENCHMARK.ru.md). Результаты не выдаются за Windows/LAN
production benchmark. Рабочая база владельца, службы, TOML и NATS не изменялись.

## Компоненты и файлы

| Компонент | Реализация | Гарантия |
| --- | --- | --- |
| WSRegistry | `internal/chatserver/ws_registry.go` | current handle по user_id, проверенное поколение, capacity 256, exact unregister, отмена старой регистрации |
| EventDispatcher | `internal/chatserver/dispatcher.go` | один worker, coalescing 50ms, страницы до 1000 строк, bounded dedupe, безопасный cursor и recovery |
| WakeSource / LocalWakeSource | `internal/chatserver/signals.go` | интерфейс транспорта, A1 channel capacity 1, nonblocking after-commit, bounded close |
| Metadata reader | `internal/chat/dispatch_reader.go` | DB-clock cutoff, max visible id, keyset paging, отдельная ScopeStamp/lease-проверка |
| RecipientResolver | `internal/chat/dispatch_acl.go`, метод `ResolveRecipients` | независимые bounded SQL-проверки категорий, existing auth/fences/presence |
| Postcommit observer | `internal/chat/commit.go`, `model.go`, `maintenance.go`, `journal.go` | callback после успешного COMMIT; replay UUID не создаёт новое уведомление |
| Composition / single WS writer | `internal/chatserver/server.go` | только владелец connection пишет JSON; ACK отдельно от sync mailbox; shutdown ждёт handlers/reader/lease release |
| Config | `dispatcher_config.go`, `internal/platform/configuration/document.go` | строгий TOML/ENV, legacy default, только legacy/a1 |
| Диагностика | `internal/chatserver/diagnostics.go` | counters и memory/pool metrics в Go и структурированном логе, без user/channel/session labels |

Также изменены `.env.example`, `configs/chat.example.toml`, инструкции Chat и
общей конфигурации; workflow публикации допускает выпуск только Chat.
Cosmic/Planet runtime-файлы, cmd, SQL-схемы, Go dependencies и браузерный клиент
не изменены. Общий mapper конфигурации расширен только ради новых полей Chat.
Релиз содержит только новый Chat EXE; старые сборки сохранены.

## Авторизация и доставка

- На подключении сохраняются Origin, session, readiness, PostgreSQL Claim и
  ограничение одной активной WS lease. Вторая активная сессия получает 409.
- Оба режима по-прежнему проверяют сессию примерно каждые 2s и renew lease
  приблизительно раз в 10–12s, TTL 45s. В A1 отдельный `ScopeStamp` дополнительно
  проверяет текущее поколение/срок lease и existing `chat_scope_stamp`.
- Scope проверяется независимо от outbox: смена company/role/moderator grant,
  fence, surface/sector/epoch или потеря свежей presence не требует outbox row.
- Resolver повторно проверяет session и точную lease для кандидатов, далее
  отдельно Global, target_user, Corporation, Private/приглашения и Spatial.
  `chat_lock_company_access` и `chat_lock_planet_presence` остаются авторитетными.
- Targeted событие **не** превращается в broadcast, даже если в нём есть global
  channel. Private `channel_id=NULL` разрешается по reference/conversation и
  реальным участникам/неистёкшим приглашениям. Имена/тела не загружаются reader.
- Отправляется только `{"type":"sync"}`. Смена прав после SQL check не передаёт
  чужой ID или body: авторизованный HTTP GET/Execute снова проверяет доступ.
- UUID-idempotency, ограничения отправки, модерация и fsync erasure journal
  сохраняются. Новых SQL-прав и миграций нет; проверен существующий restricted role.

При истечении приглашений maintenance теперь добавляет targeted `invite.changed`
для отправителя и получателя **в той же транзакции**. Это требуется общему
диспетчеру: expiry без новой outbox записи нельзя надёжно обнаруживать по
однажды дедуплицированному событию. Успешный COMMIT пробуждает dispatcher,
ROLLBACK — нет. Это metadata invalidation, не изменение правил приглашений.

## Cursor, bounded state, ошибки и завершение

- Стартовая сверка читает недавнее окно; новый WS получает первоначальный sync
  и свежий bootstrap. Это state refresh, а не replay старых пользовательских
  сообщений. После reconnect авторизация/Claim/начальная сверка выполняются заново.
- Cutoff вычисляет PostgreSQL; страницы одного прохода используют неизменные
  cutoff/max id. `id` — не порядок COMMIT: дополнительно читается overlap 60s.
  Поздний меньший id обнаруживается следующей сверкой.
- Стартовый filtering floor не является delivery cursor. highwater продвигается
  только после успешной обработки **всех** страниц; SQL/ACL failure его не двигает.
- Wake, полученный при обработке старого paged window, сохраняет необходимость
  нового window после завершения текущего. Его нельзя поглотить как continuation.
- После ошибки — degraded retry 2s, без scan storm при новых wake. После
  успешного восстановления выполняется SQL-authorized refresh всех текущих
  сессий: длительный outage может превышать overlap/retention, и старые hints
  не являются единственным способом восстановить UI.
- Mailbox sync capacity 1 на пользователя, command queue capacity 16, локальный
  wake channel capacity 1, до 1000 rows/page, до 256 connection candidates,
  dedupe ring до 32768 id. Нет текстов сообщений или бесконечной RAM-очереди.
- При вытеснении dedupe возможен лишний generic sync, но не потеря game data.
  Доставка не объявляется exactly-once; bootstrap/recheck остаются обязательными.
- 60s overlap — экспериментальный запас для текущих ограниченных транзакций
  (Execute context 8s, SQL statement timeout 5s) и polling 10s. Произвольный
  внешний SQL с неограниченным временем COMMIT не покрывается вечным replay;
  полный state refresh компенсирует recovery, а данные остаются в PostgreSQL.
- Dispatcher никогда не ждёт WS I/O. Один writer сохраняет timeout 3s;
  slow writer отключается, другие игроки продолжают получать sync.
- Shutdown закрывает admission/registry, отменяет workers и connections, ждёт
  reader goroutines, условного DB lease release и handlers до закрытия пула.

Readiness Cosmic/Planet/DB остаётся прежним; source/dispatcher health не
подменяет игровую готовность. PostgreSQL auth failure не выдаёт доступ из cache.

## Настройки и включение

После установки Chat 0.24.7 добавьте поля в **существующий** раздел `[chat]`
его отдельного TOML (не создавайте второй одноимённый раздел):

```toml
[chat]
# Остальные database/http/origin/journal настройки сохранить.
dispatcher_mode = 'a1'
fallback_poll_interval = '10s'
degraded_poll_interval = '2s'
outbox_overlap = '60s'
```

В ENV-режиме, когда CONFIG_FILE не задан:

```powershell
$env:CHAT_DISPATCHER_MODE = 'a1'
$env:CHAT_FALLBACK_POLL_INTERVAL = '10s'
$env:CHAT_DEGRADED_POLL_INTERVAL = '2s'
$env:CHAT_OUTBOX_OVERLAP = '60s'
```

Затем штатно перезапустить только Chat с прежними БД/схемой и журналом.
Для SCM значения должны быть в окружении самой службы, не другой консоли.
При CONFIG_FILE настройки из ENV не подмешиваются.

Допустимы legacy/a1 (A1 в ENV/TOML также нормализуется); poll 1–30s,
degraded 500ms–10s и <=normal, overlap 60s–10m. A2/B1 отвергаются. Эти интервалы
не окончательный стандарт производительности. Секция `[nats]` по-прежнему
отвергается строгим parser, даже с enabled=false.

**Прежние EXE Cosmic/Planet не знают четырёх новых полей.** Если раньше был один
общий TOML, для эксперимента A1 сделайте отдельную копию только для Chat.
В старый общий файл новые поля не добавлять. Обновлять Cosmic/Planet для N1
не требуется. Прежний общий TOML без новых параметров работает в legacy.

## Проверки и команды

- Unit/race: registry replacement/unregister/coalescing/capacity, bounded cache,
  cursor на ошибке, late lower id, paging, wake во время paging, fallback,
  recovery за пределами overlap, shutdown, строгие настройки и отсутствие ENV merge.
- Реальная PostgreSQL integration/race: session/lease generation, NULL-private
  invitations, target-only, корпоративный fence после выхода, пространственная
  изоляция/потеря presence, UUID replay, ROLLBACK без wake, поздний COMMIT,
  expiry invalidation, reconnect/409, session expiry, slow writer и SQL recovery.
- Существующие Chat240 сценарии: 100k search, обычные/приватные сообщения,
  модерация, корпоративный cleanup, restricted role, 50 WS — прошли под race.
- Реальный `pg_dump/pg_restore` в отдельном test container с erasure replay —
  прошёл, ранее удалённый текст/копия не появились после восстановления.
- `go test ./...` без TEST_DATABASE_URL: unit/compile всех пакетов; DB cases
  явно пропущены, это не подмена отдельных реально выполненных DB проверок.

В командах использовать **только отдельную БД с суффиксом `_test`**:

```sh
go test ./...
go test -race ./internal/chatserver ./internal/chat ./internal/platform/configuration
TEST_DATABASE_URL='<disposable database ending _test>' \
  go test -race ./internal/server/integration -run '^(TestChat240|TestChatN1)' -count=1 -timeout=8m
```

Для отдельного backup drill указать `CHAT_BACKUP_DOCKER_CONTAINER` именно
своего тестового PostgreSQL container; рабочий container/схема не выбираются.

Измерения и воспроизведение — [BENCHMARK.ru.md](BENCHMARK.ru.md),
полные числа — [benchmark-results.json](benchmark-results.json).

## Наблюдаемость и откат

Раз в 30s Chat выводит структурированную агрегированную диагностику:
mode, connections, sync writes, queued/coalesced sync, Changes/scope checks,
scans/errors/rows/dedupe/wakes/highwater/degraded, pool/SQL/heap/goroutines.
Новых browser API/WS полей или публичного metrics endpoint нет. Никаких
session hashes, user/channel IDs, message body, DSN и passwords в labels.

Оперативный откат — `dispatcher_mode='legacy'` в файле Chat либо
`CHAT_DISPATCHER_MODE=legacy` в ENV-режиме и штатный перезапуск. Данные,
схема, UUID receipts и erasure journal не удаляются/не пересоздаются.
Не запускать второй Chat параллельно: прежняя PostgreSQL advisory lock остаётся.
Для отката самого EXE на 0.24.4 сначала убрать четыре новых поля из TOML:
старый strict parser их не знает. Формат существующих таблиц не менялся.

## Ограничения и N2

A1 остаётся opt-in. Измерение — короткий Linux лабораторный Global WS workload,
не production capacity test и не проверка реальной Windows-службы. Нужны
совместное ревью, Windows smoke, длительный прогон/разнообразная аудитория и
эксплуатационная оценка; цифры не гарантируют те же CPU/latency на другом хосте.

Новая основа готова для обсуждения N2. Сам N2 не реализован: LISTEN/NOTIFY,
DBA trigger/grants и дальнейшие пилоты требуют отдельной команды и отдельной
приёмки. NATS Server, его TLS/ACL/секреты и future `[nats]` не затрагивались.
