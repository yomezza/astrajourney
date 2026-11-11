# NATS Event Bus — 01. Фактическая архитектура Astra Journey

Дата исследования: 2026-11-09. База: main 6f75b58e0f02f918b19d726baefe6d3aafa71859, релиз v0.24.4. Статус: **подтверждённый исходниками аудит**, не реализация NATS. Путь репозитория: https://github.com/yomezza/cosmosimulator .

## 1. Проверенные компоненты

| Процесс | Путь | Фактическая ответственность |
|---|---|---|
| Cosmic | cmd/cosmosimulator; internal/server | Gateway HTTP/WS, пользователи/сессии, корпорации, экономика, маршруты и handoff |
| Planet | cmd/cosmosimulator-planet; internal/planetserver, internal/planet/visits | Планетарный runtime, визиты, epoch, движение, переходы и серверная проверка presence |
| Chat | cmd/cosmosimulator-chat; internal/chatserver, internal/chat | Переписка, приватные группы, модерация, избранное, read receipts, WebSocket и фоновые задачи |
| PostgreSQL | DB_SCHEMA + Cosmic migration runner | Авторитетное состояние всех трёх служб, права и ограничения; отдельные владельцы записи |
| NATS | пока внешний сервер администратора | В репозитории v0.24.4 нет внедрённого NATS-клиента; go.mod не содержит nats.go |

Cosmic маршрутизирует GET /planet/* и GET /chat/* через ReverseProxy в Planet/Chat (включая WebSocket Upgrade). Адреса PLANET_INTERNAL_URL и CHAT_INTERNAL_URL нужны этому gateway, а не межсервисной шине событий.

Chat вызывает HTTP GET Cosmic /api/health и Planet /planet/api/v1/health/ready (проверки раз в 2s, Timeout 900ms). Отказ Cosmic запрещает все пользовательские операции Chat; отказ Planet закрывает только Planet/Sector. Chat дополнительно обращается к общей БД за авторизацией и состоянием. Cosmic проверяет Planet health/lease в административной карточке. Это не NATS RPC.

## 2. Текущая доставка Chat

- internal/chat/messages.go: отправка message, INSERT chat_message_locator/chat_messages и вызов event(...) в той же транзакции; event записывает только metadata в chat_outbox. Успех отдаётся после COMMIT через internal/chat/model.go.
- internal/chatserver/server.go: **каждый** зарегистрированный Chat WS раз в 2s вызывает Authenticate, Changes и отправляет health/sync; WS lease generation проверяется при мутации, renewal приблизительно раз в 10s. Одна активная Chat WS-сессия на игрока: chat_ws_leases PK(user_id) с generation и expiry 45s, новая вкладка отвергается пока lease действует.
- internal/chat/changes.go: SELECT max(id) FROM chat_outbox; SELECT chat_scope_stamp(user_id); затем audience-filtered md5(string_agg(id...)) по outbox с условием (id>cursor OR created_at>now-15s). В обработке Channel/Private/Corporation/Spatial используются SQL-права. Возврат next cursor и отпечатка вызывает generic WS sync без чужого текста/ID.
- internal/chat/maintenance.go: очистка сообщений, group/company/surface jobs, receipts и expired data каждые 10s. Удаление корпорации и поверхности основано на PostgreSQL requests + receipts, а не на HTTP-вызове Chat.
- internal/chat/journal.go: независимый fsync журнал UUID модераторских удалений CHAT_ERASURE_JOURNAL, необходимый при восстановлении; не заменять брокером.
- chat_outbox хранит id bigserial, event_kind, channel_id, target_user_id, reference_id, created_at, processed_at. Существующий processed_at **не означает готового JetStream delivery status**; его нельзя использовать для публикационного протокола без отдельного дизайна.

## 3. Планетарное присутствие и корпоративные права

- Planet: planet_visits хранит user_id, visit_id, state, current_surface, current_sector, connection_epoch, runtime_fence, checked_at. Переход сектора — транзакционный UPDATE + planet_transition_receipts; исполняется в internal/planet/visits/transition.go и COMMIT в этой же функции. checked_at обновляется службой Planet.
- Chat: chat_lock_planet_presence(user_id) — ограниченная SECURITY DEFINER функция; проверяет active visit, текущую географию, runtime lease/fence и свежесть checked_at < 2s; применяется при пространственных командах. Это источник разрешений; NATS signal никогда не становится разрешением.
- Корпорации: chat_company_access_fences с revision и триггеры на users/companies/company_roles; Chat проверяет через chat_lock_company_access. Удаление corporation: companies.chat_deleting → chat_company_deletion_requests → Chat очищает данные → chat_company_cleanup_receipts → Cosmic завершает.
- Администратор/корпоративные роли и сессии существуют в Cosmic-owned таблицах; NATS не переносит полномочия.

## 4. Реальная конфигурация v0.24.4

Конфигурация: internal/platform/configuration/{configuration,document}.go и docs/configuration.ru.md.

- CONFIG_FILE не задан → только environment; задан → строго TOML, без merge с ENV.
- TOML проверяется DisallowUnknownFields: любые новые поля [nats] до изменения document.go остановят все службы, которые читают этот файл.
- Cosmic использует cosmos.planet_internal_url и cosmos.chat_internal_url для gateway, они сохраняются.
- Chat использует chat.cosmic_internal_url и chat.planet_internal_url для readiness, их сохраняем в пилотах.
- Браузерные allowed_origins, public_origin, cookie_secure и все DB реквизиты не меняются.
- В go.mod пока нет NATS Go client; появится только при отдельном разрешении реализации.
- Проверки запускаются из Windows .exe, в документации возможны отдельные TOML для трёх Windows Service.

## 5. Частота фоновых действий (арифметика по коду, не benchmark)

| WS Chat | Индивидуальных Changes() / second |
|---:|---:|
| 50 | около 25 |
| 100 | около 50 |
| 200 | около 100 |
| 256 | около 128 |

Частота остальных SQL не уменьшается автоматически: WS Authenticate, чтение истории, права, SQL-команды, cleanup и health имеют собственные обязанности. Качество NATS должно оцениваться по реальным измерениям.

## 6. Известные особенности, которые нельзя забыть в новой архитектуре

1. Один outbox ID может быть выделен раньше, но закоммичен позже большего ID. Вычислять только cursor max(id) и забывать всё ниже нельзя. Существующий overlap=15s рассчитан на ограниченные длительности транзакций; при расширении резервного polling до 10–30s overlap нужно увеличить с учётом максимального времени между INSERT и COMMIT, интервала polling и jitter.
2. В chat_outbox есть events с channel_id=NULL (в том числе private.changed) и target_user_id, поэтому fanout «только подписчикам канала» неполон.
3. Изменения users/company/role/presence могут произойти **без INSERT в chat_outbox**. Отдельный scopeStamp/revalidation обязательны при любой оптимизации.
4. Chat WebSocket отправляет generic sync/health; для доставки содержимого браузер использует авторизованный HTTP. Не вводить текст сообщений или приватные идентификаторы в новые сигналы.
5. PostgreSQL data, leases, idempotency, company/planet fences и внешний журнал erasure продолжают быть авторитетными. Core NATS at-most-once; JetStream at-least-once, но это не транзакционный commit game database.
6. В текущем Chat один процесс на установку, защищённый advisory lock. Пилот не должен незаметно переходить к multi-instance Chat.
7. JetStream store на отдельном одноузловом NATS не даёт высокой доступности; при отказе сервера durable replay недоступен до восстановления.

## 7. Источники кода и операционной документации

- https://github.com/yomezza/cosmosimulator/blob/main/internal/chatserver/server.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/chat/changes.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/chat/model.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/chat/messages.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/chat/maintenance.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/server/planet.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/planet/visits/transition.go
- https://github.com/yomezza/cosmosimulator/blob/main/internal/chatintegration/schema.go
- https://github.com/yomezza/cosmosimulator/blob/main/docs/configuration.ru.md

Документ отражает анализ существующего кода, не результаты запуска PostgreSQL/NATS/браузера.