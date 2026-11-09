# 02. Индексы, пакетный scan и производительность

**Статус:** аналитическая оценка по main от 2026-11-09; планы EXPLAIN, реальные bytes/rows/latency неизвестны. Нельзя утверждать, что новый запрос быстрее, пока на отдельной тестовой PostgreSQL не получены планы с BUFFERS.

## Индексы: решение на P01

Существуют:
- chat_outbox_pkey(id) — B-tree и основной путь id > highwater.
- chat_outbox_recent(created_at,id) — B-tree и основной путь временного перекрытия.
- chat_outbox_pending(id) WHERE processed_at IS NULL — **не обязан** помогать сканеру без такого WHERE. В действующем коде запись processed_at не обнаружена; этот индекс потенциально дублирует цену insert по ID. Удалять сейчас нельзя: это изменение существующего контракта, нужна проверка всех потребителей/планов.

**Новых индексов для A1/A2/B1 на N1–N3 сейчас не рекомендую.** Особенно не добавлять индекс на target_user_id ради нового сканера: он читает очередной диапазон всех событий, а права проверяет отдельным SQL/Go. Индекс event_kind тоже не нужен без доказанного фильтра и статистики. Каждый индекс увеличивает WAL, insert cost, vacuum и storage.

## Сканирование и конкуренция

Кандидат по архитектуре: (id > старый highwater) OR (created_at >= зафиксированный cutoff = clock_timestamp()-overlap). Считать оба источника возможных committed событий. Для небольшого набора planner может выбрать BitmapOr/Index Scan, для больших долей — Seq Scan; обе стратегии допустимы в зависимости от selectivity. Для диагностики сравнить:
1. OR, ORDER BY id, LIMIT batch.
2. Две **взаимоисключающие** ветви UNION ALL: id>highwater и id<=highwater AND created_at>=cutoff; ORDER BY id, LIMIT batch. Это избегает dedup-sort, но не гарантирует лучшие планы.
3. Наличие LIMIT 1000 не делает выборку целиком обработанной. Пока есть backlog, сохраняется отдельный pass cursor last_scanned_id. Highwater продвигается только после полного прохода и enqueue, без потери нижних строк.
4. SQL на каждой странице получает фиксированные для прохода highwater и cutoff, условие id > pass_last_id, читает только метаданные. Новый committed low-ID, появившийся после уже просмотренной страницы, восстанавливается следующим overlap pass при соблюдении bound.
5. recentSeenIDs — bounded Go дедупликация, не новый SQL persistent state. Если кеш сброшен, повторный generic sync допустим; пропуск недопустим.

Пример логической формы, параметры: $1=base_highwater, $2=fixed_cutoff, $3=pass_last_id, $4=batch_size:

~~~sql
WITH candidate AS (
  SELECT id,event_kind,channel_id,target_user_id,reference_id,created_at
    FROM chat_outbox WHERE id>$1 AND id>$3
  UNION ALL
  SELECT id,event_kind,channel_id,target_user_id,reference_id,created_at
    FROM chat_outbox
   WHERE id<=$1 AND created_at>=$2 AND id>$3
)
SELECT * FROM candidate ORDER BY id LIMIT $4;
~~~

Начальное highwater=0 на пустой/новой установке не обязательно означает бесконечный replay: новый WS получает авторитетный bootstrap, старый процессный курсор не переносится без проверки. После downtime >1 day обязательна полная сверка scope/state, не попытка восстановить все outbox hints.

## Поздний COMMIT: порог 60s

BIGSERIAL выдаёт ID в порядке nextval, **не в порядке COMMIT**. created_at = clock_timestamp() вычисляется при INSERT. Открытая транзакция A пишет id=100, B пишет id=101 и фиксирует COMMIT раньше A. После B max(id)=101; чистый id>101 потеряет A. При A COMMIT через 3s перекрытие 60s его увидит. При A COMMIT через 75s timestamp INSERT старше cutoff и его не увидит. В текущем коде Go statement_timeout=5s не устанавливает глобальный bound на всю транзакцию; срок запроса/транзакции в отдельных путях различается.

**Не считать 60s гарантией**, пока DBA и backend не подтвердили максимальное INSERT→COMMIT время всех writers плюс максимальный интервал реальных успешных scan, scheduler jitter и запас. Для PostgreSQL 17 существует transaction_timeout, который может завершить чрезмерно длинную транзакцию; отдельные role/session settings требуют теста с pgx и одобрения, а применение к общей Cosmic DB без согласования запрещено. idle_in_transaction_session_timeout **не ограничивает** активно выполняющуюся длинную транзакцию. При подготовленных транзакциях нужны отдельные правила.

Без доказанного bound безопасный путь для **текущего UI**: регулярная полная перепроверка разрешённого состояния каждого аккаунта/сферы, независимо от outbox; одно только окно сканирования не даёт полного журнала событий. Для строгой *событийной* полноты без bound потребуется иной commit-order механизм (например, логическое декодирование по commit LSN, с эксплуатационными требованиями), либо сериализация выделения ID перед COMMIT, что ухудшит параллелизм. Не вводить это в P01 без отдельного решения. Полная сверка не заменяет историю событий, если бизнес-логика требует replay.

## Предварительная нагрузка от старого polling

Только **расчёт**, основанный на одном WS tick каждые 2s: Changes() выполняет 3 SQL SELECT/тик, плюс один Authenticate/тик. Renew в среднем ~раз в 10s; HTTP запросы и фоновое обслуживание не учтены.

| Одновременных WS | Changes()/s | SELECT внутри Changes()/s | Authenticate()/s | Примерная сумма SELECT/s* |
|---:|---:|---:|---:|---:|
| 50 | 25 | 75 | 25 | 100 |
| 100 | 50 | 150 | 50 | 200 |
| 200 | 100 | 300 | 100 | 400 |
| 256 | 128 | 384 | 128 | 512 |

*Исключая вложенные SQL в функциях, BEGIN/COMMIT, Renew (~5/10/20/26 UPDATE/s), пользовательские GET и write workload. Это НЕ PostgreSQL statements/s, измеренный pg_stat_statements; pg_stat_statements может агрегировать функции иначе.*

Один shared scan по таймеру в healthy 10s соответствует 0.1 плановому scan/s при отсутствии wakeup; degraded 2s — 0.5 scan/s; на каждый wake после coalescing будут дополнительные проходы. При этом ревалидация сессий/прав в P01 не реже 2s — не обещать 90% снижения всей SQL-нагрузки. 50 клиентский transport-тест уже есть (chat240_transport_test.go), на 100/200/256 реального измерения пока нет.

## Проверка

[SQL для диагностики и OR/UNION EXPLAIN](sql/diagnostics.example.sql): прогнать пустую, прогретую и заполненную таблицу (1k/100k/1m строк) с действительными event_kind/NULL пропорциями и отдельными объёмами private/corporation/spatial; capture actual rows, loops, buffers shared hit/read, sort/heap fetch, execution time, inserts/s и vacuum. Для fingerprint Changes() выполнить отдельный EXPLAIN (ANALYZE,BUFFERS) с реальным тестовым пользователем, включая текущий chat_scope_stamp и реальные joined channels. Полный SQL Changes() брать без изменения из internal/chat/changes.go. Сопоставить 50/100/200/256 соединений по одной нагрузке/одинаковым ACL.

**Gate:** не добавлять индексы по интуиции; сначала data volume, планы и total cost. После изменения индексов — сравнить INSERT p95/p99, WAL и объём таблиц.
