# Chat Service — архитектура и совместимость с Astra Journey

**Актуальность:** документ сохраняет описание репозитория и границ служб; исправления обязательны по 08 (аудит), 09 (БД), 10 (транзакции) и 12 (финальный handoff). При расхождении с ними технические предложения ниже уступают.

Версия проекта: 1.0, 2026-10-30. Статус: **технический проект для реализации**, не утверждённый код.
Продуктовые источники: [01](./01-idea-v1.ru.md), [02](./02-approved-decisions.ru.md). При конфликте применяются поздние одобренные решения 02. Технические решения ниже допускают корректировку по результатам тестов без изменения продуктовых требований.

## 1. Проверенное состояние репозитория (ветка main на момент подготовки)

- Cosmic: cmd/cosmosimulator; internal/server/app.go + routes.go; internal/account, internal/corporations; собственный migration runner internal/initialization/database.go под pg_advisory_xact_lock(784216).
- Planet: cmd/cosmosimulator-planet; internal/planetserver; текущая версия app.go 0.17.0 / P2-3. Реализованы инфраструктура и локальные сцены. GET /planet/api/v1/health/live и /ready существуют, но GET /planet/ws/v1 пока отдаёт 503 planetary_gameplay_not_implemented. **Нельзя** выдавать старую таблицу planet_visits за доказательство активного сектора: до этапа P2-4 и последующих она не содержит достоверного runtime-положения.
- PostgreSQL: одна настраиваемая схема DB_SCHEMA, подключение через internal/platform/postgres.Open, которое НЕ выполняет миграции; начальное DDL из internal/server/schema*.sql применяет только Cosmic.
- Аккаунты: users(id BIGINT, username, role=player/admin, is_bot, company_id, company_role), sessions(token_hash SHA256 в hex, user_id, expires_at). SameSite=Lax, HttpOnly cookie «session», серверная валидация в internal/account/auth.go. Боты исключаются из пользовательского чата.
- Корпорации: companies(id,owner_id,tag,...) и company_roles(company_id,permissions JSONB); членство одно — users.company_id. В internal/corporations/commands.go CompanyAccess проверяет owner или право в JSONB. Название chat.moderate ещё **не** поддерживается валидатором role_save. company_dissolve сейчас сразу обновляет users/stations, возвращает кредит и удаляет companies в одной транзакции: надо заменить при включении чата на durable двухэтапный протокол.
- География: planet_surfaces(id,...), planet_sectors(surface,number), planet_visits(user_id,colony,ship,entered_at) — legacy; будущее authoritative presence согласно docs/planetary/P2-0/HANDOFF.ru.md принадлежит Planet и имеет visit_id / connection_epoch / runtime_fence. Chat не пишет ни characters, ни visits, ни географию.
- UI: existing internal/server/web/ui/menu.js (контекстное меню), windows.js (плавающие окна), core/preferences.js, RU/EN-каталоги. Не строить второй framework чата.
- В users.role пока только player/admin. «Назначенный игровой модератор» из 01 потребует узкого расширения модели полномочий Cosmic; **нельзя** считать его ролью, уже имеющейся в коде.

## 2. Границы ответственности

| Владелец | Пишет | Читает / проверяет |
|---|---|---|
| Cosmic | users/sessions/companies/company_roles, game moderator grants, состояния удаления корпорации/поверхности | служебная готовность, chat cleanup receipt |
| Planet | канонический active visit, surface/sector, position revision, fencing | свои данные и полномочия на высадку Cosmic |
| Chat | все chat_* чаты, сообщения, приглашения, избранное, прочтение, баны/жалобы/журналы, WS leases, outbox | **только чтение** нужных полей Cosmic/Planet |
| Cosmic migration runner | DDL chat_*, индексы, функции, grants | версия миграции |

Общая БД не разрешает чужие UPDATE. Chat использует SQL read-only к Cosmic-owned и будущим Planet-owned таблицам (включая runtime visit), не создавая собственную копию присутствия; единственный владелец записи каждого поля. Исключение — таблицы **с явно разделённым владением** по разным объектам: chat_company_deletion_requests пишет Cosmic, chat_company_cleanup_receipts пишет Chat.

Поток браузер → reverse proxy same-origin: /chat/api/v1/* и /chat/ws/v1 → отдельный Go-процесс cosmosimulator-chat.exe (например loopback 127.0.0.1:8082). Ни Cosmic, ни Planet не должны импортировать internal/chatserver composition root. Общими могут быть только узкие инфраструктурные пакеты; build Windows x64 обязателен.

## 3. Слои Chat Service

1. Transport: handshake, Origin, cookie/session, размеры пакетов, JSON schema, envelope v1, backpressure, reconnect, ошибки.
2. Application: один маршрутизатор авторизованных use cases. Получает один объект **AccessContext** (actor ID, game role/grants, актуальная компания/права, service health, актуальный presence при необходимости) и принимает решение по типу канала. Нет 6 независимых почти одинаковых обработчиков send/search/delete.
3. Domain: channel resolver, private conversations/owner/invites, sending, read receipts, favorites, moderation, search.
4. Repository: короткие транзакции, назначенный порядок блокировок, idempotency, счётчики/ограничения, outbox. Никаких SQL-транзакций вокруг HTTP.
5. Workers: TTL, partition rotation/drop, invite expiry, cleanup jobs, audit/report TTL, stale WS leases, outbox GC, reconciliation.

## 4. Политика доступности, авторизации и местоположения

- Cosmic недоступен подтверждённо → весь пользовательский чат блокируется. Planet недоступен подтверждённо → блокируются ТОЛЬКО Planet/Sector. Chat/DB недоступны → весь чат блокируется.
- Предлагаемые параметры: health probe каждые 3 s, timeout 2 s, три последовательных ошибки либо 9 s без подтверждения; возобновление после двух успешных проверок и полной ревалидации. Эти **настраиваемые** значения не претендуют на одобренное пользовательское решение.
- Health/live не доказывает доступность auth/visit. Для Cosmic необходим read/ready со стабильной семантикой и/или отдельный внутренний readiness; для Planet нужен подтверждаемый в общей PostgreSQL active visit с current_surface/current_sector/epoch/last_presence_at, поддерживаемый только Planet Service. Пока этих полей/согласованного протокола нет, Planet/Sector **fail closed**, другие вкладки доступны по правилам.
- Для всех WS/HTTP действий после handshake: действующая sessions запись, пользователь не bot; чувствительные операции повторно сверяют членство/права в той же транзакции или через короткую авторитетную ревалидацию. Удаление участника/смена корпорации инвалидирует доступ; доставка событий фильтруется **на момент доставки**.
- Planet/Sector: **Решение без лишнего HTTP в критическом пути:** Planet пишет в принадлежащую ему строку актуального visit: state, visit_id, connection_epoch, current_surface, current_sector, runtime_fence и last_presence_at. Chat читает только эти Planet-owned поля, а при отправке в короткой SQL-транзакции делает SELECT этой строки FOR SHARE и проверяет state=active, свежесть и scope. Planet обязан менять sector/leave/revoke в UPDATE той же строки: FOR SHARE блокирует конкурентное изменение до COMMIT send (или отправка видит новое состояние). При чтении/поиске проверять свежий visit и eligibility без ненужных FOR SHARE для неизменяющих запросов. Обнаружение недоступности Planet — отдельный health gate, не замена visit. Пока этих полей и дисциплины блокировок нет, **не выпускать spatial chat**.
- Cosmic авторизует game moderators через собственную модель: в v1 admin из users.role; для назначенных модераторов — отдельный Cosmic-owned grant chat.game_moderate без изменения роли всех игроков. Chat только читает grant, выдача/отзыв по admin API Cosmic.
- Вкладка Corporation опирается на текущее users.company_id и company_roles.permissions. История/поиск доступны только текущим членам (плюс служебный доступ). Тег автора вычисляется по его **нынешнему** company_id при чтении, без фиксации прошлого.
- Каждый приватный автор/получатель должен быть проверен по ID; имя — отображаемый атрибут. Изменение имени не превращает упоминание в ссылку на другого игрока.
- Игровые ссылки — **структурированные серверно проверенные** {kind,id} + разрешение видимости при открытии. Клиент не может вставить произвольный href/HTML/JS и обойти права.

## 5. Существенные изменения существующего кода при реализации

1. Новые SQL миграции последовательно после schema170.sql, с version marker chat_schema_state, под уже существующим migration lock; существующие данные и схемы не перезаписывать.
2. internal/corporations/commands.go: расширить allowlist разрешений на chat.moderate и доступ к функции проверки; существующий **company_dissolve** переорганизовать в durable state machine с подтверждением Chat (см. 06).
3. Cosmic: штатные auth/readiness и grants для назначенных игровых модераторов; API для удаления корпорации, не разрушающий собственные инварианты экономики.
4. Planet: при реализации P2-4+ обеспечить authoritative current visit/sector в своей DB-строке, транзакционное изменение current_sector/visit state и маркер свежести. Chat получает узкий read-only доступ к этим полям; специальный HTTP запрос на каждую отправку не нужен.
5. Существующий JS shell: один chat-модуль, общий UI в космосе/на планете, интеграция existing Menu/Windows/Preferences, i18n RU/EN; положение окна clamp на маленьком экране.
6. Не внедрять Chat в игровые ticks, не запускать альтернативный migration runner Chat или Planet, не хранить вторую авторитетную копию users/company/position.

## 6. Противоречия/зависимости — результат аудита

| Ситуация | Решение |
|---|---|
| Planet сейчас не подтверждает user+sector | Пространственные вкладки за feature gate; контракт с P2-4/5/6 обязателен до включения |
| company_dissolve удаляет корпорацию немедленно | Сначала durable «deleting», чат блокирует доступ и очищает, затем Cosmic завершает, без долгой сетевой SQL-транзакции |
| users.role не знает «game moderator» | Новый Cosmic-owned grant; admin уже есть |
| Corporate permission отсутствует в role_save allowlist | Добавить chat.moderate, сохранить ограничение 7 пользовательских ролей |
| Существующее in-browser уведомление ≠ durable Private unread | Новые chat_read_* и event state в PostgreSQL; не использовать notifications как единственный журнал сообщений |
| География принадлежит Cosmic, текущее присутствие — будущему Planet | Сектор/название берём из planet_sectors; текущий visit — от Planet |
| Требуется физическое удаление и бессрочное Избранное | Нельзя FK ON DELETE CASCADE из избранного в сообщение; копия независима; по модерационному удалению удаляется явным действием в транзакции |
| Переход через границу одновременно с отправкой | Presence epoch + свежая авторизация + отказ при изменившемся sector; нельзя доверять устаревшей вкладке |
| Продукт хочет один WS Chat, а Planet допускает reconnect с заменой WS | Отдельные lease-политики разных служб; Chat отвергает второе активное подключение, Planet продолжает собственную логику |

## 7. Неизменяемые инварианты

- Нельзя писать/читать/искать вне серверно проверенных прав. Провалы проверок никогда не «разрешают по кэшу».
- Успех отправки возвращается только после COMMIT message + idempotency + event/outbox; повтор запроса не создаёт двойной текст.
- Прошедшее удаление стирает текст из активной PostgreSQL, включая полнотекстовые индексы. Логи не сохраняют удалённые тексты.
- Избранное — самостоятельный текст, не ссылка на оригинал, исключение для mod-delete.
- На одного user только один действующий Chat WS lease.
- Все новые обычные таблицы, партиции и их индексы называются с префиксом chat_.
