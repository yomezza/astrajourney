# NATS N2 — Chat A2 через PostgreSQL LISTEN/NOTIFY

Дата: 10 ноября 2026, Екатеринбург (UTC+5). Исходный `main` сверён и обновлён
до `e61d225`, включая исправленный SQL и [отчёт DBA](../nats-n2-dba/README.ru.md).
Выпуск **Chat 0.24.8**; Cosmic **0.24.6**, Planet **0.24.4** не пересобирались.

Реализован опциональный `a2` на изолированной PostgreSQL с полной игровой
схемой. `legacy` остаётся режимом по умолчанию; `a1` сохранён. NATS, B1,
JetStream, P-NATS-02 и N3 не реализовывались. Рабочие БД, NATS, Windows Services
и пользовательские конфиги не изменялись. Это не разрешение на production DDL.

## Изменения

| Файл | Результат |
| --- | --- |
| `internal/chatserver/postgres_signals.go` | Адаптер WakeSource: выделенный pgx-сеанс, LISTEN, reconnect/backoff, отмена, bounded wake, диагностика |
| `internal/chatserver/server.go` | Выбор legacy/A1/A2; прежний единственный EventDispatcher и WS writer; версия 0.24.8 |
| `internal/chatserver/dispatcher.go` | Reconcile при повторной подписке и независимый state refresh каждые 30s для A1/A2 |
| `dispatcher_config.go`, `signals.go`, `diagnostics.go` в том же пакете | Валидация A2, контракт источника и технические счётчики |
| `internal/chatserver/dispatcher_test.go`, `internal/platform/configuration/configuration_test.go` | ENV/TOML A2, прежние defaults, строгая проверка и отсутствие NATS-секции |
| `internal/server/integration/chat_n1_test.go` | Общие транспортные test helpers, повторное использование проверок в A2; exact generation teardown |
| `internal/server/integration/chat_n2_test.go` | Реальные SQL/WS/Planet, отказ источника, поздние COMMIT, retention, restart и rollback |
| `.github/workflows/chat-n2.yml` | Воспроизводимые functional/race проверки с disposable PostgreSQL, без нагрузочного DBA runner |
| `configs/chat.example.toml`, `.env.example`, README и инструкции конфигурации/Chat | Режимы, ограничения, совместимость и откат |
| `release-notes/v0.24.8.md`, `downloads/` | Только новая Windows-сборка Chat, SHA256; старые версии сохранены |

`internal/chat`, SQL ACL, игровые HTTP/WS, роли, схема/индексы, цепочка
`internal/server/schema*.sql`, Go dependencies и внешний erasure journal не
менялись. Для тестов применён **существующий** create-only SQL DBA, без его
переписывания и без автоматического подключения к миграциям.

## LISTEN lifecycle и fallback

1. Адаптер получает и `Hijack`-ит одно физическое соединение из настроенного
   pgxpool. Инициализация соединения, search_path и ограниченная runtime-роль
   сохраняются. Сеанс после этого не возвращается в обычный query pool.
2. Канал вычисляется PostgreSQL: `cosmos_chat_ || md5(physical_schema)`.
   Физическая схема `chat_outbox` должна совпадать с `current_schema()`;
   имя LISTEN экранируется как SQL identifier. Схемы одной БД имеют разные каналы.
3. `LISTEN` выполняется отдельной autocommit-командой. Только после её успешного
   завершения запускается начальное чтение диспетчера. Открытой транзакции на
   listener нет. При ошибке первой регистрации включается polling, а отдельный
   bounded reconnect loop продолжает попытки.
4. `WaitForNotification` принимает только собственный канал и **пустой payload**.
   Уведомление лишь будит общий диспетчер через канал capacity=1. Оно не
   содержит сообщения, адресатов, полномочий или приватных идентификаторов.
   Посторонний непустой payload игнорируется; канал сам по себе не ACL.
5. `AfterCommit()` в A2 — no-op: локальный A1 wake не подменяет PostgreSQL.
   Обычный fallback остаётся 10s, при обнаруженном отказе источника — 2s.
   Потеря сигнала, отсутствие триггера и разрыв listener не теряют запись в БД.
6. При разрыве сеанс закрывается; retry 250ms с ростом до 5s, одна попытка
   подключения/регистрации ограничена 3s. Повторный LISTEN завершается **до**
   запроса новой сверки. Диспетчер сбрасывает незавершённое окно и перепроверяет
   действующие сессии/leases, затем восстанавливает обычный scan.
7. При остановке отменяется `WaitForNotification`, закрывается выделенное
   соединение и завершается goroutine; прежний lifecycle App дожидается workers
   и WS handlers перед закрытием общего pool. Close повторяемый.

Нужен direct PostgreSQL либо session pooling; transaction pooling не сохраняет
LISTEN-сеанс. Учитывать **одно дополнительное физическое соединение** сверх
обычных соединений пула. Диагностика в прежнем структурированном логе содержит
`wake_source` (registrations/disconnects/signals/ignored_signals/backend_pid) и
`dispatcher.state_reconciliations`. Новый публичный endpoint не добавлялся.
`wake_source.healthy` означает регистрацию LISTEN, **не** наличие триггера,
очередь без потерь или игровую readiness. Без UP нормальный polling всё равно
работает; это отдельно проверено. Источник не расширяет SQL-права.

## Поздний COMMIT: проблема и защита

`BIGSERIAL` отражает выдачу номера, а не порядок COMMIT. Старый сканер исключал
меньший ID, когда более высокий уже обработан и `created_at` старше overlap.
Даже доставленный NOTIFY не исправляет такой SQL-предикат. Поэтому overlap
оставлен 60s как ускорение, но **не** объявлен гарантией полноты.

Для A1/A2 добавлена независимая сверка раз в **30s**, не зависящая от outbox
highwater, created_at и retention. Она формирует только адресные внутренние
запросы reconcile для текущих WS handles, заново вызывает существующий SQL
RecipientResolver с действующей сессией и точным WS lease/generation, затем
ставит generic `sync` в bounded mailbox. Штатный клиент по `sync` заново читает
bootstrap, уведомления и текущую историю через HTTP с актуальными ACL.
Состояние отложенного сообщения/приглашения/удаления восстанавливается даже
при пропуске самого hint. При SQL-ошибке рассылки нет; после восстановления
сверка повторяется. Истечение сессии/смена scope по-прежнему проверяются ~2s.

Срок 30s — период запуска при здоровых SQL/WS, не жёсткий сетевой SLA. Добавляются
время SQL, WS и HTTP; при отказе базы успешная сверка откладывается. Это
**eventual current UI state**, не доставка каждого исторического outbox event
и не протокол экономических проводок. Legacy сохраняет прежний Changes-путь;
новая гарантия независимой сверки относится к общему диспетчеру A1/A2.

Аудит writers: `messages.go:event()` вызывается в транзакции Execute из
messages/actions/private; отдельный INSERT при expiry и cleanup находятся в
Maintain. Сетевой Execute имеет context 8s, Maintain — 9s, но SQL
statement_timeout 5s/8s не ограничивает полную транзакцию произвольного embedding
или внешнего writer. Replay erasure journal сам outbox не пишет. Верхний bound
для всех writers не доказан; защита не опирается на него. PostgreSQL
transaction_timeout, глобальные/session/role timeout, схема и права не менялись.
Если впоследствии нужна доставка каждого hint, требуется отдельное решение с
DBA о commit-order либо доказанном transaction bound.

## Проверки на изолированном стенде

Тестовая PostgreSQL **17.11** (Debian), контейнер `cosmos-n1-test`, loopback
порт 55439, БД `cosmos_n1_test`; имя оканчивается `_test`. Для каждого теста
создаётся и затем удаляется полная игровая схема `update033_*`. Две схемы
использованы для проверки изоляции. Go **1.27.3 linux/amd64**. Владелец fixture
применяет UP/DOWN в одной транзакции с проверенной схемой и короткими timeout;
Chat использует штатные restricted NOLOGIN роли через `SET ROLE`, созданные
`GrantRuntime`. Это не проверка отдельного production TCP LOGIN/Windows.

Функциональные и race-проверки выполняются реальными pgx/WS соединениями,
а Spatial — с отдельным настоящим тестовым Planet process и подтверждённым
переходом между секторами. Cosmic health для транспортных тестов имитируется
локальным health endpoint, игровая схема и регистрация настоящие. Никаких
50/100/200/256 WS, профилирования, latency-percentile или миллионных наполнений.

### D01–D26 в этой реализации

| ID | Статус / покрытие |
| --- | --- |
| D01 | PASS: restricted INSERT невидим до COMMIT, затем строка и пустой NOTIFY |
| D02 | PASS: ROLLBACK не оставляет строку и сигнал |
| D03 | PASS: 3 INSERT statements одной tx дают один одинаковый NOTIFY |
| D04 | PASS: multirow INSERT, 3 строки, один wake; объём минимален для функциональной проверки |
| D05 | PASS: пустой INSERT даёт допустимый wake и не меняет данные |
| D06 | PASS: два отдельных Go listener получают committed signal |
| D07 | PASS: меньший ID после большего внутри overlap, штатные Chat240/N1 проверки |
| D08 | PASS: A2 реальная tx удерживается 65s; SQL overlap исключает lower ID, независимый refresh восстанавливает сохранённое сообщение. A1: та же граница с backdated timestamp без второй долгой паузы |
| D09 | NOT RUN: PostgreSQL transaction_timeout не менялся; исторический SQL-тест DBA не выдаётся за новый тест N2 |
| D10 | PASS: Start завершает autocommit LISTEN до initial scan, listener idle без открытой tx; post-registration INSERT получен |
| D11 | PASS: backend listener принудительно завершён, новый LISTEN и запрос полной сверки; последующий INSERT получен |
| D12 | PASS: отдельный listener TCP proxy недоступен, authoritative SQL работает и degraded polling доставляет sync; отдельно проверен healthy LISTEN без установленного триггера |
| D13 | PARTIAL: bounded/paging/race проверки N1 сохранены, реальная SQL-пагинация 2001 строк; rate 1000+/s и нагрузочный burst отложены |
| D14 | PASS: A2 работает под restricted runtime; DDL, UPDATE users и прямое EXECUTE функции запрещены, SQL ACL не расширены |
| D15 | PASS: прежний Chat240 send/Changes/History; legacy после UP проверен DBA, новый rollback проверяет отправку после DOWN |
| D16 | PASS: live A2 WS, смена роли/компании без outbox и отказ чтения прежней компании |
| D17 | PASS: A2 company deletion, durable cleanup receipt и Cosmic finalization |
| D18 | PASS: A2 Planet/Sector, outsider denied, реальный переход сектора, старый scope и disconnected presence запрещены |
| D19 | PASS: A2 Private/NULL-channel invitation, принятие, expiry, разрешённая история и отсутствие fanout постороннему |
| D20 | PASS: A2 session expiry, duplicate WS 409, reconnect, замена generation; старый handler не освобождает новую lease |
| D21 | PASS: outbox очищен штатным retention, новый A2 Chat/WS получает сохранённую историю без replay hints |
| D22 | NOT RUN / отложено: новые EXPLAIN и performance наполнения не запускались |
| D23 | NOT RUN / отложено: сравнения legacy/A1/A2/B1, 50–256 WS и percentiles не выполнялись |
| D24 | NOT RUN / отложено: гигабайтная saturation NOTIFY и вызванный ею COMMIT failure не выполнялись |
| D25 | PASS: stop A2 → A1 и stop A2 → legacy, UP/DOWN повторяемые, listener закрыт перед DOWN, данные и relacl сохранены |
| D26 | PASS: две полные схемы одной БД, разные каналы, cross-schema wake отсутствует |

Дополнительно: slow writer отключается по прежнему timeout и не блокирует
здорового клиента; revoke SELECT на outbox даёт fail closed и восстановление;
нет индивидуального Changes в A2; отклоняется непустой payload; штатные UUID,
модерация/журнал и restricted-role регрессии не менялись.

Итог выполнения и команды фиксируются в [FUNCTIONAL-RESULTS.ru.md](FUNCTIONAL-RESULTS.ru.md).
Нагрузочные пункты остаются отложенными, результаты DBA с миллионом строк не
переносятся на A2 Go как новые измерения.

## Конфигурация, безопасное внедрение SQL и откат

До отдельного production согласования использовать A2 **только на тестовом
стенде**. В существующей секции `[chat]` отдельного TOML Chat:

```toml
[chat]
dispatcher_mode = 'a2'
fallback_poll_interval = '10s'
degraded_poll_interval = '2s'
outbox_overlap = '60s'
```

В ENV-режиме (CONFIG_FILE не выбран) — `CHAT_DISPATCHER_MODE=a2`. При выбранном
TOML ENV не подмешивается. Старые конфиги без этих ключей сохраняют legacy;
незнакомые ключи/значения и `[nats]` отвергаются. Старые Cosmic/Planet EXE
сохраняют свои прежние конфиги; Chat получает отдельный файл. API, schema marker,
форматы journal/сообщений прежние, поэтому новая Chat EXE совместима с текущими
Cosmic/Planet без их обновления. Windows runtime smoke на машине владельца не
выполнялся; проверены cross-build PE и Linux функциональность.

Будущий утверждённый механизм миграций, **не автоматизация этого релиза**:

1. Отдельное согласование DBA/владельца на конкретные БД, схему, окно, backup
   и rollback. Проверить восстановление, marker Chat=1, владельцев и отсутствие
   чужих одноимённых функции/trigger; runtime не использовать как DDL role.
2. Cosmic migration owner в одной короткой транзакции с явным quoted
   search_path выбранной схемы, lock_timeout/statement_timeout применяет
   [проверенный create-only UP](../../ideas/nats-event-bus/dba/sql/notify-up.example.sql).
   При конфликте — ROLLBACK, без CREATE OR REPLACE/CASCADE. Сверяет функцию,
   владельца, SECURITY INVOKER, fixed search_path, statement trigger и relacl.
3. После проверок отдельного стенда переключает только Chat на A2 с прежним
   журналом, контролирует listener, polling, ошибки COMMIT и
   `pg_notification_queue_usage()`. Chat сам DDL не выполняет.
4. Для отката выбрать `a1` либо `legacy`, остановить/перезапустить Chat, проверить
   завершение LISTEN-сеанса, авторизацию, отправку и историю.
5. Только затем DDL-владелец в отдельном согласованном коротком окне применяет
   [DOWN](../../ideas/nats-event-bus/dba/sql/notify-down.example.sql) в правильной
   схеме. Он удаляет только trigger/function, без CASCADE и удаления сообщений.
   При lock timeout прервать и оставить trigger до нового согласованного окна.
   Переключение режима само по себе не убирает цену/риск установленного trigger.

Текущая цепочка миграций Cosmic намеренно не менялась. Если позднее потребуется
её изменение, это отдельный согласованный versioned/idempotent migration;
копировать пример SQL в рабочую БД по этому отчёту нельзя. Повторная выдача
расширенных прав Chat не требуется и не предлагается.

## Ограничения и готовность к N3

A2 готов к функциональному эксперименту в согласованном изолированном контуре.
Присутствуют общий dispatcher, интерфейс источника, независимая сверка,
fallback, SQL ACL и проверенный режим отката. Для отдельного следующего этапа
B1 это пригодная основа; **команды начинать N3 пока нет**.

Production A2 остаётся отдельным решением: не проведены Windows smoke,
нагрузочная оценка, насыщение NOTIFY queue и отказ COMMIT от saturation;
проверка реальных LOGIN/прокси/настроек владельца тоже не подменяется fixture.
Не добавлены HA, durable WS replay или гарантия каждой исторической инвалидации.
Документы DBA сохранены как исторические результаты до реализации; актуальное
покрытие A2 Go описано здесь. Рабочие службы и PostgreSQL не менялись.
