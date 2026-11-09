# Конфигурация трёх служб · 0.24.4

Cosmic, Planet и Chat поддерживают два режима. Источник выбирается **при запуске
каждого процесса** одной переменной окружения `CONFIG_FILE`:

- Переменная не задана или пуста — прежние переменные окружения.
- Переменная содержит путь — настройки приложения берутся только из TOML.
  Значения `DATABASE_URL`, `CHAT_ENABLED` и других параметров в окружении в этом
  режиме не подмешиваются. Отсутствующие параметры используют встроенные
  значения по умолчанию; обязательные параметры должны быть заполнены в TOML.

Файл читается один раз. После изменения требуется перезапуск соответствующей
службы. Ошибочный путь, синтаксис, неизвестный ключ или неправильный тип значения
останавливают запуск. Ошибка TOML указывает строку и столбец без содержимого
строки, чтобы реквизиты не попадали в сообщение. Источник выводится в стартовый
лог без паролей и строк подключения.

## Быстрый запуск под Windows

Готовые шаблоны:

- [Cosmic](../configs/cosmos.example.toml) — БД, HTTP, шлюзы к Planet и Chat,
  параметры cookie/proxy и первоначального администратора.
- [Planet](../configs/planet.example.toml) — БД, HTTP, допустимые Origin,
  движение, таймауты, lease и кэш сцен.
- [Chat](../configs/chat.example.toml) — БД, HTTP, адреса Cosmic/Planet,
  допустимые Origin и журнал удаления сообщений.

Скопируйте шаблоны в `cosmos.toml`, `planet.toml`, `chat.toml`, заполните строки
подключения и адрес сайта. Пример рассчитан на три процесса на одном компьютере:
Cosmic слушает `0.0.0.0:8080`, внутренние службы — `127.0.0.1:8081/8082`.
Origin во всех файлах должен соответствовать адресу в браузере. `0.0.0.0` —
адрес слушателя, а не URL сайта и не адрес для подключения к другой службе.

В **трёх отдельных PowerShell**, с путями к вашей установке:

```powershell
# Cosmic: запускается первым, выполняет миграции общей схемы.
$env:CONFIG_FILE = 'D:\cosmosimulator\configs\cosmos.toml'
& 'D:\cosmosimulator\downloads\cosmosimulator-v0.24.4-windows-amd64.exe'
```

```powershell
# Planet
$env:CONFIG_FILE = 'D:\cosmosimulator\configs\planet.toml'
& 'D:\cosmosimulator\downloads\cosmosimulator-planet-v0.24.4-windows-amd64.exe'
```

```powershell
# Chat
$env:CONFIG_FILE = 'D:\cosmosimulator\configs\chat.toml'
& 'D:\cosmosimulator\downloads\cosmosimulator-chat-v0.24.4-windows-amd64.exe'
```

Для возврата к переменным окружения перед следующим запуском:

```powershell
Remove-Item Env:CONFIG_FILE -ErrorAction SilentlyContinue
```

У SCM-службы `CONFIG_FILE` должен быть в окружении **самой службы**.
Присвоение `$env:` в отдельной консоли не меняет среду существующей службы.
Задавайте абсолютный путь; относительный `CONFIG_FILE` считается от рабочего
каталога процесса, который у SCM может отличаться от каталога EXE.

Можно использовать один файл с секциями всех служб и одной `[database]`.
Каждый процесс выбирает свои параметры. Для отдельных учётных записей Windows и
PostgreSQL удобнее три файла: выдайте каждому процессу доступ только к его
конфигурации. Локальные `configs/*.toml`, кроме шаблонов, исключены из Git.

## Формат и пути

TOML сохраняется в UTF-8, с BOM или без него. Строки URL и Windows-пути удобно заключать в одинарные
кавычки: `'D:\cosmosimulator\data\chat-erasure.log'`. В таких строках обратные
слеши не являются escape-последовательностями. URL пишутся как `http://`, без
слешей перед двоеточием. Подстановка `${VARIABLE}` не выполняется.

Булевы значения: `true`/`false` без кавычек. Числа — без кавычек. Origin и
доверенные сети задаются массивами строк, например:

```toml
[cosmos]
cookie_secure = false
trusted_proxies = ['127.0.0.1/32', '::1/128']

[planet]
allowed_origins = ['http://192.168.111.44:8080']
move_speed = 4.0
scene_capacity = 32
db_timeout = '2s'
```

Длительности используют формат Go: `'100ms'`, `'2s'`, `'5m'`. Проверки диапазонов
сохраняются, включая согласованность lease, таймаутов и скорости движения.
Ноль и `false`, явно указанные в файле, не заменяются значениями из окружения.

Относительные `planet.log_file` и `chat.erasure_journal` считаются **от папки
TOML**, а не EXE. Каталоги создаются заранее. Для существующего чата укажите
существующий долговечный журнал удаления — не подменяйте его пустым файлом.
Поведение путей в режиме переменных окружения не меняется.

## Соответствие параметров

| TOML | Прежняя переменная окружения |
| --- | --- |
| `database.url` | `DATABASE_URL` |
| `database.schema` | `DB_SCHEMA` |
| `cosmos.http_addr` | `HTTP_ADDR` |
| `cosmos.public_origin` | `PUBLIC_ORIGIN` |
| `cosmos.cookie_secure` | `COOKIE_SECURE` |
| `cosmos.trusted_proxies` | `TRUSTED_PROXIES` |
| `cosmos.planet_enabled` | `PLANET_ENABLED` |
| `cosmos.planet_internal_url` | `PLANET_INTERNAL_URL` в Cosmic |
| `cosmos.chat_enabled` | `CHAT_ENABLED` |
| `cosmos.chat_internal_url` | `CHAT_INTERNAL_URL` |
| `admin.username` | `ADMIN_USERNAME` |
| `admin.password` | `ADMIN_PASSWORD` |
| `planet.http_addr` | `PLANET_HTTP_ADDR` |
| `planet.allowed_origins` | `PLANET_ALLOWED_ORIGINS` |
| `planet.log_file` | `PLANET_LOG_FILE` |
| `planet.move_speed` | `PLANET_MOVE_SPEED` |
| `planet.path_budget` | `PLANET_PATH_BUDGET` |
| `planet.db_timeout` | `PLANET_DB_TIMEOUT` |
| `planet.lease_ttl` | `PLANET_LEASE_TTL` |
| `planet.lease_renew` | `PLANET_LEASE_RENEW` |
| `planet.shutdown_timeout` | `PLANET_SHUTDOWN_TIMEOUT` |
| `planet.scene_capacity` | `PLANET_SCENE_CAPACITY` |
| `planet.scene_idle_ttl` | `PLANET_SCENE_IDLE_TTL` |
| `planet.scene_load_timeout` | `PLANET_SCENE_LOAD_TIMEOUT` |
| `chat.database_url` | `CHAT_DATABASE_URL` |
| `chat.http_addr` | `CHAT_HTTP_ADDR` |
| `chat.cosmic_internal_url` | `COSMIC_INTERNAL_URL` |
| `chat.planet_internal_url` | `PLANET_INTERNAL_URL` в Chat |
| `chat.allowed_origins` | `CHAT_ALLOWED_ORIGINS` |
| `chat.erasure_journal` | `CHAT_ERASURE_JOURNAL` |
| `chat.maintenance_database_url` | `CHAT_MAINTENANCE_DATABASE_URL` |
| `chat.runtime_role` | `CHAT_RUNTIME_ROLE` |
| `chat.definer_role` | `CHAT_DEFINER_ROLE` |

В Chat непустой `chat.database_url` имеет приоритет над `database.url` внутри
того же файла. Если `allowed_origins` не задан, Planet и Chat используют
`cosmos.public_origin`, а при его отсутствии — прежние localhost/loopback Origins.
В общем файле `cosmos.planet_internal_url` и `chat.planet_internal_url` независимы.

Команды `--maintain` и `--grant-runtime` тоже читают `CONFIG_FILE`. Для выдачи
прав используйте отдельный DBA-файл с `database.url`, `database.schema`,
`chat.runtime_role` и `chat.definer_role`; для обслуживания — отдельную
конфигурацию с реквизитами владельца секций. Не добавляйте DBA-реквизиты в
обычный runtime-файл. Протокол выдачи прав и сохранения журнала остаётся прежним:
[инструкция Chat](chat-service/README.ru.md).
