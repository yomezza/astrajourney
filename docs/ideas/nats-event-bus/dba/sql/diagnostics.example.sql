-- PROJECT DRAFT. Только диагностические SQL, НИЧЕГО НЕ МЕНЯТЬ.
-- PostgreSQL 17. Выполнять с проверенным search_path = <TEST_DB_SCHEMA>.
-- EXPLAIN ANALYZE и полный COUNT на заполненной базе -- ТОЛЬКО тестовая БД.
-- На рабочей БД разрешены только согласованные DBA read-only диагностические
-- запросы со statement_timeout. Здесь намеренно нет VACUUM/ANALYZE/DDL.

SELECT current_database() AS db, current_schema() AS schema, version()
  AS postgres_version, current_setting('search_path') AS path;
SELECT a.attname, pg_get_serial_sequence('chat_outbox','id') AS seq
  FROM pg_attribute a WHERE a.attrelid='chat_outbox'::regclass
  AND a.attname IN ('id','event_kind','channel_id','target_user_id',
                   'reference_id','created_at','processed_at')
  ORDER BY a.attnum;
SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint
 WHERE conrelid='chat_outbox'::regclass;
SELECT indexname,indexdef FROM pg_indexes
 WHERE schemaname=current_schema() AND tablename='chat_outbox'
 ORDER BY indexname;
SELECT pg_size_pretty(pg_relation_size('chat_outbox')) AS heap,
       pg_size_pretty(pg_indexes_size('chat_outbox')) AS indexes,
       pg_size_pretty(pg_total_relation_size('chat_outbox')) AS total,
       (SELECT reltuples::bigint FROM pg_class
         WHERE oid='chat_outbox'::regclass) AS estimated_rows;
SELECT n_live_tup,n_dead_tup,n_tup_ins,n_tup_del,seq_scan,idx_scan,
       last_vacuum,last_autovacuum,last_analyze,last_autoanalyze
FROM pg_stat_user_tables WHERE relid='chat_outbox'::regclass;
-- Снимите два значения n_tup_ins с интервалом 60 s (с учётом stats reset),
-- разница / 60 -- наблюдённые inserts/s, не число сообщений.
SELECT count(*) AS rows_last_minute,
       count(*) FILTER (WHERE channel_id IS NULL) AS null_channel,
       count(*) FILTER (WHERE target_user_id IS NOT NULL) AS targeted,
       count(*) FILTER (WHERE processed_at IS NULL) AS unprocessed
FROM chat_outbox WHERE created_at >= clock_timestamp()-interval '1 minute';
SELECT event_kind,count(*) AS n FROM chat_outbox
WHERE created_at>=clock_timestamp()-interval '1 hour'
GROUP BY event_kind ORDER BY n DESC, event_kind;
SELECT 'cosmos_chat_'||md5(current_schema()) AS listen_channel,
       pg_notification_queue_usage() AS notification_queue_fraction;
SELECT pid,usename,application_name,state,now()-xact_start AS xact_age,
       wait_event_type,wait_event
FROM pg_stat_activity
WHERE datname=current_database() AND xact_start IS NOT NULL
ORDER BY xact_start LIMIT 20;
-- Нет канальной ACL в PostgreSQL; результат доступности только диагностика:
SELECT has_table_privilege('chat_runtime','chat_outbox','INSERT') AS runtime_insert,
       has_table_privilege('chat_runtime','chat_outbox','TRIGGER') AS runtime_trigger,
       has_schema_privilege('chat_runtime',current_schema(),'CREATE') AS runtime_create,
       has_function_privilege('chat_runtime','pg_catalog.pg_notify(text,text)','EXECUTE') AS notify_exec;
-- Только если роль chat_runtime существует. Другие имена проверяйте отдельно.

-- PLAN TEST ONLY. Демонстрационные параметры привязаны к распределению
-- данных, сравнить 0/1/1_000/100_000/1_000_000 строк на тестовой БД.
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
WITH params AS (
 SELECT greatest(coalesce(max(id),0)-5000,0)::bigint AS hw,
        clock_timestamp()-interval '60 seconds' AS cutoff
 FROM chat_outbox
)
SELECT o.id,o.event_kind,o.channel_id,o.target_user_id,o.reference_id
FROM chat_outbox o CROSS JOIN params p
WHERE o.id>p.hw OR o.created_at>=p.cutoff
ORDER BY o.id LIMIT 1000;

-- Сравнить OR и взаимоисключающие ветви UNION ALL; отсутствие дублей
-- обеспечивается id>hw против id<=hw. LIMIT планируется поверх UNION.
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
WITH params AS (
 SELECT greatest(coalesce(max(id),0)-5000,0)::bigint AS hw,
        clock_timestamp()-interval '60 seconds' AS cutoff
 FROM chat_outbox
), candidate AS (
 SELECT o.id,o.event_kind,o.channel_id,o.target_user_id,o.reference_id
 FROM chat_outbox o CROSS JOIN params p WHERE o.id>p.hw
 UNION ALL
 SELECT o.id,o.event_kind,o.channel_id,o.target_user_id,o.reference_id
 FROM chat_outbox o CROSS JOIN params p
 WHERE o.id<=p.hw AND o.created_at>=p.cutoff
)
SELECT * FROM candidate ORDER BY id LIMIT 1000;

-- На каждой итерации cursor внутри pass -- ПОСЛЕДНИЙ ПРОЧИТАННЫЙ id,
-- highwater общий не поднимать до полной обработки всех batch.
-- Реальный запрос параметризовать ($1=old_highwater,$2=fixed_cutoff,
-- $3=pass_last_id,$4=batch_limit). По сортировке проверять план:
-- WITH candidate AS (
--  SELECT id,event_kind,channel_id,target_user_id,reference_id,created_at
--    FROM chat_outbox WHERE id>$1 AND id>$3
--  UNION ALL
--  SELECT id,event_kind,channel_id,target_user_id,reference_id,created_at
--    FROM chat_outbox WHERE id<=$1 AND created_at>=$2 AND id>$3
-- )
-- SELECT * FROM candidate ORDER BY id LIMIT $4;
