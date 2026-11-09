# 04. PostgreSQL роли, функции доступа и границы безопасности

**Срез:** main 1c922caf, internal/chatintegration/grants.go, schema.go, schema240.sql, chat/access.go, chat/changes.go, chat/query.go, chatserver/server.go. Аудит исходников, без проверки GRANT в работающей БД.

## Разделение владельцев

- Cosmic migration role — единственный runner DDL, вызывает initialization.OpenDatabase при CHAT_ENABLED=true; schema240.sql и chatintegration.Functions/Partitions. Для новой A2 функции/триггера нужна существующая привилегия владельца chat_outbox и CREATE FUNCTION в схеме (либо эквивалентные права). Новую версию схемы/маркера нельзя добавлять без согласования.
- chat_runtime — LOGIN NOINHERIT, USAGE выбранной DB_SCHEMA и SELECT/INSERT/UPDATE/DELETE на перечисленных chat_* таблицах; USAGE,SELECT на chat_outbox_id_seq. В grants.go **не** предоставляются CREATE на схему или TRIGGER на chat_outbox, UPDATE игровых users/companies/planet_visits, прямой доступ к sessions.token_hash. Нельзя повышать до владельца.
- chat_definer — NOLOGIN NOINHERIT NOSUPERUSER и узкие SELECT/UPDATE для функций доступа. Владеет только пятью SECURITY DEFINER SQL-функциями (после --grant-runtime). Доступ к ним Chat получает через EXECUTE; PUBLIC отозван. Действующие definer функции имеют фиксированный search_path = DB_SCHEMA, pg_catalog.
- Planet отдельный writer planet_visits; Cosmic владеет company/user/migration/fence/финансами. Ни A2, ни NATS не передают право записи в их таблицы Chat.

Функция A2 **SECURITY INVOKER** (а не DEFINER) и SET search_path=pg_catalog; TG_TABLE_SCHEMA физически указывает на схему chat_outbox. Её владельцем остаётся Cosmic DDL/migration owner. REVOKE EXECUTE FROM PUBLIC применяется внутри той же миграционной tx, чтобы не оставлять временно открытую функцию. Прямое EXECUTE Chat не требуется для записи через уже установленный trigger; это нужно обязательно подтвердить ограниченно-рольным интеграционным тестом. Создание trigger никогда не выполняется пользователем chat_runtime. pg_notify не изолирует слушателей по роли и потому payload пуст.

## Права, которые защищают данные

- chat_validate_session проверяет действующую сессию через definer без выдачи Chat прямого SELECT token_hash/password_hash.
- chat_lock_company_access содержит row/fence guard для корпоративного владения, роли и chat_deleting. chat_company_access_fences.revision обновляется Cosmic-owned триггерами в тех же tx на users/company_roles/companies; dispatcher читает текущую ревизию, не пересчитывает её из события.
- chat_lock_planet_presence сверяет active visit, runtime lease owner/fence, enabled controls, geography revision и свежесть checked_at (порог 2s). Подписка на старый сектор никогда не восстанавливается по NATS/NOTIFY.
- chat_lock_service_scope применяется для модераторских операций. chat_scope_stamp содержит role, grant, company_id/revision, Planet surface/sector/epoch; он НЕ является доказательством разрешения читать чужую историю без её собственной проверки.
- Private: статус chat_members, state conversation, pending chat_invites, floor/restrictions и срок приглашения проверяются на SQL read и mutation. NULL channel у private.changed требует специальной адресной проверки. Завершение/удаление приглашения в maintenance может происходить без outbox; истечение по времени также не превращается в NOTIFY.

**Непременные проверки в Go и SQL:** на каждом WS/в установленный cadence Authenticate; Claim/Renew/Release с поколением lease; read-time ACL в Chat API; restricted user scope и role revision; scopeStamp независимо от outbox; Planet freshness; fail-closed при пропаже Cosmic/Planet и БД; запрещённый sender и истёкший инвайт отвергаются в транзакции. Fanout выдаёт только generic sync, без Private IDs/body. ACL до enqueuing generic sync полезен для сокращения лишнего трафика, но **не заменяет** проверку в обработчике HTTP GET.

## DBA отрицательные тесты

Под chat_runtime:
1. Успешный INSERT через обычный Chat send после установки A2; при COMMIT listener получает пустой wake и History разрешённого пользователя читает текст.
2. UPDATE users / companies / planet_visits / fences — DENIED; SELECT password_hash, sessions.token_hash и SELECT FOR SHARE users — DENIED.
3. CREATE FUNCTION, CREATE TRIGGER, ALTER TABLE chat_outbox — DENIED; has_schema_privilege(CREATE)=false.
4. EXECUTE только нужных SECURITY DEFINER функций, PUBLIC execute отозван. Проверить pg_proc.proowner и proconfig (search_path).
5. Новая функция не SECURITY DEFINER (prosecdef=false), владелец Cosmic; LISTEN другого пользователя не предоставляет никакого чтения chat_* таблиц.
6. Смена company/role/moderator, Planet sector и Private access без outbox не позволяет получить закрытые сообщения даже при задержанном sync.

Контроль фактических GRANT/ownership на тесте: запросы в sql/diagnostics.example.sql плюс pg_proc, pg_roles и information_schema.role_table_grants. Сверить отдельные chat runtime/definer роли на **реальной установке** перед допуском N2, не выводить фактические permissions из шаблона README.

## Риски и согласования

Одновременная DDL миграция и active Chat writes требует короткого lock_timeout и предварительного backup. ALTER FUNCTION OWNED BY chat_definer может мешать замене функций Cosmic, но A2 не трогает эти пять функций. Общий DB_SCHEMA обязателен: нельзя без проверки брать public или указывать чужую схему. Имя канала не security boundary; нельзя передавать в NOTIFY никаких PII или credentials.

Ссылки: [полный runtime GRANT в коде](https://github.com/yomezza/cosmosimulator/blob/main/internal/chatintegration/grants.go), [SQL-функции](https://github.com/yomezza/cosmosimulator/blob/main/internal/chatintegration/schema.go).
