# 03. LISTEN/NOTIFY — решение A2

**Предложение DBA, не применено.** Для P-NATS-01 A2 выбираю PostgreSQL AFTER INSERT **FOR EACH STATEMENT** trigger на chat_outbox, с вызовом pg_notify(channel,'') из той же SQL-транзакции. PostgreSQL 17 поддерживает такую конструкцию. Подтверждение корректности требует тестовой БД.

## Почему trigger

| Свойство | AFTER INSERT statement trigger | Явный pg_notify из каждого Go пути |
|---|---|---|
| Атомарность с INSERT и COMMIT | естественная; ROLLBACK отменяет | только если вызван до COMMIT в той же tx |
| Покрытие будущих Chat writers | каждый INSERT в таблицу | нужно не забыть каждый путь, включая maintenance |
| Накладные расходы | одна функция на INSERT statement | одна функция на явный SQL вызов; может быть меньше при редких writes |
| Поддержка | DDL/rollback в Cosmic migration runner | изменения нескольких Go путей; риск ошибки |
| Runtime privileges | SECURITY INVOKER, pg_notify, без CREATE | тоже без CREATE |
| Ложные пробуждения | INSERT ... SELECT 0 строк вызовет statement trigger | можно избежать |
| Контроль количества | несколько INSERT statements/tx, но одинаковый channel+payload PostgreSQL дедуплицирует в tx | требует дисциплины всех callers |

Row trigger выполняется для каждой вставленной строки; при фиксированном channel+payload PG может свернуть уведомления, но стоимость вызовов функции остаётся пропорциональной количеству rows. Поэтому statement-level без transition table подходит текущему Chat, где event() делает INSERT VALUES одной строки. В будущем массовая вставка одним SQL заявлением → один вызов. Поскольку LISTEN лишь wake, редкий ложный сигнал допустим.

PG dedup: при повторном NOTIFY одного channel с **идентичным** payload в одной транзакции слушатель получит одно уведомление. Это не означает, что statement trigger срабатывает только один раз на tx. Между транзакциями уведомления не схлопываются. При ROLLBACK ни вставки, ни уведомления.

## Форма и изоляция

- Канал: cosmos_chat_ + md5(физического TG_TABLE_SCHEMA), например имя из SELECT 'cosmos_chat_'||md5(current_schema()). Изоляция по database у PostgreSQL уже есть; отдельное имя на каждую DB_SCHEMA предотвращает лишние wake между несколькими играми в одной базе.
- Имя канала не является секретом и не является механизмом ACL: PostgreSQL LISTEN/NOTIFY не предлагает permission-based каналов. Доверять данным NOTIFY нельзя; payload всегда пустой.
- Тело сообщения, user_id, channel_id, conversation_id, токен, приватные ссылки и авторизация в payload запрещены.
- Функция chat_outbox_notify_statement_v1() SECURITY INVOKER, SET search_path=pg_catalog и pg_catalog.pg_notify. Физическую схему берём из TG_TABLE_SCHEMA, а не из изменяемого search_path соединения.
- Runtime Chat имеет права INSERT на chat_outbox, но не CREATE/TRIGGER на игровую схему. Функцию и trigger создаёт Cosmic migration owner; новый SECURITY DEFINER и новые роли не нужны.
- Пока trigger не установлен, old Chat работает без изменения. После установки old Chat продолжает писать в прежнюю таблицу, теперь с добавочным NOTIFY; listener необязателен.

[Проект UP](sql/notify-up.example.sql), [DOWN](sql/notify-down.example.sql), [сценарии](sql/concurrency-tests.example.sql).

## Важное ограничение: NOTIFY может влиять на COMMIT

При заполнении PostgreSQL notification queue транзакция, вызвавшая NOTIFY, может потерпеть ошибку COMMIT. Следовательно, trigger — не абсолютно бесплатная и безрисковая оптимизация для старого Chat. Необходимы pg_notification_queue_usage(), алерты и контроль долгих транзакций слушателей; до rollout нужен тест заполнения и безопасный быстрый rollback. Об этом прямо сказано в PostgreSQL 17 NOTIFY. Следует избегать длительных SQL транзакций на listening connection: доставка откладывается до их завершения.

## Lifecycle слушателя

1. Выделенная SQL connection (pgx.Conn либо закреплённый pgxpool.Conn) с тем же DB_SCHEMA и проверенным PostgreSQL пользователем; LISTEN привязан к **одному сеансу**, не переносится между соединениями pool.
2. Вычислить канал по current_schema() этого соединения. LISTEN с корректно экранированным идентификатором; если в явной tx — COMMIT.
3. Только после зафиксированного LISTEN считать initial outbox snapshot + full scope reconcile. Это закрывает startup race; повторы harmless.
4. Далее WaitForNotification с bounded timeout/cancel; каждое событие только coalesced wake общего scanner, никогда per-user ACL grant.
5. При разрыве/reconnect: degraded 2s DB polling, новое LISTEN+COMMIT, initial scan/full reconcile, потом normal 10s safety poll. При потере одного NOTIFY данные восстанавливаются сканом.
6. Даже если LISTEN healthy, сохранять полноценные Bootstrap/Auth/Renew/scopeStamp/GET ACL, включая private membership, corporation fence и Planet freshness.

Никаких SQL изменений для A1 или B1 не требуется: B1 получает postcommit Core NATS wake, но читает тот же chat_outbox.

## Сопоставимые тесты P01

COMMIT/ROLLBACK, 1/100 INSERT в statement, 3 statements в одной tx, 2 listeners, late lower-ID commit, restart listener, unsubscribe/потеря сигнала, 1000 events burst, одновременно с maintenance, проверки old Chat и restricted chat_runtime. В A1/A2/B1 менять только источник wake при одинаковом scanner, recipient resolver и наборе пользователей. Payload и имя канала не используются как доступ к данным.

Источник: PostgreSQL 17 [NOTIFY](https://www.postgresql.org/docs/17/sql-notify.html), [LISTEN](https://www.postgresql.org/docs/17/sql-listen.html).
