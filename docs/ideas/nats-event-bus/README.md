# Astra Journey — NATS Event Bus (этап III)

Статус на 2026-11-09: **архитектурное проектирование опубликовано; программирование NATS не разрешено и не выполнялось.** Аудированный срез — main `6f75b58e0f02f918b19d726baefe6d3aafa71859`, релиз v0.24.4. **Во время публикации появился v0.24.6:** сравнением подтверждено, что основные исследованные Chat/Planet event и dispatcher исходники не менялись; изменены frontend и версия Cosmic. Перед пилотами обязательно перепроверить новый main и заново измерить baseline.

## Решение владельца

После этапа III владелец отдельно разрешил **эксплуатационную подготовку N0**:
[отчёт, шаблоны и проверки существующего cosmoNATS](../../operations/nats-n0/README.ru.md).
Это не разрешение на N1 или изменение игровых Go-служб. В N0 уточнены реальные
`cosmos` login, `auth.config` и лимит 512MB; прежние примеры этапа III остаются
историческими проектными материалами, применять их автоматически нельзя.

Затем отдельным заданием разрешён и реализован **N1 — общий Chat Dispatcher A1**:
[код, результаты проверок и legacy/A1 benchmark](../../operations/nats-n1/README.ru.md).
Это локальные Go wakeups и PostgreSQL polling, без подключения NATS. `legacy`
остаётся default. N2 и дальнейшие транспорты по-прежнему требуют отдельной команды.

**УТВЕРЖДЕНО:** вариант B — NATS для внутренних событий и уведомлений с сохранением PostgreSQL, Cosmic HTTP gateway, существующих HTTP/WebSocket игровых контрактов, текущей авторизации/фенсинга и механизма восстановления. Вариант C (NATS Request/Reply) — только возможное расширение при доказанной пользе. Направления P-NATS-01 и P-NATS-02 приняты для будущего экспериментального внедрения, но **не разрешены к реализации в текущем этапе**.

**Основные ограничения:** Core NATS — ephemeral best-effort wakeups, не источник истины и не разрешение на чат; JetStream — только для будущих реально долговечных доменных событий. При отказе NATS обычные Chat/Planet действия сохраняют проверенные PostgreSQL и runtime-контракты. `CHAT_ERASURE_JOURNAL` остаётся независимым от брокера.

## Навигация по документам

| Документ | Содержание | Статус |
|---|---|---|
| [01 — Current Architecture](./01-current-architecture.ru.md) | Аудит v0.24.4: Cosmic/Planet/Chat, реальная частота Changes(), DB/WS/HTTP | Проверено по коду |
| [02 — NATS Decision](./02-nats-decision.ru.md) | Сравнение A/B/C/D; архитектура B и ограничения | Вариант B утверждён; настройки предложены |
| [03 — Event Contracts](./03-event-contracts.ru.md) | Версионированные Core subjects/payload и будущий JetStream/transactional outbox | Предложенные контракты |
| [04 — Chat Event Dispatcher](./04-chat-dispatcher.ru.md) | Общая регистрация WS, batching/coalescing, ACL, SQL outbox, overlap, session/lease, fallback | Проект Go-логики |
| [05 — Pilots](./05-pilots.ru.md) | P-NATS-01 A1/A2/B1, P-NATS-02 sector changed, metrics/acceptance/rollback | План экспериментов |
| [06 — Configuration and Security](./06-configuration-security.ru.md) | TLS, static NATS users, subject ACL, Windows Service, strict CONFIG_FILE/TOML | Проект конфигурации |
| [07 — Failures and Recovery](./07-failures-recovery.ru.md) | Матрица деградаций, поздние COMMIT, JetStream/different backups, fail-closed правила | Проект отказоустойчивости |
| [08 — Implementation Handoff](./08-implementation-handoff.ru.md) | Будущие изменения Go, отдельная работа DBA, N0–N7, тесты и открытые решения | Предварительный handoff, не финальное ТЗ |

## Архитектура кратко

    Браузер
      │ HTTP/WS — без изменений
      ▼
    Cosmic Gateway ──► Planet HTTP/WS
      │                └─ PostgreSQL authoritative visit + epoch
      ├──────────────► Chat HTTP/WS (один активный WS на игрока)
      │                └─ PostgreSQL messages + chat_outbox + ACL
      └── PostgreSQL (DB_SCHEMA, только Cosmic применяет миграции)

    Cosmic / Planet / Chat ─► Core NATS signals ─► Chat Dispatcher
                                 │
                                 └─ Необязательные notifications.
                                    При отказе — DB polling/reconciliation.

    В будущем: Economic-owned transactional outbox
       ─► JetStream durable events ─► идемпотентные consumers.

NATS не переносит игровую авторизацию и не заменяет надёжные PostgreSQL cleanup requests/receipts при удалении корпорации. События о смене сектора после COMMIT лишь заставляют Chat повторно проверить Planet-owned current visit. Присутствие, user/company права и idempotency по-прежнему подтверждаются SQL.

## P-NATS-01: Chat Dispatcher

**Задача:** заменить индивидуальный тяжёлый вызов Changes() каждые 2s на один общий bounded scanner Chat Service и безопасный fanout, не меняя read-time ACL.

Измеряем с **одинаковым диспетчером и одинаковой проверкой разрешённых адресатов**:

- A1: local Go wake + общий DB polling.
- A2: PostgreSQL LISTEN/NOTIFY + общий DB polling.
- B1: Core NATS wake + общий DB polling.

**Предварительные параметры:** normal safety poll 10s, degraded 2s, outbox overlap 60s, coalescing 50ms; реальные значения корректируются по тестам. 30s poll + существующий 15s overlap **не считать безопасной заменой**: lower outbox ID может COMMIT позже higher. Сессии, scopeStamp, WS lease, SQL ACL и внешний erasure journal не отключаются.

**Контроль:** 50/100/200/256 WS; PostgreSQL statements/s, CPU, p50/p95/p99 COMMIT→sync, потерянные hints, поздние COMMIT, broker down 5m, race/ACL, rollback to legacy. **Цели не считаются достигнутыми без тестов.**

## P-NATS-02: Planet → Chat

Реальный `internal/planet/visits/transition.go` фиксирует переход SQL COMMIT после обновления planet_visits и planet_transition_receipts. Postcommit Core signal `cosmos.v1.signal.planet.sector.changed` инициирует принудительную сверку current sector/epoch/lease в Chat. Отдельный presence_revision для первого эксперимента не требуется, потому что сигнал **не** применяется как state, а запускает recheck. При NATS outage старый DB path/fallback остаётся.

## Безопасность

Текущий сервер владельца: `192.168.111.44:4222`, первоначальный мониторинг `192.168.111.44:8222`, JetStream store `D:/cosmosimulator/nats/data`. В 06 предложено защищать клиентские соединения TLS и раздельными static NATS usernames/password files; monitoring перенести на доверенный loopback. **Это не менялось на реальном сервере.**

Не смешивать *.creds (JWT/NKey) с login/password. Все реальные конфигурации и секреты вне репозитория. Клиентские HTTP URL Cosmic и Chat не удалять.

## Для разработчика / DBA

- [08](./08-implementation-handoff.ru.md): будущее внедрение только после отдельного разрешения; не изменять runtime SQL или NATS Server в этапе III.
- DBA прорабатывает только обоснованные DDL: возможный PostgreSQL trigger для A2 LISTEN/NOTIFY, индекс/EXPLAIN по outbox, будущие durable domain outbox/inbox для Economic. **Готовые миграции не генерировать без DBA.**
- Экономическая служба пока отдельный будущий проект: её финансовые операции не могут считаться завершёнными по одному Core signal или PubAck без собственного durable подтверждения.
- GitHub в рамках этого этапа менялся **только** в каталоге docs/ideas/nats-event-bus/.

## Источники

- Репозиторий: https://github.com/yomezza/cosmosimulator
- Действующая конфигурация: https://github.com/yomezza/cosmosimulator/blob/main/docs/configuration.ru.md
- Действующий чат: https://github.com/yomezza/cosmosimulator/blob/main/docs/chat-service/README.ru.md
- NATS Core: https://docs.nats.io/learn/core-nats/publish-subscribe
- NATS JetStream: https://docs.nats.io/concepts/jetstream
- NATS ACL: https://docs.nats.io/learn/security/authorization
- PostgreSQL LISTEN: https://www.postgresql.org/docs/current/sql-listen.html

**Следующий шаг:** согласование опубликованных контрактов и измерительных критериев с владельцем. До отдельного разрешения не запускать P-NATS-01, P-NATS-02 и не менять код/рабочие конфиги.
