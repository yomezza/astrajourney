-- PROJECT DRAFT. НЕ ПРИМЕНЯТЬ АВТОМАТИЧЕСКИ И НЕ ЗАПУСКАТЬ НА РАБОЧЕЙ БД.
-- A2 rollback; выполняет только Cosmic migration owner. Старый Chat и
-- chat_outbox сохраняются, никакие сообщения не удаляются.
-- Остановить/переключить A2 listener на legacy/A1/B1 СНАЧАЛА; отключение
-- NOTIFY не должно выключать обязательный резервный polling.
-- Внешняя транзакция DBA: BEGIN; SET LOCAL search_path ...; SET LOCAL lock_timeout='2s';

DO $guard$
BEGIN
  IF current_schema() IS NULL OR current_schema()='pg_catalog'
     OR to_regclass('chat_outbox') IS NULL THEN
    RAISE EXCEPTION 'Wrong DB_SCHEMA: refusing A2 rollback';
  END IF;
END
$guard$;

-- DROP TRIGGER требует короткого DDL lock: при busy DB abort on lock_timeout,
-- не ждите бесконечно на рабочих INSERT.
DROP TRIGGER IF EXISTS chat_outbox_wakeup_a2 ON chat_outbox;
-- RESTRICT: если функция неожиданно где-то используется, остановиться.
DROP FUNCTION IF EXISTS chat_outbox_notify_statement_v1() RESTRICT;

-- Проверка после отката (должно быть 0):
SELECT count(*) AS remaining_a2_triggers
FROM pg_trigger
WHERE tgrelid='chat_outbox'::regclass AND tgname='chat_outbox_wakeup_a2';
