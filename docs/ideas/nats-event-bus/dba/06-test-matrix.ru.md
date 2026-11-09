# 06. Матрица DBA-тестов: PostgreSQL A1/A2/B1

**Статус:** сценарии подготовлены, НЕ запускались. Доступа к PostgreSQL тестового стенда в этой сессии нет; psql/postgres не установлены в доступной среде. Старые отчёты о пройденных тестах из docs/chat-service/REPORT.ru.md относятся к предыдущему выпуску, не к новым DBA-тестам.

## Изоляция и запуск

- Выделенная **тестовая** БД с именем *_test, отдельная DB_SCHEMA nats_dba_test, без общих данных с действующим миром. Проверить SELECT current_database(),current_schema() и отделение от рабочего пользователя перед первым SQL INSERT.
- Cosmic migration owner готовит schema240, Chat ограниченный runtime + definer через установленную процедуру. A2 UP в outer tx с SET LOCAL search_path и lock_timeout, затем down.
- Три psql соединения: A/B выполняют записи, L слушает (при необходимости L2 второй listener). Формат сценариев см. [concurrency-tests.example.sql](sql/concurrency-tests.example.sql); это ручные межсеансовые шаги, не линейный исполняемый тест.
- Перед/после набора: SELECT count(*), pg_relation_size, pg_stat_user_tables, queue_usage, EXPLAIN BUFFERS, количество/время SQL; сохранять необработанные логи без cookie/текстов сообщений.
- PostgreSQL source version 17; повторить для конкретной patch версии инсталляции. Полноценные браузерные 256 WS и Go race выполняет backend на отдельном стенде, не подменяются psql.

| ID | Проверка | Процедура/критерий |
|---|---|---|
| D01 | COMMIT | tx INSERT в chat_outbox, до COMMIT у L нет signal/row; после COMMIT ровно committed row и wake |
| D02 | ROLLBACK | INSERT и ROLLBACK; ни row, ни wake |
| D03 | 2–3 INSERT statements в tx | empty payload сворачивается PG; несколько триггерных вызовов, один доставленный identical NOTIFY на L |
| D04 | Multirow INSERT | 100 rows одним statement, один statement trigger; каждое событие сохраняется |
| D05 | Пустой INSERT | statement trigger даёт допустимый ложный wake; нулевое влияние на приватность/логику |
| D06 | 2 listeners | Оба L/L2 получают событие после COMMIT, не предполагают broadcast клиентам |
| D07 | Lower-ID поздний COMMIT | A выделяет id100, B id101 COMMIT раньше; A COMMIT в пределах 60s, SQL overlap его находит |
| D08 | Lower-ID >60s | A держит tx >70s; показать отсутствие lower id в WHERE при recorded highwater, подтвердить что 60s НЕ гарантия |
| D09 | SET transaction_timeout (test only) | Повтор D08 при ограниченном transaction_timeout; проверить фактический exception/ROLLBACK и работу pgx; bound согласовать |
| D10 | LISTEN registration | LISTEN+COMMIT перед initial SELECT; нет потерянного состояния на race startup |
| D11 | Reconnect LISTEN | Пропуск notify при disconnected; после reconnect + snapshot poll находит committed rows |
| D12 | UNLISTEN/неполученный wake | Отключить L, COMMIT, обычный scan находит запись; источник hint не обязателен |
| D13 | 1000+/s burst | Пакетный scanner 1000 rows/iteration; непропущенные pages, bounded memory, dedupe корректный |
| D14 | Роли | restricted chat_runtime отправляет сообщение с trigger; users/planet/fences writes/DDL запрещены, functions fixed path |
| D15 | Старый Chat | Legacy Changes() и Send/History работают после UP, после DOWN, без А2 подписчика |
| D16 | Компания сменена | users.company_id/role и company_roles revisions без outbox: generic sync не раскрывает прежний канал, GET SQL reject |
| D17 | company deletion | chat_deleting, durable request/receipt; Chat cleanup завершается независимо от NOTIFY/NATS, нет чужого corp access |
| D18 | Sector | Planet transition/fence/checked_at меняются без outbox; old sector access fail closed; after reconnect корректный scope |
| D19 | Private и invite | NULL channel/private.changed: только current members/pending sender/recipient; revoked/expired invite не раскрывает переписку |
| D20 | Session/WS lease | expired session/old generation не может получать sync или читать данные; сбой event source не продлевает доступ |
| D21 | Retention | maintenance удаляет outbox >1 day; новый WS bootstrap/periodic reconciliation из DB, без replay обещаний |
| D22 | 0,1k,100k,1m rows | OR vs UNION ALL EXPLAIN(ANALYZE,BUFFERS), event rate, reads, waits, WAL, vacuum dead tuples |
| D23 | 50/100/200/256 WS | baseline legacy/A1/A2/B1 при равной нагрузке; p50/p95/p99, SELECT/s, DB CPU, private leaks=0 |
| D24 | Queue saturation/long listener tx | pg_notification_queue_usage, COMMIT errors, алерт и отключение trigger как recovery |
| D25 | A2 ↔ legacy rollback | disabled A2 source, polling работает; DOWN удаляет только trigger+function, данные/права не затронуты |
| D26 | Schema isolation | Две DB_SCHEMA одной БД: разные channel md5(schema), нет перекрёстных полезных wake; DB schema fencings сохраняются |

## Acceptance и доказательства

- **P0:** D01–D12, D14–D21, D25–D26. Никакого unauthorized body/channel/ref, никакого просроченного доступа. Получение generic sync не является авторизацией.
- **Нагрузка:** D13, D22, D23 с неизменной базой/seed/workload и одинаковым dispatcher. Архитектурные цели (допускаются изменения только после согласования): ≥90% сокращение SQL *outbox detection polling* на 200/256 WS; healthy LAN p95 COMMIT→generic sync ≤300ms; целостность и отсутствие ACL leakage; нулевые некомпенсируемые потери после broker/listener outage.
- **Отказы:** A2 listener crash, NATS down 5min, PostgreSQL restart, missed NOTIFY; после восстановления initial scan/full reconcile и полная проверка прав.
- **Измерение:** сохранять raw run logs, EXPLAIN JSON/TEXT, настройки PostgreSQL и Go версии, таймауты, data volume, пиковые queue usage, фактические test statuses PASS/FAIL/SKIP. Без данных статус остаётся NOT RUN.

## Оценка полноты baseline

В main уже есть internal/server/integration/chat240_test.go: TestChat240LateCommittedOutbox (проверяет overlap старого Changes()), TestChat240RestrictedRole, TestChat240CorporateFencesAndCleanup, TestChat240Transactions; chat240_spatial_test.go, chat240_recovery_test.go, chat240_transport_test.go (тест 50 соединений). Эти тесты полезны, но не доказывают correctness новых A2 триггеров и общего dispatcher, не подтверждают 60s и 256 WS. Повторить их после изменений.

## Отчёт о выполнении этой DBA-задачи

- Проверка исходников и конфигураций: **выполнена чтением GitHub**.
- Публикация SQL/материалов: **выполнена**.
- PostgreSQL: **NOT RUN (нет предоставленной тестовой DB / psql)**.
- EXPLAIN (ANALYZE,BUFFERS): **NOT RUN**.
- Измерения 50/100/200/256 WS: **NOT RUN**.
- Применение миграции/DDL: **NOT APPLIED**.

Данный документ задаёт следующий воспроизводимый тест, но не подменяет результаты.
