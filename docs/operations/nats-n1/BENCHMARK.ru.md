# Измерения legacy / A1 — N1

Дата: 2026-11-09. **Реальный короткий лабораторный прогон**, не теоретический расчёт.

## Стенд и методика

- Linux amd64, Go 1.27.3; официальный PostgreSQL 17.11 в отдельном контейнере.
- Во время записи `nproc` показывал 5 доступных logical CPU. CPU ниже дан как процент одного ядра (100% = одно ядро), не процент всей машины.
- Уникальная schema и restricted runtime/definer roles на профиль; только БД `cosmos_n1_test`. Рабочая БД владельца не использовалась.
- Chat работает в отдельном нативном test-instrumented процессе через настоящий App.Serve/WS/Service.Execute, pool max 32. Генератор WS находится в другом процессе и не входит в CPU/heap/RSS Chat.
- Health Cosmic/Planet задаёт локальный HTTP stub. Нет реального Cosmic gateway, браузерного rendering и загрузки истории по HTTP; их нагрузка не измеряется.
- Для каждого режима: 3s warmup после подключения всех клиентов, затем 4s idle и 10s active. Active — 4 настоящих Global message.send, по одному каждые 2.5s от разных игроков, с обычным ACK, rate policy и COMMIT.
- Профили 50/100/200/256 WS; на каждый новая schema/новые аккаунты. Порядок фиксирован: legacy → A1. Нет рандомизации или пяти повторов; результат предварительный.
- COMMIT timestamp — немедленно после успешного возврата tx.Commit в Go; sync timestamp — после получения/декодирования WS в генераторе. Оба процесса используют общий Linux CLOCK_MONOTONIC, не сравнение несинхронизированных часов. Это исключает время DB COMMIT-ответа до возврата в Go и не является UI latency.
- SQL round trips — дельта pgx Query/Exec tracer процесса Chat (включая BEGIN/COMMIT и служебные запросы). PG top/all — реальные pg_stat_statements, track=all, для runtime и definer roles; all включает nested SQL функций. Эти показатели намеренно разделены.
- CPU Chat — дельта getrusage дочернего процесса. CPU PG — дельта cpu_usage.total_usage всего выделенного PostgreSQL container, включая фоновые процессы и измерительные SQL probes. Короткое bracketing измерений даёт небольшую погрешность.
- Memory — конечные RSS из /proc/self/statm и heap/goroutines из runtime, не peak memory. Параметры обоих режимов идентичны кроме dispatcher_mode; A1 10s/2s/60s, coalescing 50ms, security/scope 2s.

## Active: SQL и COMMIT → sync

| WS | Режим | SQL round trips/s | PG top/s | PG all/s | Changes за фазу | p50 ms | p95 ms | p99 ms | samples |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 50 | legacy | 163.0 | 163.9 | 264.3 | 250 | 944.0 | 1941.3 | 1946.4 | 200 |
| 50 | a1 | 66.1 | 67.4 | 187.8 | 0 | 54.1 | 55.1 | 55.1 | 200 |
| 100 | legacy | 318.0 | 318.9 | 519.2 | 500 | 956.6 | 1922.1 | 1954.3 | 400 |
| 100 | a1 | 121.4 | 122.7 | 363.1 | 0 | 55.8 | 57.6 | 57.6 | 400 |
| 200 | legacy | 628.0 | 628.9 | 1029.2 | 1000 | 958.8 | 1895.1 | 1949.2 | 800 |
| 200 | a1 | 231.1 | 232.4 | 712.7 | 0 | 65.6 | 66.3 | 67.0 | 800 |
| 256 | legacy | 801.5 | 802.4 | 1314.8 | 1280 | 963.9 | 1864.1 | 1934.5 | 1024 |
| 256 | a1 | 292.7 | 294.0 | 908.8 | 0 | 60.6 | 63.6 | 63.7 | 1024 |

samples = получатели × 4 COMMIT, а не столько независимых COMMIT. Квантили — эмпирические, без интерполяции.

## Active: ресурсы

| WS | Режим | Chat CPU % | PG CPU % | RSS MiB | Heap MiB | goroutines | лишние sync | потери | disconnect |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 50 | legacy | 1.9 | 3.0 | 20.0 | 3.8 | 109 | 0 | 0 | 0 |
| 50 | a1 | 1.4 | 2.3 | 18.9 | 1.8 | 110 | 0 | 0 | 0 |
| 100 | legacy | 3.5 | 5.2 | 22.1 | 3.7 | 209 | 0 | 0 | 0 |
| 100 | a1 | 2.8 | 4.0 | 22.1 | 2.8 | 210 | 0 | 0 | 0 |
| 200 | legacy | 6.6 | 8.7 | 29.4 | 6.6 | 409 | 0 | 0 | 0 |
| 200 | a1 | 4.7 | 7.6 | 28.5 | 7.2 | 410 | 0 | 0 | 0 |
| 256 | legacy | 8.7 | 11.7 | 33.6 | 11.1 | 521 | 0 | 0 | 0 |
| 256 | a1 | 5.7 | 8.2 | 32.4 | 6.5 | 522 | 0 | 0 | 0 |

## Idle

| WS | Режим | SQL round trips/s | PG all/s | Changes за фазу | Chat CPU % | PG CPU % | RSS MiB |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 50 | legacy | 150.0 | 249.9 | 100 | 1.6 | 2.8 | 18.7 |
| 50 | a1 | 50.0 | 150.0 | 0 | 1.1 | 2.0 | 17.4 |
| 100 | legacy | 299.9 | 499.9 | 200 | 3.3 | 6.6 | 20.8 |
| 100 | a1 | 100.0 | 299.9 | 0 | 2.0 | 3.2 | 20.2 |
| 200 | legacy | 599.9 | 999.8 | 400 | 6.0 | 7.5 | 26.5 |
| 200 | a1 | 200.0 | 599.9 | 0 | 4.1 | 7.5 | 24.9 |
| 256 | legacy | 767.8 | 1279.7 | 512 | 7.7 | 9.8 | 30.5 |
| 256 | a1 | 256.2 | 768.0 | 0 | 4.9 | 7.5 | 28.2 |

## Вывод и ограничения

В этом прогоне на 256 WS A1 снизил SQL round trips на 63.5%, PG tracked statements на 30.9%; p95 стал 63.6ms вместо 1864.1ms. Changes() в A1 — 0. Это не обещание сокращения **всей** нагрузки БД на 90%: session/scope/fence/presence проверки сохранены.

Во всех 16 measured фазах: missing/extra sync = 0, неожиданных disconnect и reconnect attempts = 0. Генератор не маскирует разрывы автоматическим reconnect; отдельный функциональный тест проверяет ручное переподключение. Ошибок ACK нет; после каждого профиля в БД проверены все 4 сообщения. Отрицательные проверки прав выполняются отдельными integration/race tests, а не имитируются нулём в счётчике benchmark.

Тест не моделирует многочасовой history backlog, Private/Corporation/Spatial трафик в нагрузке, browser HTTP GET после sync, пиковый memory, высокую message rate, WAN, реальную Windows-службу или production backup/latency. Нельзя переносить эти проценты и p99 как гарантии на другое оборудование. Для rollout и N2 нужны совместное ревью и более длинные повторные опыты.

Первый предварительный прогон также прошёл, но здесь опубликована только финальная серия после завершения исправлений и проверок. Она заняла около 160s вместе с setup/cleanup профилей; только idle+active измеряются отдельно.

## Воспроизведение

Нужны отдельная PostgreSQL 17 с ICU/pg_trgm и pg_stat_statements, preload `pg_stat_statements`, track=all, доступный только разработчику managed Docker socket для CPU counters. Создать extension в test DB до запуска. Никогда не указывать рабочую БД/контейнер; fixture требует `_test`, CPU probe — имя собственного container с prefix `cosmos-n1-`.

```sh
TEST_DATABASE_URL='<disposable PostgreSQL database ending _test>' \
RUN_CHAT_N1_BENCHMARK=1 N1_PG_CONTAINER=cosmos-n1-test \
CHAT_N1_BENCHMARK_REPORT=/tmp/chat-n1-results.json \
go test ./internal/server/integration -run '^TestChatN1Benchmark$' -count=1 -timeout=8m -v
```

Во время измерения не запускать другие тесты/нагрузку на том же PostgreSQL/CPU. Helper собирается нативно, без race instrumentation; race tests выполняются отдельно, чтобы не выдавать их overhead за production CPU.

Полные конечные значения фаз: [benchmark-results.json](benchmark-results.json).
