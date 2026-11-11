# Планетарный режим · установка и эксплуатация 0.23.0

Текущий выпуск объединяет космический терминал и отдельную многопользовательскую планетарную службу. Есть колонии/территории/площадки, высадка, постоянный персонаж, движение мышью, присутствие игроков, подтверждаемые переходы, восстановление и графика из предоставленного пака. Добыча персонажем, его рюкзак и строительство относятся к будущей очереди.

Службы используют **одну существующую PostgreSQL и одинаковый DB_SCHEMA**. Разные логины допустимы и рекомендуются для разделения прав. Cosmic управляет миром и миграциями; planet не создаёт базу/схему, не генерирует новую глобальную поверхность и не запускает мировой tick.

- [Схема, владельцы данных и полный GRANT](DATABASE.ru.md).
- [Актуальные HTTP/WS-контракты](API.ru.md).
- [Результаты 50-клиентского эксперимента](../P2-9/REPORT.ru.md).
- [Ограничения и задания по графике](../P2-8/ART.ru.md).

## Обновление

1. Остановить planet, затем cosmic. Сделать согласованную резервную копию игровой схемы, включая seed, geometry snapshots, characters, visits, receipts и финансовые таблицы.
2. Заменить **обе** Windows x64 сборки на 0.23.0. SHA256 доступны в `downloads/SHA256SUMS` и в GitHub Release; старые файлы не удаляются.
3. Запустить cosmic. Он применяет необходимые аддитивные миграции старых версий и выполняет устойчивый backfill площадок. Текущий marker — **7**; в 0.23.0 новой миграции нет.
4. Для отдельного planet login выдать актуальные права. Если они уже настроены для 0.21/0.22, новое право нужно только для административных метрик:

```sql
GRANT SELECT(role) ON cosmosimulator.users TO cosmos_planet;
```

5. Запустить planet. В административном меню «Статус служб» обе версии — 0.23.0, planet — «Готова». Readiness должен сообщать `stage=P2-9`, `schema_version=7`, `gameplay_ready=true`.
6. Перезагрузить браузер. Не удалять сохранённые поверхности/локации для обновления внешнего вида.

Для отката с 0.23 к согласованной паре 0.22/0.21 остановить обе службы, вернуть обе сборки и перезагрузить страницу: схема/формат сцены не менялись. Для отката к более старым marker использовать соответствующую резервную копию; вручную понижать marker нельзя.

## Консольный запуск Windows

В первом окне PowerShell, с корректно закодированным в URL паролем:

```powershell
$env:DATABASE_URL = 'postgres://cosmos:URL_ENCODED_PASSWORD@127.0.0.1:5432/existing_database?sslmode=disable'
$env:DB_SCHEMA = 'astra_journey'
$env:HTTP_ADDR = '127.0.0.1:8080'
$env:PLANET_ENABLED = 'true'
$env:PLANET_INTERNAL_URL = 'http://127.0.0.1:8081'
.\downloads\astra-journey-v0.24.13-windows-amd64.exe
```

Во втором окне:

```powershell
$env:DATABASE_URL = 'postgres://cosmos_planet:URL_ENCODED_PASSWORD@127.0.0.1:5432/existing_database?sslmode=disable'
$env:DB_SCHEMA = 'astra_journey'
$env:PLANET_HTTP_ADDR = '127.0.0.1:8081'
$env:PLANET_ALLOWED_ORIGINS = 'http://127.0.0.1:8080,http://localhost:8080'
.\downloads\astra-journey-planet-v0.24.13-windows-amd64.exe
```

```powershell
Invoke-RestMethod http://127.0.0.1:8081/planet/api/v1/health/live
Invoke-RestMethod http://127.0.0.1:8081/planet/api/v1/health/ready
```

Для входа с планшета/другой машины cosmic может слушать `HTTP_ADDR=0.0.0.0:8080`; адрес браузера, например `http://192.168.1.10:8080`, добавить в `PLANET_ALLOWED_ORIGINS`. Порт planet можно оставить на loopback: браузер использует cosmic gateway. При публичном доступе настроить HTTPS reverse proxy, WebSocket Upgrade, точный `PUBLIC_ORIGIN`, `COOKIE_SECURE=true` и доверенные адреса proxy. `*` в Origin не используется.

## Параметры

Оба EXE читают окружение процесса; `.env` автоматически не загружается. Для первоначального создания cosmic admin нужны оба `ADMIN_USERNAME`/`ADMIN_PASSWORD`; затем убрать их. Planet эти значения не использует.

| Параметр | Значение по умолчанию / смысл |
|---|---|
| DATABASE_URL | Обязательная строка подключения; для удалённой БД — проверяемый TLS |
| DB_SCHEMA | `public`; одно имя схемы, одинаковое у служб |
| HTTP_ADDR | Cosmic `127.0.0.1:8080` |
| PLANET_ENABLED | На cosmic: выключено, включить `true` для gateway/handoff |
| PLANET_INTERNAL_URL | На cosmic: `http://127.0.0.1:8081` |
| PUBLIC_ORIGIN / COOKIE_SECURE / TRUSTED_PROXIES | Настройки cosmic за HTTPS proxy |
| PLANET_HTTP_ADDR | Planet `127.0.0.1:8081` |
| PLANET_ALLOWED_ORIGINS | `PUBLIC_ORIGIN`, иначе два localhost/127.0.0.1 origin порта 8080; список через запятую |
| PLANET_DB_TIMEOUT | `2s`; 100ms–30s, ниже renewal |
| PLANET_LEASE_TTL / PLANET_LEASE_RENEW | `15s` / `5s`; TTL 3s–5m, renewal от 1s и не больше TTL/3 |
| PLANET_SHUTDOWN_TIMEOUT | `10s`; 1s–1m |
| PLANET_SCENE_CAPACITY | 32; 1–1024. Для перехода нужны одновременно исходная и соседняя сцены |
| PLANET_SCENE_IDLE_TTL | `2m`; 1s–1h |
| PLANET_SCENE_LOAD_TIMEOUT | `20s`; 1s–1m |
| PLANET_MOVE_SPEED | 4 мировых единицы/с; 1–12 |
| PLANET_PATH_BUDGET | 24000; 1000–65536 |
| PLANET_LOG_FILE | Не задан: JSON stderr; иначе append в существующий доступный каталог |

Размер пула можно явно ограничить параметром `pool_max_conns` в каждом DATABASE_URL. Нагрузочный профиль проверял 8 соединений на процесс; значение подбирается по собственной PostgreSQL и общему лимиту соединений. Пароли, cookie, tickets и DSN не публиковать в отчётах.

## Две службы SCM

После проверки консольного запуска скопировать выбранные EXE в постоянные пути `C:\AstraJourney\astra-journey.exe` и `C:\AstraJourney\astra-journey-planet.exe`. В административном PowerShell:

```powershell
sc.exe create Astra Journey binPath= 'C:\AstraJourney\astra-journey.exe' start= auto
sc.exe create AstraJourneyPlanet binPath= 'C:\AstraJourney\astra-journey-planet.exe' start= auto
New-Item -ItemType Directory -Force C:\AstraJourney\logs
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\AstraJourneyPlanet' -Name Environment -PropertyType MultiString -Value @('DATABASE_URL=postgres://cosmos_planet:URL_ENCODED_PASSWORD@127.0.0.1:5432/existing_database?sslmode=disable','DB_SCHEMA=cosmosimulator','PLANET_HTTP_ADDR=127.0.0.1:8081','PLANET_ALLOWED_ORIGINS=http://127.0.0.1:8080,http://localhost:8080','PLANET_LOG_FILE=C:\AstraJourney\logs\planet.log') -Force
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\Astra Journey' -Name Environment -PropertyType MultiString -Value @('DATABASE_URL=postgres://cosmos:URL_ENCODED_PASSWORD@127.0.0.1:5432/existing_database?sslmode=disable','DB_SCHEMA=cosmosimulator','HTTP_ADDR=127.0.0.1:8080','PLANET_ENABLED=true','PLANET_INTERNAL_URL=http://127.0.0.1:8081') -Force
sc.exe start Astra Journey
sc.exe start AstraJourneyPlanet
sc.exe query AstraJourneyPlanet
```

Переменные текущего окна PowerShell не меняют окружение установленной службы. Доступ к секретам реестра, каталогам и портам ограничивает оператор. У уже установленных служб повторять `sc create` не нужно; остановить, заменить EXE и запустить. SCM Running подтверждает listener; готовность БД/схемы/lease проверяется отдельно по ready.

Нативный Windows SCM и производительность Windows в этой Linux-среде не испытывались. Обе сборки кросс-компилируются; двухпроцессные проверки и измерения выполнены на Linux.

## Восстановление и обслуживание

Закрытие вкладки сохраняет визит; «Колонии → Продолжить высадку» восстанавливает персонажа. Новая вкладка заменяет controller с повышением epoch. Возврат доступен из соседнего разрешённого сектора без собственной площадки и не перемещает закреплённый корабль.

После аварии planet запустить ту же версию с той же БД/схемой. Новая служба ждёт истечения lease и получает новый fence. Возможна потеря движения после последнего checkpoint: период 5.0–5.5s. Ни браузер, ни старый процесс не могут навязать новые координаты при resume.

При потере БД движение перестаёт иметь действующее разрешение; сервис не объявляет успешное сохранение. При недоступной planet cosmic продолжает работу; возврат может ждать безопасного отзыва/reservation после fencing. Не запускать две независимые активные planet-службы на одной схеме.

Для reset/delete сначала завершить живые/отключённые/legacy-визиты. Cosmic выставляет retiring, planet прекращает новые входы и очищает свои ссылки, после чего требуется новое подтверждение удаления. Прямое удаление SQL в рабочей схеме этот протокол обходит и не является процедурой обслуживания.
