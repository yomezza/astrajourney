# Планетарный процесс · P2-1 / 0.15.0

Это работающий независимый процесс с HTTP, PostgreSQL, контролем владельца runtime и Windows SCM-адаптером. **2D-сцены, персонажи, билеты высадки и WebSocket-сессии пока отсутствуют.** Существующая информационная высадка первой очереди не превращается в 2D-игру от запуска этого EXE.

## Подготовка

Остановить прежнюю космическую службу, сделать резервную копию игровой схемы и запустить cosmic 0.15.0 с прежними DATABASE_URL/DB_SCHEMA. Существующий migration runner применит schema150.sql под прежней блокировкой 784216. Добавляются только `planet_schema_state` и `planet_runtime_leases`; счета/корабли/колонии/визиты не изменяются. Маркер версии 1 не заменяет мировой tick и не означает готовность всех будущих P2-таблиц.

Planet использует чистый `platform/postgres.Open`: нет создания схемы, DDL, seed или ADMIN bootstrap. Если схема не подготовлена, HTTP-процесс запускается, но ready=503. Если PostgreSQL недоступна уже при первоначальном подключении, процесс завершается с ошибкой; потеря подключения после старта оставляет live=200/ready=503.

## Запуск из PowerShell

В отдельном окне, на той же БД и схеме:

```powershell
$env:DATABASE_URL = 'postgres://cosmos_planet:URL_ENCODED_PASSWORD@127.0.0.1:5432/existing_database?sslmode=disable'
$env:DB_SCHEMA = 'cosmosimulator'
$env:PLANET_HTTP_ADDR = '127.0.0.1:8081'
.\downloads\cosmosimulator-planet-v0.15.0-windows-amd64.exe
```

Проверки:

```powershell
Invoke-RestMethod http://127.0.0.1:8081/planet/api/v1/health/live
Invoke-RestMethod http://127.0.0.1:8081/planet/api/v1/health/ready
```

Ответ ready=200 содержит `service=cosmosimulator-planet`, `stage=P2-1`, `status=ready`, **`gameplay_ready=false`**. Это готовность инфраструктуры, не разрешение высадки. `GET /planet/ws/v1` всегда отвечает 503 `planetary_gameplay_not_implemented`; ticket не принимается и upgrade не выполняется.

Другой planet на этой же схеме может слушать другой порт, но получит ready=503 `lease_in_use`. После завершения владельца standby автоматически повторяет захват. Случайный owner UUID и монотонный fence защищают от освобождения новой блокировки старым процессом. После crash lease истекает; старый owner не может продлить уже истёкший fence. Все сроки проверяются по часам PostgreSQL.

## Конфигурация

| Переменная | По умолчанию / назначение |
|---|---|
| DATABASE_URL | Обязательна. Можно отдельный login на ту же БД |
| DB_SCHEMA | `public`; одно quoted имя, без fallback в public для другой схемы |
| PLANET_HTTP_ADDR | `127.0.0.1:8081`; допустим `0.0.0.0:8081` для нужного развёртывания |
| PLANET_ALLOWED_ORIGINS | PUBLIC_ORIGIN, если задан, иначе localhost/127.0.0.1:8080; список явных http(s) origins через запятую, без `*`/paths/userinfo |
| PLANET_LOG_FILE | Не задан: JSON в stderr. Иначе append в указанный файл; папка уже существует и доступна роли ОС |
| PLANET_DB_TIMEOUT | `2s`, 100ms–30s; ниже lease renewal |
| PLANET_LEASE_TTL | `15s`, 3s–5m |
| PLANET_LEASE_RENEW | `5s`, не меньше 1s, не больше TTL/3 |
| PLANET_SHUTDOWN_TIMEOUT | `10s`, 1s–1m, общий срок HTTP drain/освобождения lease |

Секрет подключения не выводится в журнал. Переходы readiness логируются только при изменении причины, без бесконечного одинакового сообщения. Причины: starting/schema_missing/schema_incompatible/database_unavailable/lease_in_use/lease_lost/stopping. Readiness повторно проверяет marker и срок/fence в DB на каждом запросе; старого RAM-флага недостаточно.

В этом этапе нет проксирования/индикатора planet в игровом UI, настройки PLANET_PUBLIC_URL, CORS API с credentials, частот движения или открытого WebSocket. Origin allowlist применяется к имеющимся routes; это не авторизация будущих игроков. В будущем WS обязательно проверяет и cookie/handoff. Reverse proxy лучше оставить на одном origin `/planet/*`; космические routes обслуживает прежний процесс.

## Минимальная роль PostgreSQL

Создать отдельный login и задать пароль средствами оператора. Пример ниже использует схему `cosmosimulator`; при другом имени заменить его с правильным quoting. Выполнять оператором **после cosmic migration**, не от имени planet:

```sql
GRANT USAGE ON SCHEMA cosmosimulator TO cosmos_planet;
GRANT SELECT ON cosmosimulator.planet_schema_state,
                cosmosimulator.planet_runtime_leases TO cosmos_planet;
GRANT UPDATE (owner_id, fence, expires_at)
  ON cosmosimulator.planet_runtime_leases TO cosmos_planet;
```

Роль должна иметь CONNECT к существующей БД. Не выдавать CREATE на схему, права изменения marker/DDL, world, ships, users и финансов. В P2-1 INSERT в lease не нужен: singleton создаёт cosmic runner. Новые права на будущие runtime-таблицы будут вводиться вместе с соответствующими этапами.

Проверен запуск под ограниченной NOLOGIN role через SET ROLE в изолированной тестовой БД: SELECT/lease UPDATE разрешены; UPDATE world, изменение marker и CREATE TABLE запрещены. Это проверка полномочий, не выдача готового пароля или проверка внешнего сетевого login. При общем администраторском DATABASE_URL DB-права не изолированы, хотя код соблюдает границы.

## Служба Windows

Копировать EXE в постоянный путь. В административном PowerShell:

```powershell
New-Item -ItemType Directory -Force C:\Astra Journey\logs
sc.exe create AstraJourneyPlanet binPath= 'C:\Astra Journey\cosmosimulator-planet.exe' start= auto
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\AstraJourneyPlanet' -Name Environment -PropertyType MultiString -Value @('DATABASE_URL=postgres://cosmos_planet:URL_ENCODED_PASSWORD@127.0.0.1:5432/existing_database?sslmode=disable','DB_SCHEMA=cosmosimulator','PLANET_HTTP_ADDR=127.0.0.1:8081','PLANET_LOG_FILE=C:\Astra Journey\logs\planet.log') -Force
sc.exe start AstraJourneyPlanet
sc.exe query AstraJourneyPlanet
```

Значения окружения службы отличаются от окна PowerShell; `.env` автоматически не читается. Права на каталог/лог и секреты реестра ограничивает оператор. Имя службы отдельно от `Astra Journey`; остановка одной не останавливает другую. SCM Running означает, что HTTP listener создан, а состояние DB/schema/lease проверяется health/ready. Нет обязательной зависимости SCM от cosmic: planet может дождаться миграции в состоянии 503.

При stop сначала выключается readiness и отменяется heartbeat, затем HTTP drain и условное освобождение lease. При недоступной DB освобождение не объявляется успешным: блокировка истечёт сама. Нативный SCM не проверен в Linux; обе Windows x64 команды собираются отдельно.

## Проверки и дальнейшие этапы

[Отчёт P2-1](REPORT.ru.md). P2-2 добавит связанные территории и площадки. Пока нет worker размещения, маркеров новых площадок, изменений corporate rights, персонажей или миграции старых stub visits. Поведение первой очереди сохранено.
