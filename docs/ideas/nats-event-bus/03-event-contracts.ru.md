# NATS Event Bus — 03. Единые контракты сигналов и будущих событий

Версия проекта: 2026-11-09. **НЕ действующие API.** Все перечисленные subjects — предложения для пилотов/последующего внедрения. Архитектура B утверждена, форматы подлежат контрактным тестам.

## 1. Два независимых пространства

- **Core NATS signals:** `cosmos.v1.signal.*`. Недолговечные best-effort указатели «перечитать БД», at-most-once; при потере нет невосполнимых действий. Payload не содержит авторитетных полномочий или текста чата. Отсутствие подписчика — нормальное состояние.
- **JetStream domain events:** `cosmos.v1.event.*`. События совершившихся устойчивых изменений с отдельным transactional outbox и сервисными durable consumers; at-least-once, могут быть повторены, порядок проверяется по версии агрегата. **В пилотах не включаются.**
- **HTTP/WS:** `/planet/*`, `/chat/*` и внутренние health-check URL остаются за Cosmic reverse proxy/службами. Не смешивать RPC с сигналом изменений.

Нельзя объявлять Core NATS доставкой «гарантированно ровно один раз». Нельзя публиковать одно и то же обязательное экономическое действие и в Core, и в JetStream как два независимо применяемых источника.

## 2. Единый envelope для Core signal

Пример — не фактически опубликованное сообщение:

```json
{
  "schema_version": 1,
  "signal_id": "UUID",
  "source": "planet",
  "kind": "planet.sector.changed",
  "occurred_at": "2026-11-09T12:00:00Z",
  "data": {
    "user_id": "152",
    "visit_id": "UUID",
    "connection_epoch": "19",
    "surface_id": "42",
    "from_sector": 6,
    "to_sector": 7
  }
}
```

Обязательные общие поля: `schema_version=1`, `source` из {cosmic,planet,chat}, `kind` по каталогу и `occurred_at` в UTC, `data` объект. `signal_id` UUID обязателен для перехода сектора и полезен для диагностики остальных сигналов; **не** является разрешением и не гарантирует устранение дубликатов. Конкретный subject — обязательное поле входного контекста; `kind` должен строго соответствовать subject. BIGINT и epoch в JSON — десятичные строки. Ограничение размера Core payload — проектно 2 KiB; неизвестная версия/тип/слишком большой payload → discard + счетчик + фонова́я сверка, но не game mutation.

Producer получает значения из подтверждённой операции; Consumer не доверяет содержимому без последующей авторитетной сверки. Никаких cookie, session hash, имен пользователей, тел/частей сообщений, причин наказания, инвайтов, названий Private, произвольных URL или DSN.

**Время публикации:** только после успешного SQL COMMIT либо как посткоммитный signal из подтверждённой транзакции. Нельзя из BEFORE/AFTER trigger NATS (у PostgreSQL нет NATS Publish API); `pg_notify` внутри SQL транзакции доставляется после COMMIT и относится только к A2. После ошибки NATS публикация не влияет на результат уже совершившегося игрового действия.

## 3. Каталог Core signals

Все subjects имеют prefix `cosmos.v1.signal.` и `schema_version=1`; transport — Core NATS, без retries из брокера. Повтор/непорядок любого сигнала → invalidate + перечитать актуальную БД, **не** самостоятельно применять payload как новую state.

| Subject (полный) | Producer → Consumers | Специфические data | Момент публикации | Что делает получатель / recovery |
|---|---|---|---|---|
| cosmos.v1.signal.chat.outbox.changed | Chat → Chat dispatcher(s) | `hint="outbox"` (без ID Private) | после COMMIT tx с outbox | общий scan chat_outbox; резервный polling и startup sync |
| cosmos.v1.signal.planet.visit.started | Planet → Chat | user_id,visit_id,generation,connection_epoch,runtime_fence,surface_id,sector_number | после Activate COMMIT | refresh spatial scope из DB; при пропуске periodic scope poll |
| cosmos.v1.signal.planet.visit.resumed | Planet → Chat | те же IDs; новый epoch | после Activate(resume) COMMIT | refresh, прежний epoch игнорируется |
| cosmos.v1.signal.planet.sector.changed | Planet → Chat | user_id,visit_id,transition_operation_id,connection_epoch,runtime_fence,surface_id,from_sector,to_sector,geography_revision | после Transition COMMIT вместе с receipt | discard устаревшего hint; DB-функция определяет true scope; reconnect full recheck |
| cosmos.v1.signal.planet.visit.disconnected | Planet → Chat | user_id,visit_id,connection_epoch,runtime_fence, last known surface_id | после Disconnect COMMIT | закрыть spatial permission после проверки; восстановление из planet_visits |
| cosmos.v1.signal.planet.visit.ended | Planet → Chat | user_id,visit_id,connection_epoch, last known surface_id | после COMMIT end/sweep | invalidation; не трактовать как завершённый refund/возврат корабля |
| cosmos.v1.signal.planet.presence.stale | Planet monitor (опционально) → Chat | user_id,visit_id,connection_epoch | после определения expired freshness (это не DB COMMIT) | только wakeup; expiry checked_at всё равно проверяется по БД |
| cosmos.v1.signal.cosmic.company.membership.changed | Cosmic → Chat | user_id,old_company_id?,new_company_id?,old/new_fence_revision? | после COMMIT mutator users.company_id | stamp/ACL refresh; DB fence authoritative |
| cosmos.v1.signal.cosmic.company.role.changed | Cosmic → Chat | user_id,company_id,company_fence_revision | после role assign COMMIT | refresh rights; повтор harmless |
| cosmos.v1.signal.cosmic.company.permissions.changed | Cosmic → Chat | company_id,company_fence_revision,role_id? | после role_save/delete COMMIT | refresh all affected local connections, not per-role update from payload |
| cosmos.v1.signal.cosmic.moderator.changed | Cosmic → Chat | user_id, grant_version? (не существующее в v0.24.4 поле) | после grants COMMIT | privilege refresh via DB (не доверять granted=true) |
| cosmos.v1.signal.cosmic.company.deletion.requested | Cosmic → Chat | company_id,operation_id | после COMMIT chat_company_deletion_requests | worker wakeup; durable request exists in DB |
| cosmos.v1.signal.chat.company.cleanup.completed | Chat → Cosmic | company_id,operation_id | после COMMIT chat_company_cleanup_receipts | Cosmic перечитывает receipt и завершает процесс; loss → polling |
| cosmos.v1.signal.cosmic.transfer.return.completed | Cosmic → Chat или будущий UI adapter (необязательно) | user_id,visit_id?,generation | после фактического Cosmic return COMMIT | update UI, не сигнал от Planet leave |

Строки «grant_version?» и «presence.stale» **не требуют немедленно добавлять таблицы**: в пилотах эти события не публикуются. При отсутствии версии Consumer читает текущее состояние и не сортирует по timestamp.

## 4. Точность событий Planet: epoch и revision

В v0.24.4 `planet_visits.connection_epoch` меняется при transition/activation/resume, но **не гарантированно увеличивается при Disconnect или End**; `planet_visits.revision` относится к версии географии. Следовательно, нельзя делать compare `if event.epoch >= cached.epoch then grant access` для всех событий.

Для P-NATS-02 достаточно существующего operation UUID из `planet_transition_receipts`, visit_id, generation, connection_epoch и from/to. Получатель проверяет, что актуальный visit принадлежит игроку и имеет **сейчас** правильные state/surface/sector/lease. Старое событие никогда не вызывает downgrade.

Отдельный monotonically increasing `presence_revision` на каждый значимый state transition — **возможное будущее улучшение**, а не обязательная миграция пилота. Если появится, DBA проектирует изменение Planet-owned строки и обновления в той же SQL tx, что state mutation. Не переносить это поле в Chat-owned таблицу как второй авторитетный presence.

## 5. Утраченные сигналы, publisher crash и поздние события

- Core signal после COMMIT может не быть опубликован при падении процесса. Это допустимо **только** для replayable invalidation; fallback DB polling, scope check и bootstrapping восстановят картину.
- Для перехода сектора факт подтверждения определяет `planet_transition_receipts` и обновление planet_visits, не факт публикации Core signal.
- `signal_id` transition = `transition_operation_id` либо детерминированно связанный с ним UUID; повтор одной операции имеет тот же identity. Другие сигналы могут генерировать UUID в Go после COMMIT.
- Consumer хранит bounded cache IDs для подавления повторной работы; при его потере correctness не меняется.
- На любой reconnect/subscription generation Chat делает forced reconcile из БД; не принимает «последний Core signal» за snapshot.
- При непонятном типе/версии/epoch — discard signal и запланировать DB refresh; ошибочный сигнал не отправляется игроку и не запускает write.

## 6. JetStream — заранее установленный стандарт будущих доменных событий

Пока **не реализован** и не используется пилотами. Subjects: `cosmos.v1.event.economic.<aggregate>.<past_tense>` (точные имена после проектирования Economic Service), при необходимости другие домены с собственным namespace. Общая схема:

```json
{
  "schema_version": 1,
  "event_id": "UUID",
  "producer": "economic",
  "aggregate_type": "account",
  "aggregate_id": "decimal-or-uuid",
  "aggregate_version": "18",
  "occurred_at": "UTC",
  "payload": {"event_specific": "no secrets"}
}
```

- Producer в одной PostgreSQL транзакции сохраняет событие в отдельный **transactional outbox владельца домена** и commit выполняет игровую операцию. Только publisher после commit публикует в JetStream с `Nats-Msg-Id=event_id` и ждёт PubAck. Не считать ack игровым commit.
- При потерянном PubAck повторная публикация допустима. JetStream deduplication window ограничено; consumer обязан делать INSERT(event_id) в собственный inbox/processed_events в той же tx, где применяет эффекты, и ACK **только после своего COMMIT**.
- Имеющий смысл порядок обеспечивается `aggregate_version` внутри одной сущности; глобальный sequence/JetStream publish order не равен порядку DB COMMIT из разных транзакций/сущностей.
- Для долговечного consumer: durable identity, explicit ack, batch pull, max ack pending, backoff, retry limit и операторский dead-letter/reconciliation процесс; лимиты и TTL утверждаются отдельно вместе с Economic.
- При JetStream outage outbox остаётся pending; dependent workflow ждёт подтверждения обязательного шага (не считать его completed).
- При несовпадении backup PostgreSQL/JetStream проверять event_id+aggregate_version по источнику, не слепо воспроизводить все сообщения.

Официальные первоисточники: https://docs.nats.io/learn/core-nats/publish-subscribe ; https://docs.nats.io/concepts/jetstream ; https://docs.nats.io/learn/jetstream/ .

## 7. HTTP контракты остаются

- Cosmic gateway GET /planet/* и GET /chat/* (включая WS Upgrade).
- Chat GET Cosmic /api/health, GET Planet /planet/api/v1/health/ready.
- Cosmic административная проверка Planet /planet/api/v1/health/ready и Chat /chat/api/v1/health.
- Handoff, return, Planet WS hello/transition, Chat send/read API продолжают использовать существующие маршруты и PostgreSQL.

NATS Request/Reply **не включён**. Если когда-либо появится, его необходимо проектировать как отдельный contract с auth, deadline, idempotency и fallback.