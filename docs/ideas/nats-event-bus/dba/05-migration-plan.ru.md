# 05. План изменения PostgreSQL и отката для A2/B1

**Статус:** проект для отдельного согласования. Не является разрешением применить DDL к рабочей PostgreSQL, не внесён в internal/server/schema*.sql.

## Что меняется

A1: **ноль DDL**. Оставить текущие таблицы, индексы, роли и schema markers.

A2: один SECURITY INVOKER SQL trigger-function chat_outbox_notify_statement_v1() и один statement-level AFTER INSERT trigger chat_outbox_wakeup_a2 на существующей chat_outbox. Ни новых таблиц, ни новых последовательностей, ни новых индексов, ни изменений существующих столбцов. Вместо правки Go producers pg_notify вносится только в общий INSERT-path, с SQL COMMIT semantics. Файл [UP](sql/notify-up.example.sql).

B1: **ноль DDL**. Postcommit Core NATS publish реализует backend; тот же chat_outbox остаётся источником истины. Двойную таблицу NATS outbox не создавать. Для будущего Economic/JetStream отдельный проект domain-owned transactional outbox/inbox, aggregate version, durable receipts и recovery; без экономических миграций сейчас.

## Перед тестовым применением

1. Зафиксировать commit main, версии Cosmic/Chat/Planet, DB_SCHEMA, CHAT_ENABLED, chat schema marker=1, PostgreSQL 17, pg_notify availability. Сверить фактические роли, владельца chat_outbox/функций, список триггеров и индексы. Создать и проверить восстановимую копию **тестовой** схемы.
2. На изолированной DB_SCHEMA nats_dba_test применить действующие Cosmic миграции и штатный --grant-runtime. Не использовать shared production schema. Нельзя запускать одновременно N1 и A2 без конфигурации dispatcher.
3. В одной транзакции от Cosmic migration owner установить проверенный search_path единственной схемы; короткий lock_timeout (например 2s), statement_timeout 10s; проверить конфликт существующего имени функции/триггера, затем выполнить UP. Не держать транзакцию в ожидании сеть/пользователя.
4. Сверить pg_get_triggerdef, pg_proc.prosecdef, владельца и фактический канал; повторный запуск UP не меняет объекты, конфигурацию и права, не создаёт двойной trigger. При другой уже существующей функции того же имени — остановиться.
5. Прогнать SQL concurrency и ограниченно-рольные тесты: COMMIT/ROLLBACK, MULTI INSERT, late COMMIT, старый Chat, queue health. Только после этого разработчику разрешено включить A2 в отдельном стенде.

Для реального rollout нужен отдельный change request: снимок/rollback план, окно короткого DDL lock, подтверждение что старый Chat продолжает операции под ограниченной ролью, мониторинг очереди, пиковая скорость INSERT и существующие тесты восстановления. DDL применяет **Cosmic migration runner**; не запускать проектный SQL через Chat runtime. Встраивание в цепочку schema*.sql пока запрещено.

## Совместимость

Создание функции/триггера не меняет форму chat_outbox, результаты Changes(), историю сообщений, приватность и старые API. Старый Chat игнорирует NOTIFY. Тем не менее каждый INSERT в outbox выполняет дополнительный вызов, а очередь NOTIFY при переполнении может вызвать ошибку COMMIT: это отдельный риск, поэтому тест old Chat required, а не предположение о нулевой цене. Удаление trigger не удаляет outbox данные.

В производительности учесть стоимость: lock для CREATE TRIGGER, CPU per INSERT statement, WAL/notification queue и возможное влияние на COMMIT latency. Не нужен table rewrite, массовый backfill, индексная сборка или пересчёт географии. Объём текущей таблицы не влияет на создание простой функции так же, как на новый полный индекс, но lock ожидания при насыщенных INSERT всё равно нужно измерить. pg_notification_queue_usage() и долгие listeners обязательны к наблюдению.

## Режим отмены

1. В Chat feature flag переключить A2 на legacy либо A1/B1, сохранить обычный SQL safety polling и полноценные scope/auth checks; проверить отправку сообщения и read-time ACL.
2. Только после отключения A2 listener: Cosmic миграционным владельцем, с правильным DB_SCHEMA и короткими SQL timeout выполнить [DOWN](sql/notify-down.example.sql). В транзакции DROP TRIGGER, затем DROP FUNCTION RESTRICT.
3. Убедиться, что trigger отсутствует, исходные индексы/PK/права/данные сохранены. Переход к B1 не требует нового DDL и не зависит от удаления trigger для логической корректности, но для честного benchmark A2/B1 trigger надо выключить, иначе цена A2 скрыто останется.
4. При lock timeout — не повторять бесконечно и не останавливать Chat; оставить trigger до согласованного окна и включить прежнюю схему polling. Восстановление базы из backup не является стандартным rollback A2: данные сообщений должны сохраняться.

**Idempotence:** UP проверяет существующий trigger; идентичный повтор не создаёт ещё одного. DOWN использует IF EXISTS; не удаляет другие функции/триггеры и не применяет CASCADE. Любые DDL в рамках outer tx rollback целиком при ошибке.

## Вывод для N2

DB-схему менять **не требуется** для N1 A1 и N3 B1; для A2 N2 есть конкретный небольшой проект DDL. **Нельзя разрешать эксплуатационный N2** без подтверждения ролей, выполнения тестов на собственной PostgreSQL, определения maximum INSERT→COMMIT bound/альтернативы, сравнительных планов и завершения N1. После них N2 можно допустить к изолированному пилоту отдельным решением. Для production — ещё отдельное разрешение.
