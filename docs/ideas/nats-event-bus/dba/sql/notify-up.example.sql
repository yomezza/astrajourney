-- DBA PROJECT DRAFT; NOT A PRODUCTION MIGRATION.
-- Do not run automatically and do not execute against production PostgreSQL.
-- Test-only: use a disposable *_test database, the complete game schema,
-- a verified DB_SCHEMA, the Cosmic migration owner, and one outer transaction.
-- Example psql: -X -v ON_ERROR_STOP=1 --single-transaction
--  -c 'SET search_path TO nats_dba_test, pg_catalog'
--  -c "SET lock_timeout='2s'; SET statement_timeout='10s'"
--  -f notify-up.example.sql
-- The SQL below does NOT BEGIN/COMMIT, because it is for the Cosmic runner.

DO $guard$
DECLARE schema_name text := current_schema();
BEGIN
  IF schema_name IS NULL OR schema_name IN ('pg_catalog', 'public')
    OR to_regclass(format('%I.chat_outbox', schema_name)) IS NULL
    OR to_regclass(format('%I.chat_schema_state', schema_name)) IS NULL THEN
    RAISE EXCEPTION 'Explicit game DB_SCHEMA required with Chat installed';
  END IF;
  IF COALESCE((SELECT version FROM chat_schema_state WHERE id=1), -1) <> 1 THEN
    RAISE EXCEPTION 'Unexpected Chat schema version';
  END IF;
  IF NOT EXISTS (
     SELECT 1 FROM pg_catalog.pg_class c
     WHERE c.oid=to_regclass(format('%I.chat_outbox',schema_name))
       AND c.relkind='r'
  ) THEN
    RAISE EXCEPTION 'chat_outbox must be a regular table';
  END IF;
END
$guard$;

-- No CREATE OR REPLACE: never overwrite an unrelated function.
-- On rerun, accept only OUR same-owner, same-source, same-security function.
-- SECURITY INVOKER, immutable payload, schema-specific LISTEN channel.
DO $install$
DECLARE
  schema_name text := current_schema();
  f_oid oid;
  f record;
  f_body text := $body$
BEGIN
  PERFORM pg_catalog.pg_notify(
    'cosmos_chat_' || pg_catalog.md5(TG_TABLE_SCHEMA),
    ''
  );
  RETURN NULL;
END
$body$;
  tr record;
BEGIN
  SELECT p.oid,p.prosrc,p.prosecdef,p.proconfig,p.proowner,p.prorettype,
         p.prolang,p.prokind
  INTO f
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname=schema_name AND p.proname='chat_outbox_notify_statement_v1'
    AND p.pronargs=0;
  IF NOT FOUND THEN
    EXECUTE $ddl$
CREATE FUNCTION chat_outbox_notify_statement_v1()
RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER
SET search_path=pg_catalog
AS $body$
BEGIN
  PERFORM pg_catalog.pg_notify(
    'cosmos_chat_' || pg_catalog.md5(TG_TABLE_SCHEMA),
    ''
  );
  RETURN NULL;
END
$body$;
$ddl$;
  ELSE
    IF f.prosrc IS DISTINCT FROM f_body
       OR f.prosecdef IS DISTINCT FROM false
       OR f.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog']::text[]
       OR f.proowner <> (SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)
       OR f.prorettype <> 'pg_catalog.trigger'::regtype
       OR f.prolang <> (SELECT oid FROM pg_catalog.pg_language WHERE lanname='plpgsql')
       OR f.prokind <> 'f' THEN
      RAISE EXCEPTION 'Foreign or changed chat_outbox_notify_statement_v1(): refusing replacement';
    END IF;
  END IF;
  f_oid := to_regprocedure(format('%I.chat_outbox_notify_statement_v1()', schema_name));
  IF f_oid IS NULL THEN
    RAISE EXCEPTION 'A2 function not installed';
  END IF;

  -- New functions default EXECUTE to PUBLIC; revoke before commit.
  EXECUTE 'REVOKE ALL ON FUNCTION '
    || format('%I.chat_outbox_notify_statement_v1()',schema_name)
    || ' FROM PUBLIC';

  SELECT t.tgfoid,t.tgtype,t.tgenabled,t.tgisinternal,t.tgqual,t.tgnargs
    INTO tr
  FROM pg_catalog.pg_trigger t
  WHERE t.tgrelid=to_regclass(format('%I.chat_outbox',schema_name))
    AND t.tgname='chat_outbox_wakeup_a2';
  IF NOT FOUND THEN
    EXECUTE 'CREATE TRIGGER chat_outbox_wakeup_a2 AFTER INSERT ON '
      || format('%I.chat_outbox',schema_name)
      || ' FOR EACH STATEMENT EXECUTE FUNCTION '
      || format('%I.chat_outbox_notify_statement_v1()',schema_name);
  ELSIF tr.tgfoid <> f_oid OR tr.tgtype <> 4 OR tr.tgenabled <> 'O'
       OR tr.tgisinternal OR tr.tgqual IS NOT NULL OR tr.tgnargs <> 0 THEN
    RAISE EXCEPTION 'Foreign or changed chat_outbox_wakeup_a2: refusing modification';
  END IF;
END
$install$;

-- Verify metadata before committing the OUTER transaction:
SELECT current_database() AS database_name, current_schema() AS game_schema,
  'cosmos_chat_' || pg_catalog.md5(current_schema()) AS listen_channel,
  t.tgname, t.tgenabled, pg_catalog.pg_get_triggerdef(t.oid) AS definition
FROM pg_catalog.pg_trigger t
WHERE t.tgrelid='chat_outbox'::regclass AND t.tgname='chat_outbox_wakeup_a2';
