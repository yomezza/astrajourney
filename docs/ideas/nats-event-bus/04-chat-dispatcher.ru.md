# NATS Event Bus — 04. Chat Event Dispatcher: точная архитектура

Дата: 2026-11-09. **Проект для Go-разработчика, не реализовано.** Пилот сравнивает A1/A2/B1; выбранная реализация не должна изменить клиентский Chat WebSocket протокол, права, WS lease, idempotency, журнал удаления или восстановление.

## 1. Нынешнее поведение, которое нужно сохранить

Сейчас internal/chatserver/server.go каждые 2s **для каждого WS**: Authenticate(session), Renew(lease если пора), Changes(u,cursor). Changes в одной SQL tx вычисляет max outbox id, `chat_scope_stamp(user_id)` и fingerprint доступных за overlap=15s outbox событий. WS отправляет `health` и, при изменении, generic `sync`. Клиент по `sync` делает авторизованные HTTP GET. Это не push текста сообщения в WS.

Гарантии, которые нельзя ослабить:
- Непрерывная проверка сессии и поколения WS lease; одна active Chat WS на user_id.
- Смена users.role/company_id/permissions/moderator grant/Planet state может не создавать chat_outbox; именно scope_stamp фиксирует такую смену.
- Приватные conversation/сохранённые копии/private invites и глобальная/корпоративная/пространственная аудитория отличаются; текущий `channel_id IS NULL` у private.changed не значит «всем».
- Два outbox INSERT получают ID до COMMIT; второй может закоммититься первым. Нельзя пропустить более ранний ID.
- После reconnect F5/WS reconnect клиент должен повторно получить текущие данные без reliance на доставку конкретного `sync`.
- При ошибке Chat/DB запрещено отправлять незаверенные сведения; auth/GET всегда снова проверяет ACL.
- Slow WS client не блокирует dispatcher и остальных игроков.

## 2. Разделение компонентов (имена проектные)

| Go-компонент | Жизненный цикл / ответственность |
|---|---|
| `internal/chatserver/ws_registry.go` — WSRegistry | Map[user_id]ConnHandle; добавляет/удаляет только после подтверждения DB lease, generation guard, bounded sync queue, отдельный mutex |
| `internal/chatserver/dispatcher.go` — EventDispatcher | Один на Chat process; Wake + scan + candidate resolution + coalescing + enqueue sync |
| `internal/chatserver/signals.go` — WakeSource | Источник A1/A2/B1 с единым интерфейсом; ошибки брокера не являются отказом Chat |
| `internal/chat/dispatch_reader.go` — DispatchReader | Пакетно читает outbox, вытаскивает потенциальные target users; нет передачи body |
| `internal/chat/dispatch_acl.go` — RecipientResolver | Для ограниченного набора кандидатов проверяет current ACL, scope, private membership и fresh Planet presence; вызывается одинаково в A1/A2/B1 |
| `internal/chatserver/metrics.go` | latency/counters, bounded queues, per-source health; без user_id/текста в labels |
| `internal/chatserver/server.go` | composition root: start/stop dispatcher, старый режим legacy как fallback |

Это **план файлов**, не требование переписывать internal/chat/model.go или наследовать от server.App. Существующий app/conn writer reuse по максимуму.

## 3. Общий интерфейс источника сигналов

Псевдоконтракт без зависимости от конкретной библиотеки:

```go
type WakeReason struct {
    Cause string   // local_commit, pg_notify, nats_signal, fallback, reconnect
    Kind  string   // optional hint; never authorization
}
type WakeSource interface {
    Start(ctx context.Context, wake func(WakeReason)) error
    AfterCommit(ctx context.Context) // best-effort, no game mutation
    Health() SignalHealth
    Close(ctx context.Context) error
}
```

- A1: `AfterCommit` делает nonblocking wake в общей очереди Go. При переполнении ставит атомарный dirty flag, не теряет необходимость повторной сверки. Изменения из других процессов обнаружит DB fallback.
- A2: специализированное PostgreSQL LISTEN/NOTIFY, отдельное connection в pool, `LISTEN chat_events` (имя канала согласовать с DBA); в БД для записей в chat_outbox предлагается минимальный trigger `AFTER INSERT PER STATEMENT` или эквивалентный `pg_notify` в той же transaction. PostgreSQL доставляет NOTIFY только после commit. Установить LISTEN, выполнить COMMIT, **затем прочитать snapshot**, лишь после этого ждать notifications. `AfterCommit` noop, иначе A2 обойдёт проверяемую технологию.
- B1: NATS subscriber на `cosmos.v1.signal.chat.outbox.changed`, publisher вызывает `AfterCommit` **после** успешного tx.Commit; publish best-effort, не входит в SQL tx и не препятствует ACK чата. Subscribe + Flush/подтверждение установки подписки перед initial DB scan. Не давать локальному `AfterCommit` напрямую пробуждать dispatcher в основном benchmark B1, иначе измерим не NATS, а локальный shortcut. При подтверждённой ошибке publish — activate degraded polling; при обрыве между COMMIT и Publish поможет fallback.
- Источники не определяют audience, не передают сообщения по user_id и не пытаются менять current scope.

`Start` не блокирует запуск при временно недоступной NATS в **валидной B1-конфигурации**: ставит Health=degraded и включает polling 2s; некорректная конфигурация (нет необходимых TLS/auth файлов при enabled) должна отвергаться заранее, а не тихо выключать безопасность. Для A2 ошибка LISTEN приводит к degraded polling и reconnect listener.

## 4. Регистрация одного WS и гонки

1. В существующем `socket` сначала Origin, session, readiness, DB `Claim(user_id, instance)`. Вторая active lease => HTTP 409 до upgrade.
2. После websocket.Accept зарегистрировать ConnHandle с `user_id, lease.connection_id, lease.generation` в WSRegistry. Замена допустима **только** после подтверждённого нового DB lease; если старая handle с тем же user_id, старая обязательно перестаёт получать уведомления.
3. ConnHandle содержит bounded queue `syncPending` capacity=1 (coalescing по пользователю), отдельную командную/ACK очередь без смешивания. Все socket writes выполняет единственный писатель существующей connection loop; dispatcher не вызывает conn.Write из другого goroutine.
4. На регистрацию сразу enqueue **generic sync** или инициировать клиентский bootstrap, даже если outbox пуст. Это компенсирует события между открытием вкладки и подключением.
5. Registry считает connection active только если совпадают `user_id, generation, connection_id` и процесс не закрывается; при попытке доставить старому поколению событие отбрасывается.
6. При disconnect/expiry/stop: удаление только если current handle совпадает; `Release` DB lease с exact generation, не освобождать чужое новое поколение. Clearing same user не должен удалить новую регистрацию.
7. На reconnect аутентификация и получение разрешённого bootstrap — заново, прежний cursor/scope не переносить без проверки. Connection registry не хранит текст сообщений/сессий в сигнале.

## 5. Общая очередь и объединение

- Единый `wake` channel небольшой ёмкости (проектно 1–64) + атомарный `dirty` flag. Любое число одинаковых triggers превращается минимум в одну запланированную scan. Overflow не теряет инвалидацию.
- На пробуждение подождать короткое coalescing окно (проектно 50ms, тесты 0/10/50/100ms), затем запустить только **один** scan за раз; повторные wake во время scan устанавливают dirty и запускают ещё один цикл.
- Не запускать отдельный `Changes()` для каждого сигнала и каждого WS. Scan объединяет event_ids в batch и отдаёт только изменённые `channel_id`, target_user_id, event_kind и references для auth-resolution; приватный ref никогда не попадает в WS payload.
- Для перегруженного периода ограничить batch (например 1000 rows, 50ms–100ms CPU budget, max 256 connections). Если backlog осталось — следующая итерация немедленно, но с уступкой планировщику. При ошибке SQL не двигать cursor, включить retry/backoff.
- Глобальные/корпоративные массовые события: один SQL scan, одна группировка, один `syncPending` на каждого потенциального получателя. Повторные события одного канала в batch не создают N sync сообщений.
- Отдельный periodic `scope reconcile` следит за изменениями, не формирующими outbox; не смешивать его с проверкой события и сессии.

## 6. Scan outbox: последовательности и поздние COMMIT

Базовое состояние диспетчера: `highwater` — наибольший обработанный **видимый committed** id; `recentSeenIDs` — ограниченный набор id в safety-overlap; `lastFullReconcile`.

Алгоритм каждой итерации:

1. Прочитать подтверждённые visible строки chat_outbox по условию `id > highwater OR created_at >= clock_timestamp() - overlap`, сортировать в рамках batch и не считать max(id) достаточным доказательством полного порядка COMMIT.
2. Перекрытие должно быть **не меньше max interval(event INSERT→COMMIT) + fallback poll period + scheduling jitter + safety margin**. Рекомендуемый пилотный параметр **60s**, не нынешние 15s; утвердить после измерения реальной tx wall-time. События с allocated lower ID, которые commit после upper ID, должны попасть через created_at overlap.
3. Обрабатывать только ещё не виденные `id` (recentSeenIDs); повторная инвалидация допустима даже при потере dedupe cache. При пакетном LIMIT корректно проходить весь набор, а не назначать highwater по первой странице и забывать ещё не обработанные строки.
4. `highwater` обновить только после успешной обработки batch в памяти/распределения по pending queues. Если ошибка до готовности получателей, повторить; generic sync идемпотентен.
5. По завершении полного прохода new highwater можно поднять до максимального просмотренного committed id. Старые lower-ID commits обнаруживаются следующей overlap scan. TTL `chat_outbox` сейчас 1 сутки; при закрытии процесса дольше суток нет гарантии replay — fresh bootstrap/reconcile обязателен.
6. Добавить метрику `insert_to_commit_delay` и `late_low_id_recovered`, если возможно измерить; тест искусственно держит нижний ID в tx перед COMMIT и коммитит верхний раньше.
7. На startup нет смысла переносить `highwater` из другого процесса: сначала создать источник уведомлений, затем полностью reconcile зарегистрированных клиентов (если они появляются позже — bootstrap), установить snapshot watermark с overlap и перейти в основной цикл. Если NATS signal упал между шагами — DB scan восполнит.
8. При 1000+ активных rows в минуту проверить EXPLAIN для `chat_outbox_recent(created_at,id)` и PK(id); возможно разделить условие на два index scan + UNION с dedupe. **Никаких SQL индексов из Chat runtime**: это работа DBA при доказанной необходимости.

**Опасная комбинация:** 30s polling + overlap 15s может потерять lower-ID commit, который стал виден сразу после предыдущего опроса, но имел created_at ещё до него. Не использовать эту пару без доказанного верхнего bound и тестов.

## 7. RecipientResolver — не один огромный SQL

Общий диспетчер сохраняет одинаковую для A1/A2/B1 аудиторию:

1. `outbox` batch размечает только **потенциальных** клиентов: global → все локальные active handles; targeted → target_user_id; corporation → текущие member users выбранной компании; private → current active chat_members; `private.changed` и invite → текущие участники и автор/адресат приглашения; planet/sector → текущие active visit в surface/sector.
2. Для каждой категории выполнить **отдельный небольшой пакетный SQL запрос** по набору channel IDs/user IDs, а не N SQL на N событий. Private не раскрывает кандидатов из соседней беседы; если event.channel_id отсутствует, проверять reference type и приглашения.
3. Проверить текущие rights/context для кандидатов; корпорация — revision/fence и membership, Planet — свежая authoritative presence, Private — current member + state, login — active session. Не доверять NATS publisher, локальному cached company/sector или прошлому выбранному tab.
4. Для сложных privileged cases допускаются малые bounded SQL checks по пользователям/группам; **не объединять всё в одну монолитную SQL-функцию** только ради нулевого числа запросов.
5. Enqueue разрешённым кандидатам **только generic `sync`**. Даже если проверка/доставка пересекается с немедленным revoke, пакет не содержит Private ID/body, а HTTP GET обязательно перепроверяет права. Если DB check невозможен, fail closed и повторить после восстановления.
6. Отдельные изменения scope, не представленные в outbox (company role/grant/Planet epoch и expiry), ловятся **периодическим scopeStamp** и P-NATS-02 signals, но события никогда не заменяют самостоятельные авторизационные SQL функции.
7. На явную приватную/корпоративную/пространственную инвалидацию сначала отметить текущий view stale и прекратить отправку channel-specific hints, затем серверно refresh; никакой старый cache не становится allow.

## 8. Отдельные циклы безопасности

**Не удалять**:
- `Authenticate` действующей сессии для каждого WS с нынешней периодичностью порядка 2s (или эквивалентно проверяемое массовое переудостоверение).
- Ревалидацию текущего `chat_ws_leases` при write; renew приблизительно 10s, expiry 45s.
- `chat_scope_stamp` / обоснованную ревалидацию доступов для каждого активного пользователя не реже существующего интервала на первом пилоте (~2s), даже если нет outbox. После измерения можно предложить отдельно оптимизировать это чтение.
- Read-time авторизацию Chat API; отсутствие fresh Planet presence → запрет пространственных операций.
- Проверки готовности Cosmic (все вкладки) и Planet (только spatial), независимо от NATS.

Можно уменьшить **число SQL round trips** при scopeStamp/Authenticate за счёт bounded batch, но не скрывать фактическую нагрузку БД. Сравнение A1/A2/B1 должно использовать одинаковый refresh/ACL.

## 9. Fallback и reconnect

| Состояние | Safety poll outbox | Scope/auth | Дополнительное действие |
|---|---|---|---|
| A1/A2/B1 signal healthy | проектно 10s | auth/scope не реже 2s в пилоте | Wake запускает scan немедленно |
| NATS offline, LISTEN broken, subscription invalid | 2s | без ослабления | alert, exponential reconnect, no manual game stop |
| После NATS reconnect / LISTEN reconnect | немедленный full scan | полный scope refresh | сначала subscription + Flush/commit LISTEN, затем reconcile |
| Outbox backlog / scan error | backoff ограниченный, максимум 2s при DB healthy | fail closed для ACL | cursor не продвигать при error |
| PostgreSQL недоступна | scan невозможен | HTTP/WS доступ запрещён по существующим правилам | NATS сигнал не восполняет DB |
| WS reconnect | fresh bootstrap/sync | new auth + new DB lease | не перенимать старый unauthorized state |

30s как fallback **не рекомендовать** без нагрузки/UX теста. Даже если не нарушается безопасность (HTTP проверяет ACL), пользователь может ждать 30s пропущенного сообщения. Примечание: 60s overlap — проектное число, верность зависит от фактических bounds.

## 10. Медленные клиенты и stop

- Per-WS mailbox `syncPending` capacity 1: ставим bool, очередное sync сливается с имеющимся. ACK команд отдельно, ACK нельзя потерять из-за переполнения sync.
- Dispatcher никогда не ждёт выполнения `conn.Write`; единственный WS writer применяет существующий timeout 3s. Плохое соединение закрывается, клиент восполняет историю после reconnect.
- При graceful shutdown: запрет новых регистраций; stop WakeSource (NATS Drain с bounded timeout), cancel scanner, остановить receivers, закрыть WS, Release lease с точным generation, дождаться workers. Сначала изолировать из registry старые handles.
- Если shutdown упал между COMMIT и сигналом, данные остаются в БД; новый process after startup и local clients bootstrap вернут состояние. Отдельного durable WS fanout не требуется.
- Не логировать message body, cookie, Private conversation ID в telemetry tags; счетчики per source/kind only.

## 11. Acceptance тесты

- 50/100/200/256 WS; одиночное сообщение, burst 100, устаревшая роль, групповой DM, изоляция Invite, Planet transition, logout, повторная регистрация.
- Одновременно один user из двух вкладок → вторая 409 до Upgrade, первое WS продолжает получать sync; release old generation не отключает новое.
- Race registration vs outbox COMMIT, signal before Subscribe/after Subscribe, crash before Publish, SQL error after partial scan; событие не пропадает и не делает ложного data leak.
- Lower outbox ID commits позже higher; overlap выбран по реальному длительному tx; no missed sync после резервной сверки.
- При Core broker down 5m сообщения хранятся и видны, auth/ACL действуют, latency соответствует fallback; reconnect принудительно сверяет.
- Connection slow write >3s прекращается без блокирования других; mailbox и goroutines bounded.
- Характеристики измерены и представлены отдельно: не путать уменьшение вызовов Changes с сокращением общей нагрузки DB.

Официальный порядок LISTEN: https://www.postgresql.org/docs/current/sql-listen.html . Core at-most-once: https://docs.nats.io/learn/core-nats/publish-subscribe .