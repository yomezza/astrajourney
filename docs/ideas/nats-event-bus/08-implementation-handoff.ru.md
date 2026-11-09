# NATS Event Bus — 08. Предварительный implementation handoff (без разрешения на код)

Дата: 2026-11-09. **Не финальное ТЗ Codex, не поручение на реализацию.** Архитектура B утверждена; точные механизмы/таймауты/режим Chat будут выбраны по экспериментам. В этапе III разрешены только Markdown в docs/ideas/nats-event-bus/.

## 1. Правила для будущего разработчика

- Не менять продуктовые правила Chat, Planet или Cosmic.
- Не менять существующий API/WS для браузера. Имеющиеся Cosmic proxy HTTP URLs продолжают работать.
- Не превращать NATS в dependency игровых команд: при broker down штатные сохранения/движения работают по старым транзакционным контрактам.
- Не пересоздавать таблицы пользователей, компаний, планетарных визитов, сообщений чата и erasure journal.
- Не читать/публиковать тело сообщений через NATS и не доверять incoming payload как авторизации.
- Не удалять current `Changes()` до измеренного rollback-ready завершения P-NATS-01.
- Не запускать SQL DDL из Chat или Planet runtime: новые схемы только через существующий Cosmic migration runner и согласование DBA.
- Не изменять рабочие *.toml, nats-server.conf или Windows Services без отдельного разрешения владельца.

## 2. Последовательность будущих этапов

| Шаг | Цель | Основные изменения | Gate |
|---|---|---|---|
| N0 | Проверка базовой конфигурации | Поднять отдельный тестовый NATS с TLS + static users/ACL, проверить файлы/версию и отказ; **не production** | TLS SAN, auth, negative ACL, transport down не останавливает игру |
| N1 | P01 A1 | Общий Chat Dispatcher/WSRegistry, local Go wake + common outbox polling | All ACL/concurrency/WS tests, baseline vs legacy |
| N2 | P01 A2 | PG LISTEN/NOTIFY source + DBA change, общий dispatcher неизменен | LISTEN race, commit/rollback, durable fallback |
| N3 | P01 B1 | Optional Core NATS source, client config, common dispatcher неизменен | broker restart/down 5m, benchmark vs A1/A2 |
| N4 | Решение по P01 | Анализ цифр и выбор Chat signal mechanism | Отчёт p50/p95/p99, pg_stat_statements, no leaks, rollback |
| N5 | P02 Planet → Chat | Postcommit sector.changed, spatial dirty + SQL ACL revalidation | Sector/epoch tests, multiple players, duplicates, fallback |
| N6 | Остальные Cosmic/Planet signals | Corporate revoke, grants, cleanup wakeups, visit start/end | Existing durable company/surface workflows unaffected |
| N7 | JetStream/Economic — отдельное ТЗ | Domain-owned outbox, puback, durable consumers, inbox/ack | Только после проектирования Economic и DBA/операционного согласования |

**Важное:** N0–N7 — проектная последовательность будущей работы. Сейчас эти этапы не запускать.

## 3. Карта Go-пакетов

| Пакет/файл | Будущее изменение | Ограничение |
|---|---|---|
| internal/chatserver/server.go | Декомпозиция per-WS Changes polling, общий dispatcher lifecycle, выбор режима, health деградации | Не менять пользовательский WS контракт, Authenticate, Claim/Renew lease |
| internal/chatserver/ws_registry.go (новый) | Один user_id → ConnHandle, generation, bounded sync mailbox | Только один writer, race-safe unregister |
| internal/chatserver/dispatcher.go (новый) | Coalescing, DB scan, audience resolution, forced reconcile | Ни один Core signal не grant access |
| internal/chatserver/signals.go / nats_signals.go (новые) | A1/A2/B1 WakeSource, reconnect, transport metrics | Noncritical signal errors не превращаются в game error |
| internal/chat/changes.go | Выделение batch reader; old code остаётся под legacy/test | Сохранить 15s overlap semantics, расширить overlap при более редком polling |
| internal/chat/dispatch_reader.go (новый/расширение) | Batch read committed chat_outbox | Проектировать индексы с DBA, не копировать message body |
| internal/chat/dispatch_acl.go (новый/расширение) | Current membership/fence/presence for candidates | Единая реализация для всех трёх режимов |
| internal/chat/model.go, messages.go, actions.go, private.go, maintenance.go | Postcommit signalling where mutation has committed outbox, если нужно | Нельзя публиковать внутри SQL tx до COMMIT |
| internal/planetserver/transitions.go | P02 postcommit notifier после successful visits.Transition | Не менять движения/portal/transition receipt |
| internal/planetserver/app.go, run.go | Optional NATS client lifecycle | Run game без брокера |
| internal/platform/configuration/document.go, configuration.go | New strict [nats], ENV mapping, validation, file resolution | CONFIG_FILE не смешивает TOML/ENV |
| configs/cosmos.example.toml, planet.example.toml, chat.example.toml | Только будущие документированные примеры | Не заменять реальные секреты |
| internal/corporations/commands.go, directory.go; internal/administration/chat.go | В будущем postcommit signals роли/членства/модераторов | Не изменять DB fences/transaction semantics |
| internal/platform/eventbus (возможный новый узкий пакет) | Общие NATS Go adapter, validation, metrics | Никакого обращения к game domain из платформы |

Потребитель Core NATS должен валидировать собственный schema_version/kind/subject, применять ограничения размера, быть неавторитетным и работать с ошибками переподключения. Версии Go-пакетов/библиотек зафиксировать только при фактическом изменении go.mod (запрещено на этапе III).

## 4. Отдельное задание будущему DBA

Это **список предметов проектирования**, не SQL-миграции:

| Вопрос | Пилот | Решение/проверка DBA |
|---|---|---|
| A2 NOTIFY inside COMMIT | P01 A2 | Минимальный trigger на chat_outbox AFTER INSERT (предпочтительно STATEMENT с NOTIFY один раз за транзакцию) или явный SQL call; grants, concurrency, SET search_path, version |
| Outbox retrieval | P01 все | EXPLAIN (ANALYZE,BUFFERS) по (id,created_at), условия OR/UNION, 60s overlap, индекс только если текущих недостаточно |
| Lower-ID late COMMIT | P01 все | Тест реальных PostgreSQL транзакций с искусственной задержкой и проверка max duration до выбора overlap |
| Корпоративная ревокация | P01/P02 | Доказать актуальность chat_company_access_fences и SECURITY DEFINER при массовой аудитории; менять SQL только по необходимости |
| Пространственная авторизация | P02 | Подтвердить chat_lock_planet_presence при переходе; без копии presence в Chat |
| Transactional domain outbox | Future Economic | Таблица владельца события, event_id, aggregate_id/version, published_at, leases, retries, storage TTL |
| Idempotent consumer inbox | Future Economic | UNIQUE(event_id), transactional apply+ack, gap detection, reconciliation |
| Restore PostgreSQL/JetStream | Future Economic | Сценарии recovery, сравнение source aggregate version и receipts, fail closed для неизвестных финансовых операций |

Только **Cosmic migration runner** применяет игровые DDL; функции Chat уже принадлежат ограниченным DB roles. DBA учитывает существующие SQL права и схему DB_SCHEMA. JetStream не заменяет SQL source-of-truth.

## 5. Acceptance gate для будущих PR

- Unit tests Go race, deterministic WS registry/queue/dedupe, 409 second connection, single writer, reconnect and shutdown.
- Integration PostgreSQL: correct COMMIT ordering, ROLLBACK, late lower ID vs greater ID, 1000 events burst, chat private invite+corporation+spatial ACL, 256 WS, session expire, changed company/grants without outbox.
- E2E Chromium/LAN: GET message after sync, Private cannot leak through channel hints or URL; outgoing user cannot continue spatial chat.
- Failure tests: NATS down at boot, 5m disconnect, broker restart, drop Core packet, listener reconnect, invalid payload, slow WS, DB timeout, simultaneous Planet+NATS down.
- Valid old TOML/ENV stays valid, CONFIG_FILE strict unknown-field test remains; nats.enabled=false means **no NATS connection attempt**; disabled services are not NATS-gated.
- Observability metrics and no secrets in logs; Windows x64 cross-build and real service smoke separately; do not assert performance without real pg_stat_statements/metrics.
- Reversible feature flag: legacy/A1/A2/B1, rollback to legacy and production DB schema stays intact.

## 6. Документы, которые нужно передать будущему исполнителю

- 01-current-architecture.ru.md — текущее состояние v0.24.4.
- 02-nats-decision.ru.md — утверждённая модель B, критерии выбора.
- 03-event-contracts.ru.md — Core and future JetStream contracts.
- 04-chat-dispatcher.ru.md — конкретные Go component/lifecycle/SQL/ACL algorithms.
- 05-pilots.ru.md — последовательные P-NATS-01 и P-NATS-02, tests, metrics, rollback.
- 06-configuration-security.ru.md — TLS, static auth, ACL, strict TOML, Windows.
- 07-failures-recovery.ru.md — полный список деградаций и восстановления.

Все документы — проектные, только вариант B и направление двух пилотов утверждены владельцем. Архитектор после будущих измерений может уточнить конкретный signal source, интервалы, scopes и потребность в JetStream.

## 7. Открытые решения перед настоящей реализацией

1. **Проверенная версия NATS server** на машине владельца, фактический parser/сертификат и способ запуска Windows Service. Текущий conf указан по данным владельца, не прочитан с работающего сервера.
2. Реальная PostgreSQL версия, tx duration, индексные планы chat_outbox, размер 15s→60s overlap.
3. Имеет ли преимущество B1 над A2 при 50/100/200/256 WS в LAN? Решается измерением, не предположением.
4. Минимально приемлемая degraded latency; предложение 2s на transport outage, 10s healthy, не обязательный продуктовый стандарт.
5. Выбор A2 NOTIFY trigger versus explicit SQL, и его влияние на transaction cost: DBA.
6. Детальная схема новой отдельной Economic Service и её обязательных receipts — отсутствует; JetStream пока только стандарт будущего.
7. Для длительного multi-host расширения понадобится отдельный анализ multi-instance Chat; нынешняя одиночная DB runtime lock не должна сниматься без нового проекта.
8. Backup/PITR Policy NATS/JetStream: одиночный сервер, текущие RPO/RTO не проверены. Их нельзя использовать для финансовых гарантий без отдельного операционного согласования.
9. Условия предоставления мониторинга 8222, CA/SAN, корпоративного auth файла и парольных секретов для безопасного пилота.
10. Необязательный дальнейший Request/Reply вариант C — только если будут реальные внутренние RPC с обоснованной выгодой.

## 8. Запреты текущего этапа

НЕ менять Go, SQL/миграции, go.mod/go.sum, реальные TOML, nats-server, Windows Services, frontend, игровые данные, публичные и внутренние HTTP/WS контракты. Создавать только Markdown под docs/ideas/nats-event-bus/. Не запускать эксперимент без следующего разрешения владельца.

**Готовность этапа III** определяется полнотой документов, а не сборкой NATS-клиента.