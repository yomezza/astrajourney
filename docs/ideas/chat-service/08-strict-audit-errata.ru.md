# Chat Service — независимый аудит ТЗ, исправления и приоритеты

Редакция: 2026-10-30, технический аудит v1.1.
Прочитаны 01–07 и исходные файлы Astra Journey 0.17.0. Этот документ — **техническое уточнение** 03–07. Утверждённые продуктовые требования 01 и 02 НЕ переопределяет.
Уровень приоритета: 01+02 (согласованные правила) > 08 (технические исправления) > 03–07 (ранние технические предложения).
Не считать выполненным кодом. Ни миграции, ни тесты на PostgreSQL в ходе проектирования не запускались.

## 1. Результат ревизии: ошибки и исправления

| № | Критичность | Обнаруженная проблема | Исправление для Codex |
|---|---|---|---|
| A01 | P0 | Указатель chat_message_locator уникален, но сам текст хранится отдельно в партиционированной chat_messages. Нет сквозного FK, существует риск висячего locator после сбоя очистки | Все изменения locator и message только в одной SQL TX; транзакционные helpers insert/delete; ежедневная reconciliation locator ↔ partition rows; любые orphan locators устранять без доступного текста. Публиковать событие исключительно после commit |
| A02 | P0 | Исходная схема chat_operations с одним result_ref не позволяет воспроизвести идентичный ACK после удаления оригинала и locator | Хранить небольшой immutable result_json без body (message_id/channel_id/seq/sent_at/expiry/status). Упорядоченная дедупликация по (user_id,request_id) в SQL. Не создавать повторное сообщение даже если original уже очищен |
| A03 | P0 | Срок хранения idempotency 48 часов не обеспечивает долговременную гарантию: поздний retry UUID может создать второй текст | Держать tombstone/operation на период самой долгой истории (не менее 180 суток + 7 суток запас), а новое клиентское действие обязано использовать новый UUID; явно не допускать повторный request_id для иной команды. Рассмотреть агрессивный GC только после измерения объёмов; статус удалённого оригинала может иметь outcome=expired/deleted |
| A04 | P0 | Уникальная chat_dm_pairs строка не даёт блокировку, если строки ещё нет, и две встречные заявки могут одновременно решить, что пары не существует | Создать chat_dm_pair_locks(low_user_id,high_user_id) с уникальным PK; INSERT ON CONFLICT DO NOTHING, затем SELECT FOR UPDATE. Только внутри блокировки читать/создавать chat_dm_pairs. Не блокировать таблицу users для синхронизации chat |
| A05 | P0 | Отправка Planet/Sector по ранее полученному location и HTTP check допускает гонку с переходом игрока | В короткой tx SELECT актуального Planet-owned visit FOR SHARE, сверить active/freshness/current_surface/current_sector; все Planet переходы, выход, отзыв должны UPDATE ту же запись. Это уже отражено в 03/05/06; до появления этого контракта spatial выключен |
| A06 | P0 | В предыдущем проекте нет durable записей уведомлений об упоминаниях при отсутствии WS; один ephemeral outbox не заменяет личный inbox | Создать chat_notifications с user_id/event/type/reference/read_at, без текста переписки. Для mention/@all/invite события создаются одновременно с mutation; выдача всегда повторно ACL-проверяется. Outbox только сигнализирует о новых уведомлениях |
| A07 | P0 | Заявлено «одно активное Chat WS», но lease TTL сам по себе не останавливает писателя с потерянной lease при сетевом разделении | У lease должна быть generation BIGINT. Каждую команду проверять под tx на текущее (user_id,connection_id,generation,expires_at). Устаревший контроллер получает stale_connection и не пишет; второй WS не может вытеснить живую lease, только истёкшую |
| A08 | P1 | Физическое удаление беседы может затронуть огромное число строк; попытка держать одну транзакцию на миллионы сообщений блокирует работу, но необъявленный async cleanup нарушает обещание «удалена» | Переводить беседу в deleting **сразу** и запрещать чтение/запись; очистку делать управляемыми транзакционными батчами, durable progress. Конечный статус deleted/успех полного удаления выставлять только после физической очистки. Клиенту допускается отдельное состояние «Удаляется»; не сообщать «Полностью удалено» до завершения. Для малых бесед один tx быстрее. Вопрос синхронной физической очистки любого размера является продуктовым ограничением: НЕ изменять без согласования |
| A09 | P1 | chat_corporation_mutes с проверкой текущего времени не может иметь корректный частичный UNIQUE по now(); одного индекса на историю недостаточно | Разделить current-row chat_corporation_mute_state(PK company_id,user_id) и append-only chat_corporation_mutes(history). Строка current меняется под lock, expire определяется по expires_at; история сохраняет все причины и сроки 180d |
| A10 | P1 | Списание unread при истечении/удалении может обрабатывать десятки тысяч участников/сообщений; точные receipts только у прочитавших | Отдельный maintenance action по диапазонам message seq, сверка unread с динамическим join_floor_seq. Блокировки с conversation lock. Пересчёт (reconcile) обязательно воспроизводим после crash |
| A11 | P1 | Корпоративные права проверялись при открытии вкладки, а поздние WS events и поиск могут открыть доступ после исключения | Проверка ACL для каждой страницы/поиска и перед fanout; все corporate mutations/role changes порождают revocation. Служебный просмотр не смешивать с правом писать/удалять |
| A12 | P1 | Удаление original модератором требует удаления Избранного, однако saved copy может параллельно создаваться | INSERT saved-item требует SELECT locator FOR UPDATE; moderator-delete тоже держит этот lock до удаления copies и original. Результат либо сохранено-до-и-очищено, либо отказ save. Любое post-commit alert не содержит body |
| A13 | P1 | Непонятна область полномочий game moderators в Private: продукт разрешает удалять нарушения, но не разрешает читать все личные беседы | Не давать постоянный полный доступ ко всем Private. Разрешить только адресное рассмотрение оригинала по **активной авторизованной жалобе**, с административным audit; просмотр закрыт за пределами основания жалобы. Если продукту потребуется произвольное чтение Private, это отдельное решение владельца |
| A14 | P1 | Сохранение срока 180 суток и physical purge несовместимы с неограниченным хранением полного message body в reports/audit/operation/outbox | Ни один из этих объектов не хранит текст. Отчёт после удаления показывает «Сообщение недоступно». Нельзя скрытно копировать body для расследования после TTL |
| A15 | P1 | Нынешний company_dissolve удаляет компанию до подтверждения Chat, а планета может удаляться без зачистки пространственных каналов | Durable deletion/retirement barriers, request/receipt с operation UUID, подтверждение physical cleanup до финального Cosmic DELETE; Crash-safe replay, без двойного refund |
| A16 | P1 | 03–07 не определили, какие функции SQL гарантируют атомарность, а какие остаются Go: риск дублирования и обхода транзакции | 09 описывает точную матрицу функций/объектов; одна точка применения лимитов, один AccessResolver, один SendUseCase независимо от HTTP/WS |
| A17 | P1 | Клиентский @all/упоминания могут привести к множественным повторам и пересылке персональных уведомлений без ACL | Упоминания нормализовать к стабильным user_id, сохранить recipients в chat_notifications без body, dedupe по message_id,target_id,type. @all в Corporation и любых Private (включая DM), не больше 1 звукового массового уведомления за 60s на sender/channel |
| A18 | P2 | Границы календарного дня, timestamp и TZ меняют скользящие лимиты/TTL | Считать интервалы как строгое время от clock_timestamp() в UTC и секунды: 7d=604800s, 30d=2592000s, 90d=7776000s, 180d=15552000s. Квоты rolling 24h не «с начала местных суток» |
| A19 | P2 | Поиск по lower(body) требует согласованного expression index; расплывчатое pg_trgm может не использоваться | WHERE lower(body) LIKE '%' || lower($q) || '%' + соответствующий GIN lower(body) gin_trgm_ops на каждой партиции; отдельно тесты кириллицы, латиницы и Unicode эмодзи; лицензированное pg_trgm расширение/доступность валидировать до запуска |
| A20 | P2 | Коллекции public/private cache, фавориты и user settings могут сохранять данные чужой учётной записи после logout на том же браузере | Очистить runtime store и персональные persistent keys на logout/смене учётной записи; backend всегда фильтрует owner_id и права |
| A21 | P2 | Пакет 16 KiB может оказаться недостаточен для допустимых сочетаний escaped Unicode и links/mentions | Разделить MAX_BODY_RUNES (256/500/2500), MAX_FRAME_BYTES (например 32 KiB), MAX_JSON_DEPTH/arrays; проверять значения независимо. 16 KiB остаётся первоначальным baseline до тестов |
| A22 | P1 | SQL DELETE не гарантирует, что остаточные байты сразу исчезли из MVCC heap pages, WAL, backup и файловой системы | Обязателен настоящий DELETE (не soft flag), шифрование PostgreSQL storage и backups, регулярный VACUUM/ретенция; не обещать немедленное криптографическое стирание с диска. После restore применять доступный WAL/deletion manifest |

## 2. Фундаментальные правила исправленной архитектуры

1. **Ни одного полного скопированного текста** в служебных журналах, нотификациях, жалобах, outbox и idempotency. Только messages.body и user-intended favorites.body.
2. Один resolver определяет текущий канал по authority: Global singleton, Planet/sector по visit, Corporation по users.company_id, Private по активному membership, Favorites по owner_id.
3. Состояния и отказы: чат в целом требует Cosmic и Chat, Planet/Sector дополнительно Planet. Непригодность Planet НЕ отключает остальное.
4. Все необратимые действия фиксируются транзакциями с request_id; многотранзакционная очистка использует durable deletion phase, а не «вроде удалилось».
5. Продуктовые ограничения на физическую очистку, старую историю после преобразования/повторного входа, число участников и права собственности не смягчать.
6. Все новые СУБД объекты с префиксом chat_; новые таблицы lock/state/notification определены в 09 как исправления к списку 04.
7. PostgreSQL отвечает за инварианты, сериализацию и durable queue, Go — за интерпретацию правил и интеграцию с Cosmic/Planet; БД никогда не вызывает HTTP и не запускает бизнес-таймеры.

## 3. Исправленный порядок блокировок

**У каждой операции список только фактически нужных блокировок.** Не блокировать «всё на всякий случай».

- DM create/invite: chat_dm_pair_locks sorted pair → conversation (if exists) → chat_invite_rate_locks(sender,target) → invites and pair row.
- Group invite/accept/leave/transfer/delete: conversation → ordered members/invites → invite rate (если новое invite); при SEND после conversation → channel → rate row → locator/message/outbox.
- Global send: channel → rate row → locator/message/outbox; не надо блокировать conversation.
- Corporation send: Cosmic-owned company row FOR SHARE (проверка не-deleting) + membership/role snapshot → channel → rate → message. У Cosmic deletion первым берётся company row FOR UPDATE; после изменения deleting новые send отказывают.
- Spatial send: Planet-owned visit row FOR SHARE → scope/channel → rate → message. Planet transition меняет ту же visit row и не блокирует chat_*.
- Save-copy/moderator-delete: locator row FOR UPDATE → saved_item rows → message row → audit/outbox. Чтобы не создавать cross-order deadlock, transaction save не должен брать conversation/channel FOR UPDATE после locator; проверку права провести до locator и повторно по узкому актуальному состоянию без нарушения lock order.
- Удаление parent/channel: состояние deleting закрывает новые operations до chunk cleanup. Конкурентный send не должен ждать multi-minute cleanup; проверяет deleting и отказывает.

У каждой операции есть тайм-аут ожидания блокировки и ограниченные повторы SQLSTATE 40001/40P01, с небольшим jitter, и без повтора уже успешно committed idempotent operation.

## 4. Необходимые изменения ранее подготовленных документов

- 04: дополнить chat_dm_pair_locks, chat_notifications, chat_corporation_mute_state, generation в chat_ws_leases, immutable result_json в chat_operations. См. 09; не реализовать одновременно старое и новое.
- 05: добавить lifecycle notifications и per-connection WS fencing, прояснить pending deletion и обработку больших бесед.
- 06: расширить контракт event recovery и операции status для idempotency после удаления originals.
- 07: на M0 включить P0-проблемы A01–A07; на M2/M4 тестировать A08–A17, прежде чем включать функциональность.

## 5. Решения, требующие специальных допусков или проверки, но не новых вопросов о геймплее

- Проверить реальную PostgreSQL версию, pg_trgm и права установки расширения. Нельзя подтвердить их без доступного действующего DB instance.
- Проверить, что Cosmos/Planet и Chat не делают UPDATE одного owned-row разными владельцами. Cosmic должен явно поддерживать добавленные поля deletion/role grants.
- В случае роста корпуса сообщений пересмотреть retention-job batch size, расписание секций и стратегию не-горячих индексов **по EXPLAIN ANALYZE**, а не заранее добавлять десятки индексов.
- При административном удалении пользователя или корпорации нужен безопасный cleanup barrier; если удаление аккаунта появится позже, нельзя просто CASCADE delete chat_saved_items/messages без оценки прав и требований к удалению личных данных.
- Все технические значения времени/батчей настраиваемы; проверки на реальные нагрузки и рестарт обязательны.

## 6. Условия завершённости аудита

Аудит **документарный**, не proof of deployed behavior. P0 исправлены на уровне архитектурных решений и контрактов документов 09–11; разработчик обязан реализовать миграции, тесты и подтвердить ограничения на PostgreSQL и Go. Не объявлять проект безопасным лишь потому, что написаны документы.
