# N2 DBA — ограничения допуска, роли и SQL rollback

**Статус:** тестовый SQL проверен; производственная миграция не утверждена. N2 Go-режим в Chat 0.24.7 отсутствует.

## Исправленный UP

Проект: [notify-up.example.sql](../../ideas/nats-event-bus/dba/sql/notify-up.example.sql).

Изначально проект содержал CREATE OR REPLACE FUNCTION, способный перезаписать постороннюю одноимённую функцию в выбранной DB_SCHEMA. Это **исправлено до выполнения тестов**: новый UP создаёт функцию, только если такого объекта нет; иначе сверяет исходный код, owner, SECURITY INVOKER, search_path, тип функции. Для существующего trigger сверяются tgfoid/tgtype/tgenabled/tgqual/tgnargs. Обнаружение отличающегося объекта → исключение и ROLLBACK всей внешней транзакции, а не перезапись.

Проверено: вручную создали чужую функцию той же сигнатуры, получили отказ UP, сравнили её неизменённый текст; удалили её и выполнили UP дважды. Создан ровно один AFTER INSERT FOR EACH STATEMENT trigger. Обычный INSERT от chat_runtime без прямого EXECUTE на функцию сработал.

**Тестовый объект:** владелец функции cosmos, prosecdef=false, search_path=pg_catalog; имена каналов cosmos_chat_ + md5(TG_TABLE_SCHEMA). Никаких payload-данных, новых таблиц, индексов, ролей и GRANT для Chat runtime. ALTER TABLE/TRIGGER, CREATE FUNCTION применяет Cosmic migration owner с правами владельца, не чат.

## Фактический откат в PostgreSQL

1. После D01–D24 запомнили содержимое relacl таблицы chat_outbox и count().
2. Выполнили [notify-down.example.sql](../../ideas/nats-event-bus/dba/sql/notify-down.example.sql) в outer SQL tx, затем повторили DOWN. Проверили отсутствие функции/триггера; обычные INSERT работают и больше не дают NOTIFY.
3. Выполнили UP дважды и проверили восстановление уведомлений.
4. Повторили DOWN, сравнили relacl и данные: права и строки сохранены. Проверка D25 **PASS**.

Испытание ограничено тестовой схемой; Production rollback требует сначала feature flag A2→legacy/a1, затем остановки LISTEN и отдельно согласованного короткого DDL окна. DROP TRIGGER / FUNCTION RESTRICT не удаляют игровые сообщения или outbox.

## PostgreSQL 17 транзакционный bound

D08: первая tx выделила lower id, вторая закоммитила higher; после 70.7s lower закоммичен, но 60s overlap его исключил. Это реально существующий failure mode текущего id+created_at сканера, даже при успешном NOTIFY.

D09: тестовая session transaction_timeout='3s' прервала транзакцию, запись не осталась. **Не установлено** такое ограничение для игровых учётных записей. Рекомендуем обсудить роль/базу ограниченную величину transaction_timeout для chat_runtime и других реальных writers, совместно с максимальным интервалом успешного fallback scan и допустимым запасом внутри overlap. Idle-only timeout недостаточен для actively running long SQL.

Если ограничивать tx нельзя, проектировать независимую периодическую сверку актуального состояния Chat, не зависящую от outbox highwater; если необходима delivery каждого hint — отдельно договориться о commit-order/watermark протоколе.

## Список блокирующих проверок перед production

- [ ] D16–D20 после реализации A2 listener: Private, Corp, Sector, revoke, session expiry, lease generation; именно E2E на A2, не перенос результатов A1.
- [ ] D21: full scope/history bootstrap после удаления из outbox за 1 день.
- [ ] D23: равные legacy/A1/A2/B1 50/100/200/256 WS и реальный COMMIT→WS sync, CPU/PG ops, sustained rates, p95/p99.
- [ ] D24: отдельный ресурсно-контролируемый тест переполнения NOTIFY queue и обработки COMMIT failures; мониторинг pg_notification_queue_usage и long listener tx.
- [ ] Зафиксировать max INSERT→COMMIT и worst scan delay либо полный reconcile, проверить alert и rollback по ошибке источника.
- [ ] Windows smoke для реального Chat service, DB_SCHEMA/privilege compatibility, резервные копии и отдельное разрешение на применение DDL.

**Для изолированной реализации N2/A2 все необходимые SQL подготовлены и проверены.** Это не разрешение применять их к рабочей базе.

[Итоговый протокол](README.ru.md) | [EXPLAIN](EXPLAIN.ru.md) | [фактический CI](https://github.com/yomezza/cosmosimulator/actions/runs/42756857729).
