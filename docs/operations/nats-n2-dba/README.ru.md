# Cosmosimulator — NATS N2 DBA: PostgreSQL 17, итог изолированной проверки

**Дата:** 2026-11-10. **Статус:** выполнена DBA-проверка на disposable PostgreSQL. **Разрешение на реализацию N2/A2 здесь не выдаётся**: его отдельно согласует владелец. Production rollout не разрешён.

## Воспроизводимость

- Исходный игровой код: main после N1, релиз Chat 0.24.7, до DBA изменений HEAD ade1450a08d93b6451c50844466a99c2b239f9ec. SQL-проверка по последнему тестовому коммиту [713429b9](https://github.com/yomezza/cosmosimulator/commit/713429b9212473fa78469de4ce505aaf9c45bc92).
- [Успешный запуск GitHub Actions № 42756857729](https://github.com/yomezza/cosmosimulator/actions/runs/42756857729); **тестовая БД cosmos_n2_dba_test**, PostgreSQL 17.11, Linux x86_64, официальный postgres:17 Docker service.
- Две полностью инициализированные **игровые** схемы nats_dba_test и nats_dba_test_b в одной тестовой БД. Использован настоящий initialization.OpenDatabase, все существующие schema*.sql, Chat schema marker 1, chatintegration.Functions, Partitions и GrantRuntime. Не создавали облегчённую подделку chat_outbox.
- Роли chat_runtime / chat_definer созданы как NOLOGIN NOINHERIT для CI. Испытания под chat_runtime выполняются через SET ROLE; это действительно ограниченные PostgreSQL-права, но не отдельный тест TCP LOGIN.
- Исходные данные с действующей игры **не использовались**. PostgreSQL производственного сервера, Cosmic/Planet/Chat службы, TOML, NATS и цепочка internal/server/schema*.sql **не изменялись**.
- Исходники тестов: [run_postgres.py](run_postgres.py), [seed.go](seed.go), [legacy.go](legacy.go) и [CI-workflow](../../../.github/workflows/nats-n2-dba.yml). Workflow фактически расположен в корне репозитория .github/workflows/nats-n2-dba.yml.
- В артефакте **nats-n2-dba-results** запуска 42756857729: summary.json, три полных EXPLAIN JSON и insert-commit-bench.json. Хеш артефакта sha256:d0d329d38453a1630ec89f3e078f4c941ad7c51504fcb32dceb6014c29237847.

## Итог по матрице

**18 PASS / 0 FAIL / 8 NOT RUN.** PASS относится только к указанному фактически выполненному сценарию; отдельные старые Chat/N1 регрессионные Go-тесты прошли, но не считаются проверкой ещё не существующего A2 Go-адаптера.

| ID | Статус | Наблюдение или точная причина |
|---|---|---|
| D01 | PASS | INSERT не виден до COMMIT; после COMMIT строка и пустой NOTIFY |
| D02 | PASS | ROLLBACK не оставляет ни строки, ни NOTIFY |
| D03 | PASS | 3 отдельных INSERT в одной транзакции → 1 идентичный NOTIFY слушателю |
| D04 | PASS | Один INSERT SELECT на 100 строк → 100 строк и 1 NOTIFY |
| D05 | PASS | INSERT с нулём строк вызвал один ложный wake, без изменения таблицы |
| D06 | PASS | Два независимых LISTEN получили сигнал после COMMIT |
| D07 | PASS | Меньший id=107 закоммичен после id=108; 60s overlap обнаружил меньший |
| D08 | PASS | При удержании транзакции **70,7 s** меньший id=109 после id=110 **не попал** в 60s overlap. PASS означает, что отрицательный тест подтвердил потерю, а не гарантию доставки |
| D09 | PASS | PostgreSQL 17 transaction_timeout=3s прервал долгую транзакцию, строка откатилась |
| D10 | PASS | Зафиксировали LISTEN до snapshot, затем получили следующий committed hint |
| D11 | PASS | При отключённом LISTEN сигнал пропущен; новый LISTEN + SELECT восстановил строку |
| D12 | PASS | UNLISTEN пропускает hint, fallback scan видит строку, новый LISTEN принимает следующие |
| D13 | PASS | 2300 событий: страницы 1000 / 1000 / 300, без потерь/дублей |
| D14 | PASS | chat_runtime может INSERT в outbox, триггер работает; CREATE/TRIGGER, UPDATE users и EXECUTE новой функции напрямую не разрешены; SECURITY DEFINER=false |
| D15 | PASS | Настоящий Go Chat Service без LISTEN, с A2 trigger, под chat_runtime: Authenticate, Claim, Changes, message.send, History |
| D16 | NOT RUN | Полный A2 WS-проход при изменении корпоративных прав невозможен: A2 Go listener/dispatcher mode в 0.24.7 не реализован. Нынешние ACL отдельно проверены N1 |
| D17 | NOT RUN | Конечный A2 WS/cleanup путь при удалении корпорации ещё не существует. Текущий Chat240CorporateFencesAndCleanup прошёл отдельно без A2 |
| D18 | NOT RUN | E2E переход сектора через A2 listener и реальный Planet/Chat не существует в 0.24.7; текущие Chat240SpatialPresence и N1SpatialRecipients прошли отдельно |
| D19 | NOT RUN | Полный A2 fanout Private/invites, включая ревокацию, требует A2 adapter; текущие Chat240Transactions и N1RecipientsAndPostCommit прошли без него |
| D20 | NOT RUN | Проверка live A2 WS lease/session expiry и teardown источника требует A2 adapter; штатные N1 socket/reconnect тесты прошли отдельно |
| D21 | NOT RUN | Проверен только действующий SQL DELETE outbox старше 1 суток (1 старая строка удалена); полный bootstrap клиента после outage/retention не прогнан |
| D22 | PASS | 1 002 416 строк; три настоящих EXPLAIN (ANALYZE, BUFFERS) и проверка одинаковых 1000 ID; см. [планы](EXPLAIN.ru.md) |
| D23 | NOT RUN | Нет режимов A2/B1 в Chat 0.24.7, поэтому честное сравнение 50/100/200/256 WS в четырёх режимах пока невозможно. Отдельный N1 baseline legacy/A1 есть |
| D24 | NOT RUN | Проверены 320 COMMIT с долго открытой транзакцией слушателя, очередь выросла до 0.0000019073 и вернулась к нулю; **8GB saturation и отказ COMMIT** не вызывались на общем CI runner |
| D25 | PASS | UP дважды, DOWN дважды, сигнал исчезает после DOWN и возвращается после UP; таблица, строки и relacl сохраняются. Реальные измерения INSERT/COMMIT выполнены |
| D26 | PASS | Две DB_SCHEMA: разные md5(schema) channel, отсутствие перекрёстных сигналов и строк |

Полный автоматизированный журнал и исходные JSON доступны в артефакте CI. Заявленных «успешных» тестов на продуктивной БД нет.

## Проверки SQL и прав

Перед первым UP создали **постороннюю** функцию с тем же именем и сигнатурой chat_outbox_notify_statement_v1(), но другим телом. UP **отказал**, текст функции не изменился. После удаления чужого объекта применили UP дважды: повторный запуск успешен и не создаёт дублей.

[Исправленный проект UP](../../ideas/nats-event-bus/dba/sql/notify-up.example.sql) больше **не** использует опасный CREATE OR REPLACE FUNCTION. Он выполняет create-only и сверяет исходный текст функции, владельца, SECURITY INVOKER, search_path, тип и тип триггера при повторной установке. [Проект DOWN](../../ideas/nats-event-bus/dba/sql/notify-down.example.sql) удаляет только проектные объекты без CASCADE.

Реальное состояние после UP: владелец функции cosmos (тестовый Cosmic migration owner), prosecdef=false, proconfig={search_path=pg_catalog}, AFTER INSERT FOR EACH STATEMENT на nats_dba_test.chat_outbox. Для runtime в тесте: INSERT=true, TRIGGER=false, CREATE(schema)=false, EXECUTE(A2-function)=false, UPDATE users запрещён; INSERT при этом успешно вызывает trigger. Роль chat_definer и действующие SECURITY DEFINER-функции не переписывались.

Сохранены SQL ACL, WS lease, company fences, Planet presence, Private-инварианты; в N1 ими продолжает заниматься Go/SQL. NOTIFY содержит **пустой payload**, не текст, user_id или ссылки, и никак не авторизует клиента.

## Производительность

См. [EXPLAIN.ru.md](EXPLAIN.ru.md) и [SQL rollback/допуск](SQL-ROLLBACK.ru.md). На 1 002 416 строках реальный запрос текущего OutboxPage дал **0,295 ms**; упрощённый OR — **0,339 ms**; экспериментальный UNION ALL — **13,835 ms**. Использованы существующие PK(id) и (created_at,id), новых индексов **не предлагается**. Это единичный EXPLAIN на тестовом распределении, не прогноз на заполненной рабочей БД.

В тесте по 250 последовательных транзакций на каждый режим (одна строка INSERT + COMMIT, малая нагрузка):

| Серия | Медиана INSERT, ms | Медиана COMMIT, ms | p95 INSERT / COMMIT, ms |
|---|---:|---:|---:|
| A2 включён, до отката | 0,2103 | 0,2374 | 0,2455 / 0,3163 |
| Без триггера | 0,1779 | 0,2284 | 0,2783 / 0,3658 |
| A2 включён повторно | 0,1810 | 0,2159 | 0,2480 / 0,2837 |

В других успешных CI-прогонах порядок величин похожий, но относительные разницы колебались и иногда меняли знак. **Стабильный процент удорожания INSERT/COMMIT не установлен.** При production-оценке нужны повторные рандомизированные прогоны с конкуренцией, WAL, pg_stat_statements, CPU, p95/p99, peak queue, изолированным сервером.

## Поздние COMMIT: обязательное решение

bigserial выдаёт id до COMMIT. N1 OutboxPage, A2 и B1 имеют одну и ту же потенциальную проблему: после highwater=110 delayed lower id=109 с created_at старше overlap не найдётся; сигнал может вызвать скан, который сам исключит запись. Обычный 10s fallback с тем же предикатом **не чинит** это молчаливое пропускание. Утверждать гарантированную доставку нельзя.

Предлагаемый gate:
1. Измерять/ограничить INSERT→COMMIT всех реальных writers chat_outbox, в том числе maintenance, с запасом: **max transaction age + worst successful scan delay + scheduling margin < overlap**.
2. Для Chat-runtime PostgreSQL 17 рассмотреть отдельный session/role-scoped transaction_timeout (например 30s как обсуждаемый эксперимент, а не автоматический продакшен-параметр), не применять к общим Cosmic/Planet без аудита. Наш D09 фактически проверил работу transaction_timeout=3s в отдельной тестовой сессии.
3. Если верхнюю границу гарантировать нельзя, добавить независимую периодическую сверку актуального состояния доступных каналов/истории клиентов, а не только chat_scope_stamp, и явно согласовать семантику «все hints» против «eventual UI state». Для строгой ordered delivery нужен отдельный протокол commit ordering, а не произвольное overlap.

## Rollback и риски

Rollback действительно проверен на тестовом PostgreSQL: после отключения слушателя SQL DOWN удалил trigger/function, таблица/данные/relacl остались, legacy send и SQL под chat_runtime совместимы. Повторный UP восстановил notification, финальный DOWN оставил только старые объекты. Производственную процедуру выполнять только Cosmic migration owner с правильным DB_SCHEMA, одной транзакцией, lock_timeout и отдельным разрешением.

Незакрытое: pg_notify в экстремуме заполненной очереди способен привести к ошибке **COMMIT** и увеличить накладные расходы прежнего Chat; поэтому нужен контроль pg_notification_queue_usage, долгих LISTEN transactions, ошибок коммита, дешёвый возврат к legacy, а затем удаление trigger по согласованному окну. Даже один disabled A2 listener не отключает стоимость установленного trigger.

## Решение DBA

**Разработчику можно разрешить реализацию N2/A2 на отдельном стенде — только после отдельного согласования владельцем.** DBA-часть создана и проверена для начала изолированного разработки; менять active Cosmic schema*.sql и рабочую PostgreSQL не требуется. Рекомендуемая последовательность — опциональный режим A2 + dedicated LISTEN connection → LISTEN/COMMIT → authoritative scan → ordinary fallback/reconnect и обязательный SQL ACL. Старый legacy сохраняется, A1/B1 архитектурно используют тот же reader.

**Production N2/A2 пока НЕ разрешать.** До него обязательны D16–D21, D23 и безопасно расширенный D24 после создания A2 Go-кода, worst-case transaction bound/реconciliation решение, измерения на реальной целевой архитектуре, Windows smoke, rollback drill и отдельное одобрение.

## Ссылки

- [Архитектура NATS](../../ideas/nats-event-bus/README.md), [N1 отчёт](../nats-n1/README.ru.md), [DBA проект](../../ideas/nats-event-bus/dba/README.ru.md).
- [SQL с созданием A2](../../ideas/nats-event-bus/dba/sql/notify-up.example.sql), [SQL отката](../../ideas/nats-event-bus/dba/sql/notify-down.example.sql).
- [GitHub Actions — фактические результаты и артефакт](https://github.com/yomezza/cosmosimulator/actions/runs/42756857729).
