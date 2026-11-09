# 01. Аудит действующего chat_outbox и Changes()

**Основание:** main 1c922caf4e20347381e3f4a34709d9bf438279a4, 9 ноября 2026. Исходники проверены чтением; запросы к действующей PostgreSQL не выполнялись. Код Chat соответствует линии v0.24.4, но использован именно актуальный main. Данные ниже относятся к реализации, а не к ещё не выполненному P-NATS-01.

## Объекты и контракт

Источник: internal/server/schema240.sql. Таблица chat_outbox создана в текущей DB_SCHEMA:
- id BIGSERIAL PRIMARY KEY, последовательность через BIGSERIAL (типичное имя chat_outbox_id_seq; фактическую привязку проверить pg_get_serial_sequence).
- event_kind TEXT NOT NULL, channel_id UUID NULL, target_user_id BIGINT NULL, reference_id UUID NULL.
- created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), processed_at TIMESTAMPTZ NULL.
- Индексы: PK chat_outbox_pkey(id); chat_outbox_pending(id) WHERE processed_at IS NULL; chat_outbox_recent(created_at,id). Никакого универсального FK для channel_id/reference_id/target_user_id в outbox нет.
- Таблица НЕ является очередью принятых/подтверждённых сообщений, не содержит тела сообщений, не партиционирована.

internal/chat/messages.go:event() всегда выполняет INSERT chat_outbox в переданной pgx.Tx. События фиксируются вместе с изменением сообщения/канала/состояния при успешном COMMIT. При ROLLBACK не остаётся ни строки outbox, ни связанного изменения. Код в internal/chat/model.go:execute() выполняет SET LOCAL lock_timeout=2s и statement_timeout=5s, но это не серверное ограничение общей длительности транзакции. Другие пути (например, maintenance) имеют другие дедлайны.

Найденные места генерации:
| Источник | event_kind из текущего кода | Поля и смысл |
|---|---|---|
| messages.go | channel.changed | channel_id канала, reference_id сообщения; аудитория только после проверки kind и прав |
| actions.go | read.changed, channel.changed, moderation.changed | чтение/канал; moderation.changed содержит target_user_id |
| private.go | membership.changed, private.changed, invite.changed | membership/invite targeted, private.changed имеет NULL channel_id и reference_id разговора |
| maintenance.go | cleanup.completed | вызывается с NULL channel_id, NULL target_user_id, NULL reference_id; **не делать из NULL широковещательный сигнал** |

По функциям и SQL нельзя считать reference_id однородным внешним ключом: для channel.changed это ID сообщения, для private.changed — conversation ID, для invite.changed — иной идентификатор. Диспетчер обязан различать event_kind и валидировать указанный тип ссылки. Строки, у которых channel_id IS NULL, не означают global: только адресные события или специальные проверяемые private/invite изменения имеют аудиторию. Текущее cleanup.completed без адресата не участвует в fingerprint Changes(); обнаружение соответствующих удалений должно опираться на текущую БД и отдельную reconciliation.

## Текущий запрос Changes()

Источник: internal/chat/changes.go. Каждые ~2 s в каждом WebSocket (internal/chatserver/server.go) после Authenticate и возможного Renew:
1. Начинается транзакция READ COMMITTED (по умолчанию).
2. SELECT COALESCE(max(id),0) FROM chat_outbox.
3. SELECT chat_scope_stamp(user_id).
4. SELECT md5(string_agg(o.id::text,',' ORDER BY o.id)) по chat_outbox o LEFT JOIN chat_channels c, где (id > прежний cursor ИЛИ created_at > clock_timestamp()-15s), id <= latest и проверен target/kind/scope/компания/private/Planet.
5. COMMIT, возвращается latest и stamp:fingerprint. Поле bool в Changes сейчас возвращается false; вызывающая WS-логика сравнивает stamps сама.

Функция chat_scope_stamp из internal/chatintegration/schema.go входит в SECURITY DEFINER контракт: роль, moderator grant, company_id + chat_company_access_fences.revision, действующая Planet presence (surface/sector/epoch). Присутствие берётся через chat_lock_planet_presence (lease/fence, checked_at свежий не более 2s, runtime controls). Поэтому авторизация и охрана scope являются частью текущей стоимости одного опроса, а не бесплатной проверкой outbox. SQL для fingerprint включает подзапрос users.company_id и проверки company.chat_deleting / chat_members / chat_invites. Для пространственных событий он вызывает защищённую функцию присутствия.

**Важное свойство READ COMMITTED:** отдельные SELECT внутри одной tx могут видеть разные committed snapshots. Это не делает строку видимой до COMMIT; но нельзя принимать три чтения Changes за атомарный snapshot всего состояния. Новый dispatcher не должен делать из полученного stamp или outbox ID полномочие пользователя.

## Retention, вставки, ёмкость

В internal/chat/maintenance.go выполняется DELETE FROM chat_outbox WHERE created_at < clock_timestamp()-interval '1 day'. Worker вызывается примерно раз в 10s, с advisory lock по схеме; очистка выполняется внутри общей maintenance tx. Удаление outbox не подтверждает доставку, поле processed_at в актуальном коде не используется как курсор диспетчера. Нельзя менять его смысл или строить новые права на processed_at без контракта. DELETE не означает немедленное физическое освобождение: dead tuples требуют VACUUM, а текущий DELETE не ограничен LIMIT.

Фактические количество строк, bloat, размер relation+indexes, insertion rate и распределение event_kind **не измерены**: доступа к рабочему PostgreSQL нет. Для сбора: sql/diagnostics.example.sql. Проектные масштабы за сутки при равномерной интенсивности: 1 событие/s → 86 400 строк; 10/s → 864 000; 100/s → 8 640 000. Это арифметика, не наблюдение за игрой. Более одного event() может вызываться на одну игровую команду; частоту событий нужно измерять отдельно от message sends.

## Вывод

**Имеющаяся таблица пригодна** как авторитетный committed журнал *краткосрочных hints* для общего диспетчера A1/A2/B1. Новая таблица не требуется. Сначала сохранить event kinds и NULL semantics, добавить общий ограниченный сканер и безопасный RecipientResolver, затем измерить. Уведомление не заменяет SQL ACL или bootstrap; спустя >1 дня прекращает существовать история краткосрочных hints, а актуальное состояние читается непосредственно из базы.

Ссылки: [архитектура](../04-chat-dispatcher.ru.md), [риски](../07-failures-recovery.ru.md), [индексы](02-indexes-and-performance.ru.md).
