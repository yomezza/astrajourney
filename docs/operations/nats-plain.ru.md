# NATS без TLS · 0.24.12

Cosmic, Planet и Chat поддерживают обычный NATS с авторизацией user/password.
Рабочий cosmoNATS и его auth/config из облака не изменялись. Брокер должен сам
предоставлять обычный NATS listener на выбранном порту и знать эти аккаунты.

## Один CONFIG_FILE для всех служб

Полный пример: [configs/shared.example.toml](../../configs/shared.example.toml).
Подставить PostgreSQL, адрес игры и три NATS-пароля; создать каталоги логов и
сохранить существующий chat-erasure.log при переносе. Для Windows заменить
пути /var/lib/... на свои Windows paths в TOML literal строках.

Для включения NATS у Chat установить `[chat] dispatcher_mode='b1'`, затем:

```toml
[nats]
enabled = true
servers = ['nats://127.0.0.1:4222']
connect_timeout = '2s'
reconnect_wait = '1s'

[nats.cosmos]
username = 'cosmos'
password = 'YOUR_COSMOS_NATS_PASSWORD'

[nats.planet]
username = 'planet'
password = 'YOUR_PLANET_NATS_PASSWORD'

[nats.chat]
username = 'chat'
password = 'YOUR_CHAT_NATS_PASSWORD'
```

Всем трём процессам задать CONFIG_FILE с одним абсолютным путём. Если NATS
на другом компьютере — заменить loopback на его адрес. Перезапустить службы
после изменения конфигурации: горячая смена auth не добавлялась.

Общие параметры наследуются из `[nats]`, выбранный профиль может переопределить
servers/enabled/timeouts/ca_file. Identity автоматически соответствует процессу.
Логины остаются раздельными `cosmos`, `planet`, `chat`; exact PUB/SUB ACL из
[отчёта N6](nats-n6/README.ru.md) не менялись. При профилях учётные данные нельзя
задавать одновременно в корневой секции — это исключает их случайное смешивание.

## Пароль, ENV и TLS

- Ровно один источник: `password` **либо** `password_file`. Оба одновременно — ошибка.
- ENV: NATS_ENABLED=true, NATS_SERVERS=nats://host:4222, NATS_USERNAME и
  NATS_PASSWORD. NATS_SERVICE_IDENTITY можно опустить, она берётся из username.
- CONFIG_FILE по-прежнему исключает подмешивание ENV, включая пароли.
- Старый плоский `[nats]` для отдельной службы сохранён. TLS: servers с `tls://`,
  существующий ca_file и password_file либо password. CA/SAN проверки сохранены.
- В одном списке серверов нельзя смешивать nats:// и tls://. Plain профиль не
  использует ca_file; удалите его при переключении с TLS на обычный listener.
- Пароль не помещается в URL. Неверные настройки не печатают его в ошибке;
  диагностика транспорта использует прежние фиксированные error codes.
- Без TLS содержимое соединения не шифруется. TOML с паролями не хранить в Git;
  на Linux можно ограничить чтение конфигурационного файла через chmod 600.

При недоступности брокера остаются прежние SQL polling/recovery и игровые
транзакции. Переход назад на TLS требует соответствующего listener, CA и URL;
отключение NATS — enabled=false и Chat A1/legacy. SQL DOWN/новых GRANT нет.

## Проверки

Изолированный NATS Server 2.15.2 с обычным listener без TLS, случайные три пароля
и прежние exact subjects. Проверены auth success/failure, Chat outbox hints,
переподключение, недопустимые/смешанные профили, отсутствие fallback TLS→plain,
старые TLS/CA/SAN/file проверки, strict TOML и исключение ENV.

Отдельный native integration запускает Cosmic, Planet и Chat с `-race` в трёх
OS processes, **с одним и тем же CONFIG_FILE**, настоящими PostgreSQL и plain NATS.
Chat использует restricted SQL role. Проверяются регистрации трёх identities,
Cosmic membership → Chat sync, Planet visit signals → актуальный SQL spatial scope,
работа SQL при остановке NATS и доставка после его рестарта. Рабочие Windows/LAN
службы не использовались. Нагрузочных тестов и profiling не проводилось.

Итог: unit/config/transport с `-race` — PASS; native shared-config сценарий —
PASS (17.73s); `go test ./...` без DB/NATS ENV — PASS для compile/unit (DB cases
там пропускаются); `go vet` — PASS. Все шесть Windows/Linux amd64 бинарников
собраны `scripts/build-release.py`, PE/ELF и статическая Linux-линковка проверены.
SHA256 и параметры сборки записаны в downloads. Windows runtime отдельно не запускался.
В общей проверке исправлен прежний SVG-тест: статическая заглушка object-unknown
не относится к семнадцати анимированным голограммам. Сами рисунки не менялись.
