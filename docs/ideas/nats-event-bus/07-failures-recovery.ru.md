# NATS Event Bus — 07. Отказы, деградация, восстановление

Дата: 2026-11-09. Статус: проект. Предпосылка — **архитектура B**, Core NATS как необязательное средство пробуждения. PostgreSQL/HTTP/WS и действующие роли/fencing/журнал удалений — авторитетны; временная недоступность брокера не должна ломать обычные игровые операции.

## 1. Что является «необязательным сигналом»

Допустимо работать без брокера, если операция:
1. уже полностью зафиксирована в PostgreSQL либо local in-memory runtime по существующему игровому контракту;
2. не требует независимого обязательного подтверждения downstream для завершения;
3. имеет способ сверить actual state (DB query/scope stamp/cleanup request/receipt);
4. при потере сигнала не выдаёт лишних разрешений и не теряет irreversible effect.

Обычный Chat message, Planet sector transition, invalidation корпоративного членства, wakeup company cleanup — удовлетворяют этим условиям при сохранённых проверках и fallback.

**НЕ допускается** считать успешно завершённой будущую межсервисную экономическую операцию, когда она требует подтверждения другого владельца состояния: если нет durable receipt/consumer COMMIT, оставаться pending и повторять обработку после восстановления, а не верить Core сигналу.

## 2. Матрица с явным поведением

| Отказ | Основное поведение | Механизм восстановления / запрет |
|---|---|---|
| NATS не запущен до старта Cosmic, Planet, Chat | Службы стартуют при валидных settings, NATS статус degraded; обычный Chat/Planet движение работает | Core publisher skip/buffer bounded? **не** бесконечная RAM очередь; Chat shared safety poll 2s, таймеры scope и HTTP health прежние |
| NATS выключился на 5 минут | Уже подтверждённые сообщения/переходы сохраняются; NATS hints пропадают | Chat fallback DB scan; после reconnect immediate reconciliation; Planet не ждёт NATS |
| Отказ NATS между Chat COMMIT и Publish | Отправка подтверждена игроку; Core событие может отсутствовать | chat_outbox уцелел; safety polling+bootstrap; не повторять message send |
| NATS отказал при отправке, Publish вернул успех в client buffer | Не считать, что подписчик получил signal: Core at-most-once | Flush/metrics полезны для диагностики, но не guarantee delivery; fallback всегда включён |
| Core signal пришёл старым после reconnect | Нет перераспределения доступа по event data | DB revalidation, epoch/current company fence, stale ignored |
| Дубликат Core события | Повторная инвалидация coalesced, не новая игровая операция | bounded signal ID dedupe; correctness не зависит от cache |
| Новый subscriber пропустил событие до установки Subscribe | Нельзя полагаться на replay | Subscribe/Flush перед initial DB scan; forced reconciliation |
| A2 LISTEN подключился заново | Первые NOTIFY можно потерять при переподписке | LISTEN+COMMIT, затем snapshot SELECT и normal notifications, fallback |
| NATS Subscribe permission violation | Signal source degraded, не выход из игры | Лог subject/error без секретов, 2s poll, alert, исправить ACL и resubscribe |
| NATS медленный consumer / server slow consumer | Только NATS сигнал может потеряться; не game data | bounded local queues, reconnect + forced DB scan; monitor slow_consumers |
| WS клиент медленно читает | WS sync coalesced, клиент отключается по write timeout | Reconnect bootstrap и HTTP history с ACL, без блока всей fanout |
| Chat неожиданно завершился во время fanout | Текст/командные результаты останутся в БД, Core sync мог не дойти | На запуске journal replay/DB readiness, каждый WS reconnect/bootstraps |
| Chat исчез после moderator delete journal fsync, до SQL COMMIT | Journal хранит UUID удаляемого сообщения | На старте replay из внешнего журнала, независимого от NATS |
| PostgreSQL недоступна | Chat не может подтвердить session, ACL, operations — fail closed; Cosmic/Planet применяют свои существующие DB/lease правила | Не выдавать права по NATS или cache; после восстановления выполнить recovery/maintenance/initial scan |
| Planet и NATS недоступны одновременно | Planet/Sector чаты недоступны; другие Chat вкладки работают при healthy Cosmic/DB/Chat | Проверить readiness Planet и current visit в БД перед восстановлением Spatial |
| Cosmic недоступен | Весь Chat недоступен независимо от NATS | Вернуть по прежнему Cosmic readiness, auth и актуальным DB состояниям |
| Уничтожен/ошибочен NATS auth password/TLS cert | Client degraded; reconnect может fail | Readiness обычного чата не зависит от NATS, alert и исправление secret/cert |
| Отстаёт долговечный JetStream consumer (будущее) | Увеличивается delivery lag, downstream зависимые шаги остаются pending | Durable consumer replay/backoff, idempotent DB inbox, alarm |
| JetStream недоступен при наличии Core NATS | Core signals могут идти, durable events не подтверждаются | Publisher сохраняет transactional outbox, повторы после PubAck; dependent step pending |
| JetStream event доставлен повторно | Consumer не должен применить второй раз | UNIQUE(event_id) в DB inbox в той же SQL tx эффекта, ACK после COMMIT |
| JetStream события пришли не по порядку | Не применять устаревшее состояние | aggregate_version/epoch, gap handling и reconciliation |
| Восстановлены разные версии PostgreSQL и JetStream | Event может быть «из будущего», отсутствовать в DB или уже быть применённым | Сверка event_id/aggregate_version/DB install epoch; fail-closed для финансово значимых эффектов |

## 3. Отделить сигнальную доступность и игровую готовность

- NATS transport health не заменяет `GET /api/health` Cosmic, `GET /planet/api/v1/health/ready` Planet или DB Ping Chat. Возможность подписаться на Core NATS не удостоверяет, что Planet может дать active visit.
- Независимо от NATS, Chat отправляет generic WS `health` и проверяет session/lease; revocation scope проверяется не реже текущего интервала в пилоте.
- Время жизни planet_visits.checked_at (2s) и планетарного runtime lease остаётся неизменным. Ни застрявший, ни старый signal не продлевает их.
- Company role/permissions — актуальные DB company fence, не payload. Corporate cleanup завершается только по durable PostgreSQL receipt.
- Любой sync, сформированный до ревокации, не содержит body/Private ID; авторизованный GET после revocation отказывает.

## 4. Контроль полноты при Core, outbox и поздних COMMIT

Проблема: последовательность chat_outbox bigserial отражает **выделение** ID, но не порядок COMMIT. Нельзя отсечь все ID ниже max видимого.

**Проект правила:** safety scan `id>highwater OR created_at>=now()-overlap`; overlap >= максимальное время между INSERT и COMMIT + fallback poll interval + jitter + margin. В пилоте предложено 60s при normal polling 10s и degraded 2s, но число требует проверки сценария с преднамеренно задержанным lower-ID COMMIT.

- Сканировать пакетами и не продвигать watermark при SQL failure/частичной обработке.
- Dedupe recent IDs; при переполнении памяти можно отправить лишний generic sync, но нельзя скрыто потерять сообщения.
- На reconnect сигнального источника forced scan + scope reconcile.
- При долгой остановке дольше retention chat_outbox (~1 сутки в v0.24.4) каждый WS bootstrap читает current state; не обещать replay давно удалённых служебных hints.
- Временное падение PostgreSQL: не отмечать обработанными события, которые не прочитали; resume только после доступности DB.

## 5. JetStream: что может блокировать действия

Внешний Economic Service пока не реализован; ниже будущий стандарт:

1. Любая операция, требующая подтверждения Economic consumer, создаёт pending запись и transactional outbox в PostgreSQL источника.
2. Publisher отправляет в JetStream, ждёт PubAck; при outage не удаляет outbox и повторяет с тем же event_id.
3. Consumer получает at-least-once, проверяет schema/aggregate_version, выполняет идемпотентную DB tx с processed_event_id; ACK после commit. Если сообщение оказалось старым — фиксирует appropriate ignored/handled status без повторного списания.
4. Producer не помечает межсервисную операцию completed, пока отсутствует **её собственная durable downstream receipt**, а не просто PubAck брокера.
5. При восстановлении DB/JetStream из разных snapshot: блокировать денежный settlement, сравнить outbox/inbox/receipts/aggregate versions, провести rollback/reconciliation по документированному алгоритму DBA; без данных для сверки — ручное подтверждение. Нельзя перепроводить оплату по старому JetStream сообщению «для удобства».
6. Первый одиночный NATS с JetStream replicas=1 — точка отказа, поэтому критичное применение требует отдельной эксплуатации/backup/HA-обоснования.

## 6. Стратегия восстановлений и метрики

Событийные проверки после восстановления процесса/брокера:
- Reconnect NATS/TLS auth → Subscribe success + Flush → reset in-memory hint cache → DB reconciliation → normal batching.
- Reconnect PostgreSQL LISTEN → LISTEN commit → read authoritative snapshot → event-driven.
- Restart Chat → erasure journal checked/replayed → schema ready → core source best effort + initial DB scan → accept WS → user bootstrap.
- Restart Planet → runtime lease/fence revalidated, existing Planet resume checks; Chat current presence check, NATS old state discarded.
- Restore DB + JetStream → отдельный «recovery mode» для будущих financial consumers, сравнение installation/event IDs и downstream receipts перед возобновлением.

Метрики/алерты: `nats_connected`, `nats_reconnect_total`, `signal_publish_failed`, `signal_subscribe_failed`, `core_signal_invalid`, `dispatcher_degraded`, `outbox_scan_error`, `outbox_lag_seconds`, `dispatcher_pending_ws`, `late_outbox_commit_seen`, `ws_slow_disconnect`, `js_consumer_pending` (future), `js_redelivered` (future). Не включать user_id, body или credentials в labels.

## 7. Приёмка отказоустойчивости

**Недопустимые результаты**:
- отказ NATS приводит к HTTP 503 на Global/Private при здоровом Cosmic/DB/Chat;
- NATS сигнал даёт доступ к старому Sector после смены epoch;
- после missed Core event сообщение исчезает из доступной истории или перестает восстанавливаться;
- downstream refund повторяется из-за дублированного уведомления;
- после restore модерационно удалённое сообщение появляется из-за отказа от erasure journal;
- игровой HTTP gateway зависит от успешного NATS connection.

**Тесты:** передача 1000 сигналов при broker on/off, выключение на 5 минут, рестарт сервера, потерянный сигнал перед Subscribe, out-of-order Planet transition, double Core publish, медленный WS, SQL disconnect, JetStream PubAck loss (future). Ни одна acceptance запись не считается пройденной до реального теста.

Официальные основы: https://docs.nats.io/learn/core-nats/publish-subscribe ; https://docs.nats.io/concepts/jetstream ; https://www.postgresql.org/docs/current/sql-listen.html .