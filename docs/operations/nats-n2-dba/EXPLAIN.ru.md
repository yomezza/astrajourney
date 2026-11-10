# EXPLAIN (ANALYZE, BUFFERS) — PostgreSQL N2 DBA

**Реальные измерения**, 2026-11-10, отдельный PostgreSQL 17.11, база cosmos_n2_dba_test, схема nats_dba_test, 1 002 416 строк chat_outbox. Запуск [42756857729](https://github.com/yomezza/cosmosimulator/actions/runs/42756857729), исходные полные планы: EXPLAIN JSON в артефакте nats-n2-dba-results, файлы explain-or.json, explain-union.json, explain-n1-outboxpage.json.

Параметры: highwater=952419, cutoff=2026-11-10T04:37:00.384949Z, в результате каждого запроса 1000 строк. Сравнение проводилось в одной тестовой базе после ANALYZE chat_outbox. Встроенные реальные значения Actual/Rows/Blocks приведены ниже; это не предполагаемый план.

## 1. Точная текущая выборка Chat 0.24.7

Источник: internal/chat/dispatch_reader.go — метод OutboxPage. Подставлены after=0, max_id=1002419, highwater=952419, cutoff и LIMIT=1000.

~~~sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT id, event_kind, channel_id::text, target_user_id,
       reference_id::text, created_at
FROM chat_outbox
WHERE id > $1 AND id <= $2 AND (id > $3 OR created_at >= $4)
ORDER BY id
LIMIT 1000;
~~~

**Execution Time: 0.295 ms; Planning Time: 0.223 ms; Shared Hit Blocks: 18; Shared Read Blocks: 0.**

Фактический узел:

~~~text
Limit (Actual Rows: 1000)
  Index Scan using chat_outbox_pkey on chat_outbox
    Filter: id > 952419 OR created_at >= cutoff
    Actual Rows: 1000
~~~

Полный JSON план в артефакте CI: **explain-n1-outboxpage.json**.

## 2. Простой OR без верхней границы окна

~~~sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT id,event_kind,channel_id,target_user_id,reference_id
FROM chat_outbox
WHERE id > $1 OR created_at >= $2
ORDER BY id LIMIT 1000;
~~~

**Execution Time: 0.339 ms; Planning Time: 0.157 ms; Shared Hit: 1; Shared Read: 14.**

~~~text
Limit (Actual Rows: 1000)
  Index Scan using chat_outbox_pkey on chat_outbox
    Filter: id > 952419 OR created_at >= cutoff
    Actual Rows: 1000
~~~

Это отдельный эксперимент; у точного OutboxPage дополнительно есть id>after и id<=window.max.

## 3. UNION ALL из взаимоисключающих ветвей

~~~sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT *
FROM (
 SELECT id,event_kind,channel_id,target_user_id,reference_id
 FROM chat_outbox WHERE id > $1
 UNION ALL
 SELECT id,event_kind,channel_id,target_user_id,reference_id
 FROM chat_outbox WHERE id <= $1 AND created_at >= $2
) e ORDER BY id LIMIT 1000;
~~~

**Execution Time: 13.835 ms; Planning Time: 0.128 ms; Shared Hit: 643; Shared Read: 18.**

~~~text
Limit (Actual Rows: 1000)
  Gather Merge (Actual Rows: 1000, Workers Launched: 2)
    Sort (top-N heapsort)
      Append
        Bitmap Heap Scan on chat_outbox
          Bitmap Index Scan using chat_outbox_recent
        Index Scan using chat_outbox_pkey
~~~

Полный JSON: **explain-union.json**. UNION ALL с двумя disjoint диапазонами возвращает те же 1000 первых ID, но на данном распределении приводит к более дорогой сортировке. Нельзя автоматически заменять OR на UNION из предположения, что два индексных пути всегда лучше.

## Вывод по индексам

Уже существуют три индекса:
- chat_outbox_pkey (id)
- chat_outbox_recent (created_at, id)
- chat_outbox_pending (id) WHERE processed_at IS NULL

**Новый индекс для N2/A2 не нужен по результатам данного прогона.** Сохранить текущую структуру до реальных измерений профиля Private/Corp/Spatial, многократных циклов scan, активного INSERT rate и bloat/vacuum в отдельной целевой нагрузке. Отдельный индекс по event_kind, channel_id или target_user_id на уровне общего page scan не обоснован.

Числа из EXPLAIN относятся к единственному набору данных, конкретному cache state и последовательности запросов. Они не измеряют SQL ACL resolver, session checks, WS fanout, commit→sync или полную стоимость Chat.

[Основной протокол](README.ru.md), [workflow](https://github.com/yomezza/cosmosimulator/blob/main/.github/workflows/nats-n2-dba.yml).
