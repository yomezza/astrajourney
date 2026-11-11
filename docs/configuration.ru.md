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
$env:CONFIG_FILE = 'D:\AstraJourney\configs\cosmos.toml'
& 'D:\AstraJourney\downloads\astra-journey-v0.24.13-windows-amd64.exe'
```

```powershell
# Planet
$env:CONFIG_FILE = 'D:\AstraJourney\configs\planet.toml'
& 'D:\AstraJourney\downloads\astra-journey-planet-v0.24.13-windows-amd64.exe'
```

```powershell
# Chat
$env:CONFIG_FILE = 'D:\AstraJourney\configs\chat.toml'
& 'D:\AstraJourney\downloads\astra-journey-chat-v0.24.13-windows-amd64.exe'
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
кавычки: `'D:\AstraJourney\data\chat-erasure.log'`. В таких строках обратные
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
| `chat.dispatcher_mode` | `CHAT_DISPATCHER_MODE` (Chat 0.24.7+, `legacy`/`a1`, с Chat 0.24.8 также `a2`, с 0.24.9 `b1`) |
| `chat.fallback_poll_interval` | `CHAT_FALLBACK_POLL_INTERVAL` (default `10s`) |
| `chat.degraded_poll_interval` | `CHAT_DEGRADED_POLL_INTERVAL` (default `2s`) |
| `chat.outbox_overlap` | `CHAT_OUTBOX_OVERLAP` (default `60s`) |

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

## Chat Dispatcher N1, начиная с Chat 0.24.7

Режим по умолчанию — `legacy`. Для экспериментального A1 укажите
`chat.dispatcher_mode = 'a1'` в отдельном TOML Chat либо `CHAT_DISPATCHER_MODE=a1`
в режиме ENV. После изменения нужен перезапуск только Chat. A1 использует
локальные уведомления и PostgreSQL fallback; NATS в A1/A2 не подключается. С Chat 0.24.8 поддерживается A2 на изолированном стенде;
с Chat 0.24.9 добавлен B1 и секция `[nats]` только для Chat.

Нормальный poll: 1–30s; degraded: 500ms–10s, не больше normal; overlap: 60s–10m.
На N1 остаются отдельные security/scope проверки каждые 2s. Значения 10s/2s/60s
экспериментальные. Откат — `legacy` и перезапуск, без удаления данных.

**Старые EXE Cosmic/Planet не знают этих новых полей.** Если прежде использовался
общий файл трёх служб, для включения A1 сделайте отдельную копию для Chat;
в старом общем файле новые параметры не добавляйте. Режим CONFIG_FILE не
подмешивает ENV, поэтому переключение через ENV при выбранном TOML не сработает.
Миграций/повторной выдачи SQL-прав нет. [Полный отчёт N1](operations/nats-n1/README.ru.md).


### Chat N2: A2 с PostgreSQL, начиная с Chat 0.24.8

Для изолированного теста установите `dispatcher_mode = 'a2'` в существующем
разделе `[chat]` отдельного TOML Chat, либо `CHAT_DISPATCHER_MODE=a2` при
неустановленном `CONFIG_FILE`. Остальные параметры 10s/2s/60s сохраняются.
A2 использует LISTEN на отдельном физическом соединении с прежними DB_SCHEMA
и SQL-правами; требует session pooling/direct PostgreSQL (transaction pooling
для LISTEN не подходит). Локальные post-COMMIT wakeups в этом режиме отключены.

A1/A2 независимо от outbox выполняют авторизованный state refresh каждые 30s.
Это защита текущего UI при поздних COMMIT за пределами overlap и retention,
а не гарантия доставки каждого hint. Никаких новых TOML/ENV-ключей для этого нет.

A2-триггер Chat не устанавливает; его внедрение через утверждённый механизм
миграций требует отдельного разрешения. При отсутствии/потере NOTIFY остаётся
polling. Откат: выбрать `a1` либо `legacy`, перезапустить Chat и лишь затем
согласовывать SQL DOWN. [Инструкция N2](operations/nats-n2/README.ru.md).


## Chat N3: опциональный Core NATS, Chat 0.24.9

`dispatcher_mode = 'b1'` требует `[nats] enabled=true` и полного TLS-профиля
пользователя `chat`. Режим по умолчанию — `legacy`. В A1/A2/legacy соединение
NATS не создаётся и секреты не читаются; сервер брокера им не требуется.

| TOML, секция `[nats]` | ENV | Значение / ограничение |
| --- | --- | --- |
| `enabled` | `NATS_ENABLED` | false по умолчанию; B1 требует true |
| `service_identity` | `NATS_SERVICE_IDENTITY` | только `chat` на N3 |
| `servers` | `NATS_SERVERS` | 1–3 `tls://host:port`; ENV через запятую, TOML array; URL без credentials/path/query |
| `username` | `NATS_USERNAME` | только `chat` на N3 |
| `password_file` | `NATS_PASSWORD_FILE` | защищённый обычный файл UTF-8, исходный пароль, не bcrypt hash и не `.creds` |
| `ca_file` | `NATS_CA_FILE` | существующий PEM CA bundle, действующие CA-сертификаты |
| `connect_timeout` | `NATS_CONNECT_TIMEOUT` | 2s default, 100ms–10s |
| `reconnect_wait` | `NATS_RECONNECT_WAIT` | 1s default, 250ms–30s; небольшой jitter |

Пути TOML разрешаются относительно CONFIG_FILE, ENV требует абсолютных путей.
Сохраняется исключительный выбор TOML/ENV. Неизвестные поля (включая password,
creds, JWT, NKey, insecure, JetStream) отвергаются strict decoder. Секция `[nats]` с 0.24.10 разрешена Chat и Planet; с 0.24.11 — также Cosmic,
всегда с отдельными профилями. Старые Planet EXE до 0.24.10 её не знают.

Неверные локальные CA/secret файлы — ошибка конфигурации до запуска Chat.
Удалённые TLS/auth/ACL ошибки классифицируются в логах отдельно от сети, Chat
продолжает SQL fallback. Пароли, payload и DSN в диагностике NATS не выводятся.
Файлы перечитываются после перезапуска Chat, горячей ротации клиента нет.

Реальный `192.168.111.44:4222` пока не подтверждён как TLS-ready: одного
изменения URL недостаточно. [Отчёт N3, безопасный стенд и Windows-подготовка](operations/nats-n3/README.ru.md).


## N5: отдельный профиль NATS Planet (0.24.10)

Planet поддерживает те же ключи `[nats]` / `NATS_*`, но `service_identity` и
`username` должны быть `planet`, а password_file — собственным. В Chat остаются
значения `chat`. TOML не смешивается с ENV, относительные пути считаются от
своего CONFIG_FILE. Старый Planet TOML без NATS сохраняет прежнее поведение.
Default — disabled; отключение не требует работающего брокера или secret files.

Сначала обновите Planet EXE, затем в отдельном согласованном стенде добавляйте
секцию в её **собственный** TOML. Старые EXE отвергают неизвестные поля.
На этапе N5 Cosmic не использовал `[nats]`; его поддержка добавлена в N6 ниже. Для отключения публикации
установите в Planet `enabled=false` и перезапустите только Planet.
Откат Chat на A1/legacy не требует остановки Planet и не меняет SQL данные.

[Отчёт N5 и точные будущие ACL](operations/nats-n5/README.ru.md).


## N6: три независимых профиля Core NATS (0.24.11)

Ключи и TLS/file ограничения из таблицы N3 одинаковы для всех служб.
`service_identity` **и** `username`: Cosmic — `cosmos`, Planet — `planet`,
Chat — `chat`. Source в envelope Cosmic при этом называется `cosmic`.
Пароли лежат в трёх разных защищённых password_file. Один общий TOML с
включённым NATS для трёх служб не подходит: каждый процесс выбирает свой
CONFIG_FILE либо собственные ENV. Смешивания источников нет.

Все новые профили по умолчанию disabled. В Cosmic/Planet `enabled=true`
включает фоновые Core hints; у Chat также нужен `dispatcher_mode='b1'`.
`legacy`, A1 и A2 не подключаются к NATS. Старые TOML без секции работают.
При ошибке локального CA/password/identity запуск прекращается с безопасной
ошибкой конфигурации. Удалённый брокер может быть недоступен: исправно
сконфигурированные службы стартуют и продолжают работу через SQL recovery.

Новые события не меняют HTTP адреса, gateway, WebSocket API и readiness.
Шаблоны в configs отключены; боевой NATS автоматически не настраивается.
Для текущих exact PUB/SUB ACL, функциональных проверок и rollback смотрите
[отчёт N6](operations/nats-n6/README.ru.md). Старому Cosmic 0.24.6 перед
возвратом нужно убрать даже disabled `[nats]` из его TOML: strict parser той
версии ещё не поддерживает секцию. Новому EXE для отката достаточно disabled.

## 0.24.12: обычный NATS и общий TOML

Поддержаны nats:// без TLS и `password` / NATS_PASSWORD вместо password_file.
Для общего файла используются `[nats.cosmos]`, `[nats.planet]`, `[nats.chat]`
с раздельными username/password; общие servers/enabled/timeouts — в `[nats]`.
Старые плоские TLS-конфиги сохранены. Один список servers использует одну схему.
[Настройка и проверки](operations/nats-plain.ru.md),
[полный общий пример](../configs/shared.example.toml).
