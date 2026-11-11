# Astra Journey — DBA / PostgreSQL для NATS Event Bus

**Состояние: 9 ноября 2026. Проектирование завершено, SQL опубликован как НЕПРИМЕНЯЕМЫЕ примеры, PostgreSQL и игровой код не изменены.**

Аудирован HEAD main 1c922caf4e20347381e3f4a34709d9bf438279a4 с реализованным Chat Service v0.24.x; исходные замечания NATS stage III были подготовлены для прежнего commit, поэтому перепроверены действующие schema240.sql, Chat/ChatIntegration/ChatServer и DB-role code. Подготовка N0 на отдельном стенде — не подтверждение N1/N2/N3 в рабочем мире.

> **Актуализация от 2026-11-10 после Chat 0.24.7/N1:** DBA-проект A2 прошёл реальные проверки на изолированной PostgreSQL 17.11. [Итог D01–D26, EXPLAIN, роли, rollback и условное заключение по N2](../../../operations/nats-n2-dba/README.ru.md). Результат: **18 PASS, 0 FAIL, 8 NOT RUN**. Старые пометки «NOT RUN» ниже отражают статус на момент первоначального проектирования, до тестового этапа. Рабочая БД и schema*.sql не изменялись. Исходный notify-up.example.sql скорректирован: нет CREATE OR REPLACE FUNCTION, повторная установка проверяет сигнатуру, текст и владельца.

## Материалы

| Файл | Что в нём |
|---|---|
| [01 — Chat Outbox](01-chat-outbox-audit.ru.md) | Таблица, поля/индексы, INSERT, maintenance/retention, реальный Changes() и права |
| [02 — Indexes/Performance](02-indexes-and-performance.ru.md) | OR vs UNION ALL, планы, нагрузка 50–256, late COMMIT и безопасные альтернативы |
| [03 — LISTEN/NOTIFY](03-listen-notify-design.ru.md) | Выбор statement trigger A2, payload, схема канала, lifecycle/reconnect |
| [04 — Security/Roles](04-security-and-roles.ru.md) | chat_runtime/definer, definer functions, database privileges, private/corp/planet ACL |
| [05 — Migration Plan](05-migration-plan.ru.md) | Порядок тестового применения A2, предохранители, откат и нулевая DDL для B1 |
| [06 — Tests](06-test-matrix.ru.md) | D01–D26, SQL конкуренции, security checks, PR/P01 acceptance, фактический статус |
| [SQL UP — проект](sql/notify-up.example.sql) | SECURITY INVOKER, statement trigger, пустой pg_notify |
| [SQL DOWN — проект](sql/notify-down.example.sql) | Удалить только A2 trigger/function после переключения feature flag |
| [SQL diagnostics — проект](sql/diagnostics.example.sql) | DB roles, размеры, insert count, индексы и EXPLAIN для отдельной тестовой БД |
| [SQL concurrency — проект](sql/concurrency-tests.example.sql) | Сеансы A/B/L: COMMIT/ROLLBACK, late lower ID, несколько INSERT, reconnect |

**Ни один SQL-файл не включён в internal/server/schema*.sql; ни один SQL не запускался на рабочей или тестовой БД.** Это проект DBA для согласования с системным архитектором и backend.

## Решение DBA по этапам

- **N1/A1:** новые SQL-объекты не нужны, текущий outbox пригоден для общего batch dispatcher; backend оставляет SQL ACL.
- **N2/A2:** нужен минимальный trigger, если именно A2 выбран для эксперимента; предлагается AFTER INSERT FOR EACH STATEMENT + pg_notify('', schema-isolated channel) в одной транзакции. Не вводить новые таблицы и права chat_runtime. EXPLAIN и ограниченно-рольные тесты обязательны **до** разрешения пилота.
- **N3/B1:** текущая таблица годится, DDL не нужен, Core NATS — postcommit hint, SQL polling/reconcile остаются.
- **JetStream/Economic:** не проектировались миграции; будущий domain transactional outbox/inbox/receipts — самостоятельная задача, Core сигнал недостаточен для платежей.

**Индексы:** PK(id) и chat_outbox_recent(created_at,id) уже есть; без планов и статистики дополнительные НЕ рекомендованы. Индекс pending(id) частичный, новый scanner не зависит от него; не удалять без отдельного аудита.

**Критический риск:** id из BIGSERIAL не равен порядку SQL COMMIT. 60s overlap — экспериментальный bound, не гарантия при бесконечно долгой tx. Подтвердить max INSERT→COMMIT, safe poll interval и scheduling margin либо обеспечивать полную reconcile независимо от event stream. PostgreSQL 17 имеет transaction_timeout, но менять настройки без отдельного согласования запрещено.

## Что требует согласования

1. Признаёт ли backend 60s перекрытие только latency optimization с обязательной полным scope/state reconcile или требуется строгая доставка **каждого** committed hint? Последнее требует дополнительного протокола.
2. Кто и как устанавливает максимальный INSERT→COMMIT bound для **всех** Chat writers? Каков worst-case scan delay при DB busy?
3. Подтверждён ли выбор statement trigger для A2 и schema-isolated channel cosmos_chat_<md5(DB_SCHEMA)>? Нужна ли настройка имени в конфигурации Chat?
4. Какие production роли/владельцы/DB_SCHEMA реально работают (не из образца README)? Есть ли CREATE/EXECUTE и не существует ли одноимённый trigger?
5. Требуется ли A2 на production или достаточно A1/B1 после честного P01 benchmark? Не включать A2 по умолчанию без данных.
6. Какая верхняя граница degraded обнаружения и период scope/auth revalidation принимаются как продуктовый контракт?
7. Когда backend завершит N1/WSRegistry и запустит D01–D26, кто подписывает acceptance и rollback window?
8. Как контролировать pg_notification_queue_usage и error на COMMIT при долгом LISTEN transaction?

## Фактически проверено / не проверено

Проверено по текущему исходному коду: схема chat_outbox, два B-tree пути, outbox INSERT в Chat transaction, per-WS Changes(), maintenance DELETE 1 day, роли в GrantRuntime, фиксированные SECURITY DEFINER функции, требование current SQL ACL.

**NOT RUN:** тестовая PostgreSQL, реальные count/size/insert rate, EXPLAIN/BUFFERS, наблюдённые tx durations, 50/100/200/256 WS и latency/CPU. Изолированный стенд N0 не измеряет новые A2 SQL. Заявления о производительности являются расчётом/гипотезой.

**Допуск к N2 с позиции DBA: пока НЕТ.** Проект SQL готов для обсуждения и отдельной тестовой установки; до N2 нужны N1 baseline, PostgreSQL тесты D01–D26, подтверждение транзакционных границ и ролей, одобрение архитектора/владельца. Для production требуется отдельный допуск и backup/rollback.

## Связанные документы

- [NATS решение и handoff](../README.md), [общий dispatcher](../04-chat-dispatcher.ru.md), [пилоты](../05-pilots.ru.md), [отказы](../07-failures-recovery.ru.md).
- [Подготовка N0](../../../operations/nats-n0/README.ru.md), [актуальный Chat](../../../chat-service/README.ru.md), [schema240.sql](https://github.com/yomezza/cosmosimulator/blob/main/internal/server/schema240.sql).
- PostgreSQL 17: [LISTEN](https://www.postgresql.org/docs/17/sql-listen.html), [NOTIFY](https://www.postgresql.org/docs/17/sql-notify.html), [timeouts](https://www.postgresql.org/docs/17/runtime-config-client.html).
