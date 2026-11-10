# N2 — фактически выполненные функциональные проверки

10 ноября 2026. PostgreSQL 17.11, Go 1.27.3 Linux, полная игровая схема,
restricted runtime через SET ROLE; изолированная БД `cosmos_n1_test`.

## Итог

Все перечисленные ниже сценарии имеют успешный результат. Матрица D01–D26:
**21 PASS, 1 PARTIAL (D13), 4 NOT RUN (D09/D22/D23/D24)**. Это итог только
описанного функционального объёма: 1000+/s, performance EXPLAIN/миллион строк,
50–256 WS, latency-percentiles и saturation NOTIFY **не запускались**.
D09 с изменением PostgreSQL timeout повторно не выполнялся.

| Проверка | Фактический результат |
| --- | --- |
| `go test -race ./internal/chatserver ./internal/chat ./internal/platform/configuration` | PASS; lifecycle, paging/dedupe/retry, generation, bounded queue, config A2, default legacy, strict/exclusive TOML |
| `TestChatN2SourceTransactionsReconnectAndIsolation` | PASS; финальная точечная версия 11.44s, включая zero-row INSERT, запрет прямого EXECUTE функции, два listener, reconnect и два schema |
| `TestChatN2SocketFallbackScopeAndReconnect` | PASS; финальная версия 14.85s, включая old generation teardown и сохранение replacement lease |
| `TestChatN2SlowWriterIsolation` | PASS, 10.62s |
| `TestChatN2SQLFailureRecovery` | PASS, 7.35s |
| `TestChatN2SourceUnavailableFallback` | PASS, 7.03s, отдельный listener TCP proxy отключён до регистрации |
| `TestChatN2MissingTriggerFallback` | PASS, 8.41s, registered LISTEN без trigger, 0 NOTIFY, shared polling |
| `TestChatN2PrivateCorporationAndCleanup` | PASS, 9.48s, настоящий A2 WS под restricted role |
| `TestChatN2SpatialTransition` | PASS, 12.42s, настоящий Planet и переход 1→2 без NATS |
| `TestChatN2LateCommitBeyondOverlap/a1` | PASS, 35.37s; timestamp lower-ID backdated за 60s, независимая сверка и History |
| `TestChatN2LateCommitBeyondOverlap/a2` | PASS, 95.57s; настоящая транзакция удерживается 65s, lower ID исключён SQL overlap; последующий state refresh и History |
| `TestChatN2RestartRetentionAndRollback` | PASS, финальная версия 15.52s; A2 restart после retention, A2→A1 и A2→legacy, сохранённый journal, закрытые listener/leases |
| `TestChatN1RecipientsAndPostCommit` | PASS, 8.62s; в том числе SQL-пагинация 2001 строки, targeted/null-private ACL, UUID/rollback |
| `TestChatN1SocketFallbackScopeAndReconnect`, `TestChatN1SQLFailureRecovery` | PASS, 13.43s / 7.62s |
| `TestChat240Transactions`, `TestChat240RestrictedRole`, `TestChat240LateCommittedOutbox` | PASS, 8.78s / 6.88s / 6.73s; прежние send/history/ACL/UUID/moderation/journal |
| `env -u TEST_DATABASE_URL go test ./...` | PASS; только unit/compile, DB-тесты в этом отдельном запуске ожидаемо skip; реальные DB-тесты перечислены выше |
| `go vet ./internal/chat ./internal/chatserver ./internal/platform/configuration ./internal/server/integration` | PASS |
| Windows amd64 cross-build Chat 0.24.8, проверка PE/MZ | PASS; не является запуском Windows Service на машине владельца |

Время у отдельных сценариев приведено только как длительность фактического
функционального теста. Это не замеры скорости игрового Chat и не p50/p95/p99.

## Найденная ошибка тестового стенда

Первый combined recovery/regression прогон занял 208.301s и вернул FAIL только
на `RestartRetentionAndRollback`: общий helper открывал новый `t.TempDir()`
с новым erasure journal при каждом перезапуске. Штатная защита правильно
отказала в старте (`erasure journal unavailable`). Product-код и проверка
журнала не ослаблялись: helper теперь принимает один сохранённый файл на все
перезапуски и закрывает прежний дескриптор после остановки App. Исправленный
сценарий отдельно завершился PASS для обоих целевых режимов.

Успешные части того первого прогона (ACL/N1/Chat240 и оба late-COMMIT сценария)
не выдаются за успешный общий exit code. После исправления test helper
выполнен focused race прогон source/socket/missing-trigger/restart, **51.237s,
PASS**. Первый основной A2 functional/race набор: **73.042s, PASS**.
Во время искусственно долгой tx допустимы bounded maintenance lock timeout;
успешный COMMIT сообщения и последующее чтение проверены отдельно.

## Воспроизведение

Нужна **собственная disposable** PostgreSQL 17 БД с именем, заканчивающимся
`_test`, правами fixture-owner на создание временных схем/ролей и уже заданным
`TEST_DATABASE_URL`. Не направлять команду на рабочую игровую БД.

```sh
go test -race ./internal/chatserver ./internal/chat ./internal/platform/configuration

go test -race ./internal/server/integration \
  -run '^(TestChatN2.*|TestChatN1(RecipientsAndPostCommit|SocketFallbackScopeAndReconnect|SQLFailureRecovery)|TestChat240(Transactions|RestrictedRole|LateCommittedOutbox))$' \
  -count=1 -timeout=9m -v
```

Один реальный late-COMMIT тест намеренно ждёт 65s, затем независимый refresh.
Это необходимо для воспроизведения ошибки за пределами 60s, а не нагрузка.
Тесты сами выполняют утверждённый для **стенда** DBA UP/DOWN и очищают схемы;
никакой команды для запуска примера SQL на production здесь нет.

[CI для такого же функционального набора](../../../.github/workflows/chat-n2.yml)
создаёт свежую PostgreSQL service database и не запускает прежний DBA runner
с performance наполнением. Результат будущего CI не подменяется локальными
успешными результатами; фактический статус смотреть в GitHub Actions.

[Отчёт N2 и ограничения](README.ru.md).
