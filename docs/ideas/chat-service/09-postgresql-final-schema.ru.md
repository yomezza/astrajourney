# Chat Service — PostgreSQL v1.1: окончательный каталог объектов и транзакционные контракты

Дата 2026-10-30. **Дополнение-исправление к 04**. Сначала читать [08](./08-strict-audit-errata.ru.md); новые поля/таблицы 09 имеют приоритет над первоначальной строкой перечня 04. Код/DDL в БД не вносились.

## 1. Нормализованная схема объектов

Все **новые** объекты СУБД именуются с префиксом chat_. Имена существующих таблиц Astra Journey (users/sessions/companies/planet_visits/planet_surfaces/planet_sectors) не меняются. Для SQL всё в текущем DB_SCHEMA.

### 1.1. Scope / беседы

| Объект | Поля и инварианты | Критичный индекс |
|---|---|---|
| chat_schema_state | PK id=1, version, applied_at | PK |
| chat_channels | id UUID PK, kind TEXT enum global/planet/sector/corporation/private, surface_id, sector_number, company_id, conversation_id, next_seq BIGINT NOT NULL DEFAULT 0, created_at; CHECK ровно допустимый для kind scope | отдельные UNIQUE partial: global singleton, planet(surface), sector(surface,sector), corp(company), private(conversation) |
| chat_conversations | id UUID PK, type enum dm/group, state pending/active/deleting, owner_id BIGINT, title TEXT, revision BIGINT, created_at, activated_at, delete_started_at; CHECK revision>=0 | owner_id/state |
| chat_dm_pair_locks **NEW** | low_user_id BIGINT, high_user_id BIGINT, CHECK low<high, PK(low,high); долговечная row-lock сущность для сериализации отсутствующей пары | PK |
| chat_dm_pairs | PK(low_user_id,high_user_id), UNIQUE conversation_id, FK conversation(id). Содержит pending и active DM; после conversion в group запись удаляется | PK, UNIQUE conversation_id |
| chat_members | PK(conversation_id,user_id), status active/left/kicked; joined_at,left_at, join_floor_seq BIGINT, unread_count BIGINT DEFAULT 0 CHECK>=0 | (user_id,status), (conversation_id,status) |
| chat_invites | id UUID PK, conversation_id, sender_id,recipient_id,status pending/accepted/rejected/expired/cancelled, created_at,expires_at,decided_at; FK conversation ON DELETE CASCADE только для invitation records, cooldown отдельно | UNIQUE(conversation_id,recipient_id) WHERE status='pending'; (status,expires_at) |
| chat_invite_rate_locks | PK(sender_id,recipient_id); запрещает параллельное превышение 20/24h | PK |
| chat_invite_attempts | id UUID PK, sender_id,recipient_id,conversation_id UUID без FK, dm_low_id,dm_high_id для DM-cooldown, attempted_at, status, rejected_at | (sender_id,recipient_id,attempted_at DESC); (dm_low_id,dm_high_id,rejected_at DESC); (conversation_id,recipient_id,rejected_at DESC) |

*Почему дополнительная lock-таблица обязательна:* SELECT FOR UPDATE отсутствующей chat_dm_pairs строки не блокирует «отсутствие» в PostgreSQL READ COMMITTED; создание под уникальным ограничением само по себе приведёт к конфликтам, но не обеспечивает договорённое поведение взаимного accept. Сначала INSERT pair_lock ON CONFLICT DO NOTHING, затем FOR UPDATE; всегда min/max порядок.

### 1.2. Сообщения и состояния прочтения

| Объект | Поля / ограничения | Ключевой индекс |
|---|---|---|
| chat_messages | PARTITION BY LIST(kind) → RANGE(sent_at); PK(kind,sent_at,id); channel_id,seq,author_id,message_type user/system,body,origin_sector_number INT NULL, reply_to_id UUID NULL, entities JSONB DEFAULT '[]',system_code/system_data,expires_at | каждой секции (channel_id,seq DESC); (expires_at,id); GIN lower(body) gin_trgm_ops |
| chat_message_locator | id UUID PK, kind,sent_at,channel_id,seq,author_id,expires_at; UNIQUE(channel_id,seq). В tx 1:1 с chat_messages без FK на partition parent | PK; (channel_id,seq DESC); (expires_at,id) |
| chat_read_receipts | PK(user_id,message_id); message_id FK locator ON DELETE CASCADE; read_at; пользователь должен иметь private ACL на момент ack | (message_id,read_at), (user_id,read_at DESC) |
| chat_read_marks | PK(user_id,channel_id), last_seen_seq; публичные индикаторы без numeric counter | PK |
| chat_notifications **NEW** | id UUID PK, user_id BIGINT, type TEXT, ref_id UUID NOT NULL, channel_id UUID NULL, created_at, seen_at NULL, expires_at TIMESTAMPTZ NULL (операционный TTL по типу, например 30 дней); **без текста**. Рекомендуемый dedupe UNIQUE(user_id,type,ref_id) | (user_id,seen_at,created_at DESC), UNIQUE |
| chat_rate_state | PK(user_id,kind), last_sent_at TIMESTAMPTZ; global/planet/sector/corporation, плюс private/report как mutex rows | PK |
| chat_rate_events | PK(user_id,operation_id), kind TEXT CHECK(kind IN ('private','report')), sent_at; private SEND 10/10s, report 5/10min | (user_id,kind,sent_at DESC); GC после >11min |

*Расширение к 04:* entities JSONB хранит только структурированные spans (entity kind, stable ID, start/end offset, validator version). Независимая пользовательская строка остаётся body. При display offsets в JS должны быть согласованы с Unicode (использовать серверно заданную UTF-8/codepoint позиционную схему либо рендеринг в Go-derived segments, не индексы JS UTF-16 по умолчанию). Для game links клиент не может выбрать произвольный href. system_data JSONB whitelist, без пользовательского текста/причин наказания для общего канала.

*Выбор по unread:* для <=50 участников точные chat_read_receipts сохраняют смысл «сообщение действительно было видно». Частотная цена — одна запись на прочтённое сообщение, без второго копирования текста. Если PostgreSQL нагрузочные тесты покажут рост до неприемлемого уровня — отдельная оптимизация с компактными read ranges, без смены product semantics.

### 1.3. Избранное/настройки/жалобы

| Объект | Поля | Индексы |
|---|---|---|
| chat_saved_items | id UUID PK,owner_id,folder_id NULL,kind note/copy,body TEXT,source_message_id UUID NULL (не FK),source_channel_id UUID NULL,source_kind,author_id,author_name_snapshot,original_at,surface_id,sector_number,saved_at,updated_at; для note original/source NULL | (owner_id,saved_at DESC,id), (owner_id,folder_id,saved_at DESC), (source_message_id) WHERE NOT NULL |
| chat_folders | id UUID PK,owner_id,name,position,created_at | (owner_id,position,id), UNIQUE(owner_id,normalized_name) — normalizer один на сервере |
| chat_user_settings | PK user_id; selected tab/conversation, window geometry, compact, hidden_tabs, sound preferences, updated_at | PK |
| chat_reports | id UUID PK, reporter_id, message_id UUID без FK, channel_id, kind, reason, state open/in_review/resolved/rejected, reviewer_id, created_at,resolved_at; текст исходника не копируется | (state,created_at), (reporter_id,message_id,state) |
| chat_audit | id BIGSERIAL PK,actor_id,subject_id,company_id,action,reason,reference_id,metadata JSONB,created_at; no message body | (created_at), (company_id,created_at DESC) |

Политика FK: owner_id/user_id по возможности REFERENCES users ON DELETE RESTRICT / SET NULL согласно владельцу жизненного цикла. Нельзя случайным CASCADE удалить сохранённые копии/запись прав и оставить неочищенные partition rows. Удаление аккаунта, если появится, — отдельный согласованный barrier (как company deletion).

### 1.4. Модерация и эксплуатация

| Объект | Поля / механизм | Индекс |
|---|---|---|
| chat_corporation_mutes | id UUID PK,company_id,user_id,actor_id,duration_sec,reason,starts_at,expires_at,lifted_at,action_code; append-only история, TTL <=180d после окончания | (company_id,user_id,starts_at DESC), (expires_at) |
| chat_corporation_mute_state **NEW** | PK(company_id,user_id), current_mute_id UUID NULL, expires_at, changed_at; одна current row, состояние актуально только if now<expires_at | PK |
| chat_operations | PK(user_id,request_id), action, payload_digest BYTEA length 32, state TEXT CHECK(state IN ('in_progress','completed')), result_code TEXT, result_json JSONB **без body**, created_at,expires_at. Один и тот же request_id нельзя переиспользовать для другого action. Retention >=187d | (expires_at) |
| chat_outbox | id BIGSERIAL PK,event_kind,channel_id,target_user_id,reference_id,created_at,processed_at; metadata only | (id) WHERE processed_at IS NULL |
| chat_ws_leases | PK user_id, connection_id UUID, server_instance UUID, **generation BIGINT**, expires_at,renewed_at | (expires_at) |
| chat_company_deletion_requests | PK company_id,operation_id UUID UNIQUE,initiated_at,state; Cosmic writer, receipt после удаления master row | (state,initiated_at) |
| chat_company_cleanup_receipts | PK company_id,operation_id UUID UNIQUE,cleaned_at,channel_deleted; Chat writer | (cleaned_at) |
| chat_surface_cleanup_requests | PK surface_id,operation_id UUID UNIQUE,initiated_at,state; Cosmic writer, совместить с P2-3 retirement | (state,initiated_at) |
| chat_surface_cleanup_receipts | PK surface_id,operation_id UUID UNIQUE,cleaned_at; Chat writer | (cleaned_at) |
| chat_game_moderator_grants | PK user_id,granted_by,granted_at,revoked_at; Cosmic writer | (revoked_at) |
| chat_cleanup_jobs **NEW** | id UUID PK, kind group/company/surface/retention,scope_id,operation_id,phase queued/running/completed/failed,created_at,updated_at,last_cursor,error_code; **ни одного тела сообщения** | UNIQUE(kind,scope_id,operation_id), (phase,updated_at) |
| chat_worker_lease **NEW** | PK worker_kind TEXT,owner_id UUID,generation BIGINT,expires_at | PK |

chat_worker_lease требуется только если несколько Chat процессов или процессов maintenance; при одном процессе достаточно advisory locks. Не создавать два конкурирующих механизма лидерства без причины: **рекомендация: только pg_try_advisory_lock для maintenance**, а chat_worker_lease не создавать в минимальной миграции, пока не появится реальный потребитель. Аналогично chat_cleanup_jobs обязателен для многотранзакционного удаления больших бесед/компаний, но не для одиночного DELETE сообщения.

## 2. Партиционирование и DDL-гарантии

- Разделить chat_messages на 5 LIST partitions (global/planet/sector/corporation/private). Каждая — RANGE(sent_at) subpartition. Дневные: global/planet/sector. Недельные: corporation/private. Называть секции chat_messages_{kind}_{yyyymmdd} / chat_messages_{kind}_{yyyymmdd}_week (без коллизий).
- Глобальный PK на parent должен включать ВСЕ partition keys: (kind,sent_at,id). Попытка UNIQUE(id) на partitioned parent незаконна. Уникальность id обеспечивает chat_message_locator PK(id).
- INSERT в правильную секцию и INSERT locator проводятся в одной tx. Все per-partition index definitions создаются в проверяемой миграции; GIN trigram индекс по **lower(body)** должен соответствовать query lower(body) LIKE normalized-pattern.
- CHECK, ensuring source scope: Global all 4 scope NULL; Planet surface NOT NULL, all others NULL; Sector surface+sector NOT NULL only; Corp company NOT NULL only; Private conversation NOT NULL only. UNIQUE partial с конкретными scope, поскольку обычный UNIQUE с nullable колонками не обеспечит нужное.
- Не рассчитывать на FK из chat_message_locator → chat_messages(id), поскольку partition key по kind/sent_at. Обязательный тест reconciliation и немедленный deny при отсутствующем message row.
- MESSAGE id globally UUID, locator UNIQUE(channel_id,seq). Рекомендуется UUIDv4 (crypto/rand) без дополнительных extensions. Глобальный канальный seq выбирается UPDATE chat_channels SET next_seq=next_seq+1 RETURNING next_seq.
- В SQL sent_at брать через clock_timestamp() **после получения locks**, expires_at как sent_at + duration exact seconds по kind. Никогда не использовать timestamp от браузера.
- Нет DEFAULT partition, чтобы ошибка missing future partition была видна; daily maintenance создаёт на 14 дней вперёд, weekly — на 8 недель. Отсутствие будущей секции = отказ send+alert, а не INSERT куда попало.
- Retention job удаляет просроченные строки адресно каждые <=10 минут с limit 1000 и повтором пока backlog; только после locator cleanup допускается DETACH/DROP пустой секции. При любой read/history/search проверять expires_at>DB clock.

## 3. Уточнённые транзакционные SQL операции (контракт, не готовые миграции)

### 3.1. Atomic idempotency claim

1. BEGIN.
2. Попытаться INSERT chat_operations(user_id,request_id,action,payload_digest,state=in_progress). Если конфликт — SELECT FOR UPDATE existing; сравнить action и digest; если completed — вернуть immutable result_json; если in_progress — сериализованно дождаться или отдать retryable.
3. Выполнить domain action + все нужные изменения; UPDATE state=completed, result_code и небольшой result_json; COMMIT.
4. ACK только после COMMIT. Так невозможно дважды взять rate slot при concurrent retry.

**Требуется дополнить chat_operations.state enum in_progress/completed** в итоговом DDL; исходный 04 его ещё не содержит. На время TX незакоммиченный in_progress не должен видеться другим писателям. Внешнего долговечного in_progress без завершения не оставлять, если action выполняется в одной TX; multi-stage deletion хранится отдельно в chat_cleanup_jobs. Никаких скрытых payload body внутри result_json.

### 3.2. Rolling rate

Global/Planet/Sector/Corp: lock chat_rate_state(user_id,kind), INSERT пустой строкой при отсутствии, затем сравнить DB timestamp - last_sent_at >=3/2/1/1 секунды. При допуске UPDATE last_sent_at; все в tx сообщения, при rollback «квота» не израсходована.

Private: для одного user сериализовать quota через **chat_rate_state(user_id,'private')** как пустую mutex row, затем COUNT(*) chat_rate_events WHERE user_id=$1 AND sent_at > (clock - interval '10 seconds'); если <10, INSERT event. Зависимость от channel отсутствует. GC events только после >11 s с запасом, при гонке с запросом не удалять событие, которое ещё в окне.

Reports: lock chat_rate_state(user_id,'report'), COUNT chat_rate_events(kind='report') за 600s; при <5 вставить событие в той же TX, что и chat_reports. Иначе rate_limited. После COMMIT слот не откатывается; повтор той же operation UUID не увеличивает quota.

Invites: lock chat_invite_rate_locks(sender,target), COUNT chat_invite_attempts последние 24h; если <20, INSERT попытку одновременно с invitation; 24h cooldown на (recipient,conversation) либо на canonical DM pair. Никакой GET-запрос без lock не резервирует квоту.

### 3.3. Save vs moderator-delete

Обе операции lock chat_message_locator(message_id) FOR UPDATE.
- SAVE: проверить текущие права на source, что partition row есть и не истекла; INSERT chat_saved_items(copy) snapshot body; COMMIT. Дубли разрешены.
- MOD DELETE: проверить reason и moderator rights; DELETE chat_saved_items WHERE source_message_id=...; DELETE partition row по (kind,sent_at,id); DELETE locator + receipts, audit action metadata only, пересчёт unread; COMMIT.
- Если сохранение «после» MOD DELETE, locator отсутствует и Save returns not_found. Ни при каких условиях не оставлять orphan copy после модерационного удаления.

### 3.4. Current mute

Lock chat_corporation_mute_state(company_id,target_id) FOR UPDATE (INSERT empty row ON CONFLICT DO NOTHING). Для mute: проверить current expires_at <= DB now либо явное policy override; INSERT history, UPDATE current_mute_id+expires_at. Для unmute: UPDATE state expires_at=now/current=NULL + завершить history lifted_at и audit. Отдельный expired-worker не нужен, поскольку проверка времени при отправке достаточна; уборка истории по 180d.

### 3.5. Connection fencing

Handshake: active lease with expiry >now → reject newer connection; expired → UPDATE connection_id, generation=generation+1, expires_at=now+45s RETURNING generation. Renewal только WHERE connection_id+generation match; если 0 rows → close stale WS. Send through WS и HTTP mutation проверяет generation в **том же write TX**, чтобы stale instance не смог создать сообщение после lease takeover.

Если HTTP mutations разрешены параллельно с WS, браузер должен иметь **привязанный к lease недоступный другим вкладкам секрет** (не просто user cookie), иначе второй tab с неактивным WS сможет посылать POST и нарушить «одна чат-сессия». Более простой и рекомендуемый v1: **все пользовательские mutations исполнять только через авторизованный активный WS**, HTTP endpoints для чтения/history/bootstrap/operation status; не иметь публичных HTTP POST для мутаций без проверенной привязки к действующей lease. Один application use case всё равно пригоден для внутренних тестов/transport adapters. Прежнюю таблицу POST endpoints в 06 считать описанием команд, а не безусловно разрешённым обходом WS.

## 4. DB функции, представления, триггеры и роли: окончательный выбор

| Объект | Решение | Почему |
|---|---|---|
| chat_message_expiry(kind,sent_at) | узкая SQL FUNCTION либо expression в единственном repo helper | один источник TTL; не ставить IMMUTABLE на timezone-dependent expression; используйте INTERVAL seconds |
| chat_take_send_slot | **не создавать универсальную SECURITY DEFINER процедуру** в v1: SQL tx с FOR UPDATE внутри Go repository проще проверить | непрозрачные side effects не нужны |
| chat_take_invite_slot | так же SQL tx + lock rows | один источник quota |
| chat_validate_session(token_hash) | **рекомендуется SECURITY DEFINER узкая функция, предоставляющая только ID/role/is_bot**, без выдачи Chat полного SELECT по sessions/password_hash; owner миграций, зафиксированный безопасный search_path, no dynamic SQL. Проверять SQL SECURITY DEFINER в regression test | минимизация утечки credential hashes |
| chat_partition_maintain | отдельный **maintenance Go job** с SQL DDL с повышенными ограниченными правами owner chat_messages; не обычная runtime DB role. Для DDL могут потребоваться owner rights, не только GRANT INSERT/DELETE | runtime компрометация не должна давать unrestricted DDL |
| chat_* triggers | **не создавать** для динамической авторизации, network, уведомлений, каскадных company удаления. Только если CHECK/constraints недостаточно для неизменного DB-инварианта, с отдельным SQL test | избегать скрытого дублирования |
| chat_* views | 0 обязательных. Административную диагностическую view по размерам/окнам добавлять только если есть конкретный потребитель и SELECT-only доступ | меньше поверхностей доступа |
| pg_cron | не требуется | timer и locks в Chat maintenance Go |
| LISTEN/NOTIFY | можно для wake-up, outbox/notification DB = источник истины | NOTIFY не гарантирует доставку |

**Разделить DB credentials:** chat_runtime (DML по chat-owned, EXECUTE auth function, узкий SELECT на owned Planet visit/companies/roles), chat_maintenance (DDL только по chat partition objects и их owner rights, без admin secrets), Cosmic migration runner (новые миграции, Космические-owned requests/grants). Для физической эксплуатации необходимо протестировать grants на реальном PostgreSQL.

## 5. Индексы, которые НЕ следует создавать без EXPLAIN

- Не индексировать каждый text/JSONB metadata без запроса.
- Не добавлять UNIQUE с now() в выражении или WHERE.
- Не строить отдельный индекс по каждому company/surface/sector: один scope index и channel_seq index per partition.
- Не делать GIN по всей body без lower(), если поиск использует lower(body).
- Не увеличивать количество партиций на каждый сектор/корпорацию: число секций зависит только от kind+UTC window.

## 6. Матрица удаления и оставшихся данных

| Действие | Original message | Locator/receipts | Favorites independent copy | Report/audit |
|---|---|---|---|---|
| author-delete | физически удалить | удалить | оставить | metadata, если есть |
| moderator-delete за нарушение | физически удалить | удалить | удалить **все связанные copies** | metadata без body |
| сообщение истекло TTL | физически удалить | удалить | оставить | metadata без body |
| private group/dialog deleted | весь scope физически очистить до статуса completed | удалить | оставить | закрыть операции, metadata без body |
| corporation deleted | весь scope физически очистить и receipt до Cosmic final DELETE | удалить | оставить | metadata + receipt |
| surface retiring/deleted | все Planet/Sector scope этой surface очистить перед final removal | удалить | оставить | metadata + receipt |
| user deletes favorite folder | не затрагивать | не затрагивать | folder_id=NULL, записи остаются | обычный operation metadata |

Если реальная политика защиты личных данных требует удалять авторство из сохранённых чужих копий при удалении аккаунта — это отдельный контролируемый процесс, а не автоматический cascade существующих таблиц.

## 7. SQL acceptance до первого Go обработчика

- Все PK/UNIQUE/CHECK/FK работают, составной parent partition PK корректен, index lower(body) используется; forbidden scope отклоняется.
- 30 одновременных встречных DM invite по одной паре создают один объект/одно состояние.
- 21 concurrent invite от одного sender одному target за sliding 24h: допущено <=20; остальные 429/cooldown; 10+ concurrent Private sends: <=10 в 10s.
- Moderator-delete vs simultaneous 10 SAVE: после commit оригинала нет и **ни одной** исходно связанной saved copy.
- Two WS renew/takeover с generation: stale writer INSERT не проходит.
- Удаление partition + locator чистит receipts, но не favorites; столбец body отсутствует в audit/outbox/reports/notifications/operations.
- Role-grants не дают chat_runtime ALTER/DROP чужой таблицы, SELECT чужих password hashes или произвольный SET ROLE.
- При DB restore запускается cleanup worker до выдачи history и подтверждённой готовности.
