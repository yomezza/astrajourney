-- PROJECT DRAFT. НЕ ПРИМЕНЯТЬ АВТОМАТИЧЕСКИ И НЕ ЗАПУСКАТЬ НА РАБОЧЕЙ БД.
-- P-NATS-01 / A2. PostgreSQL 17+. Не является очередной schema*.sql-миграцией.
-- Выполняет только Cosmic migration owner в ОДНОЙ транзакции после
-- установки проверенного search_path на единственную выбранную DB_SCHEMA.
-- Пример применения на ИЗОЛИРОВАННОЙ тестовой схеме:
-- BEGIN; SET LOCAL search_path TO nats_dba_test, pg_catalog;
-- SET LOCAL lock_timeout = '2s'; SET LOCAL statement_timeout = '10s';
-- [этот файл без внешнего BEGIN/COMMIT]; COMMIT;
-- Перед внедрением проверить pg_trigger/pg_proc и сделанный backup.

DO $guard$
BEGIN
  IF current_schema() IS NULL OR current_schema() = 'pg_catalog'
     OR to_regclass('chat_outbox') IS NULL
     OR to_regclass('chat_schema_state') IS NULL THEN
    RAISE EXCEPTION 'Wrong DB_SCHEMA or Chat migration 0.24 not installed';
  END IF;
  IF COALESCE((SELECT version FROM chat_schema_state WHERE id=1),-1) <> 1 THEN
    RAISE EXCEPTION 'Chat schema version differs; review A2 migration';
  END IF;
END
$guard$;

-- SECURITY INVOKER (по умолчанию): никаких дополнительных прав на игровые
-- таблицы, никакого SECURITY DEFINER, пустой payload.
-- TG_TABLE_SCHEMA задаёт физическую схему таблицы, а НЕ search_path runtime.
-- Имя канала в пределах одного DB: cosmos_chat_<md5(schema)>.
CREATE OR REPLACE FUNCTION chat_outbox_notify_statement_v1()
RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog
AS $notify$
BEGIN
  PERFORM pg_catalog.pg_notify(
    'cosmos_chat_' || pg_catalog.md5(TG_TABLE_SCHEMA),
    ''
  );
  RETURN NULL;
END
$notify$;

-- Не даём PUBLIC право создавать триггеры через функцию; runtime НЕ становится
-- владельцем схемы/таблицы и НЕ получает CREATE.
REVOKE ALL ON FUNCTION chat_outbox_notify_statement_v1() FROM PUBLIC;

-- Один trigger на INSERT statement (не на строку, и не строго один на tx).
-- Identical channel+payload в рамках tx PostgreSQL сворачивает в один NOTIFY.
-- INSERT с 0 строк также вызовет statement-trigger: допустимый ложный wake.
DO $install$
DECLARE expected oid := 'chat_outbox_notify_statement_v1()'::regprocedure;
DECLARE actual oid;
DECLARE actual_type integer;
BEGIN
  SELECT t.tgfoid, t.tgtype INTO actual, actual_type
  FROM pg_catalog.pg_trigger t
  WHERE t.tgrelid = 'chat_outbox'::regclass
    AND t.tgname = 'chat_outbox_wakeup_a2';
  IF actual IS NULL THEN
    CREATE TRIGGER chat_outbox_wakeup_a2
      AFTER INSERT ON chat_outbox FOR EACH STATEMENT
      EXECUTE FUNCTION chat_outbox_notify_statement_v1();
  ELSIF actual <> expected OR actual_type <> 4 THEN
    RAISE EXCEPTION 'Existing chat_outbox_wakeup_a2 has incompatible definition';
  ELSIF EXISTS (
    SELECT 1 FROM pg_catalog.pg_trigger
    WHERE tgrelid='chat_outbox'::regclass
      AND tgname='chat_outbox_wakeup_a2' AND tgenabled NOT IN ('O','A')
  ) THEN
    RAISE EXCEPTION 'Existing chat_outbox_wakeup_a2 is not enabled';
  END IF;
END
$install$;

-- Только диагностика, не новая запись:
SELECT current_database() AS database_name,
       current_schema() AS chat_schema,
       'cosmos_chat_' || md5(current_schema()) AS listen_channel,
       t.tgname, t.tgenabled, pg_get_triggerdef(t.oid) AS definition
FROM pg_trigger t
WHERE t.tgrelid='chat_outbox'::regclass AND t.tgname='chat_outbox_wakeup_a2';
