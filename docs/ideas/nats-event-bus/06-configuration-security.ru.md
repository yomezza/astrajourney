# NATS Event Bus — 06. Сервер, TLS, авторизация, TOML, Windows

Дата: 2026-11-09. **Только проектная конфигурация**, не применялась к работающему серверу. Задано владельцем: server_name cosmoNATS; listen 192.168.111.44:4222; HTTP monitoring 192.168.111.44:8222; store_dir D:/cosmosimulator/nats/data; max_memory_store 2GB, max_file_store 10GB.

## 1. Выбранная схема идентификации

**Проект:** централизованная авторизация NATS Server с отдельными static user/password для Cosmic, Planet и Chat, subject-level ACL, TLS для клиентов; bcrypt hash для серверного config и защищённые password files у Windows-служб. Простота предпочтительнее немедленной инфраструктуры operator/JWT/NKey для трёх собственных служб.

- Формат .creds **не использовать**: он предназначен для JWT + NKey seed, а не обычного user/password.
- Cosmic, Planet и Chat имеют разные сильные секреты; не передавать в URL, логах, CLI-аргументах или Git. Сервер хранит bcrypt hash, клиент — plaintext password в защищённом локальном файле, доступном только учётной записи конкретной службы и администраторам.
- Серверный TLS сертификат должен содержать IP 192.168.111.44 в SAN или DNS имя, которым пользуется клиент; Go NATS client проверяет CA и server identity. Запрещён insecure skip verification. TLS не является автоматически mTLS: аутентификация клиента здесь user/password.
- Windows Firewall 4222 разрешён только доверенным компьютерам/процессам. Monitoring 8222 должен быть доступен только администратору, не публично в LAN.
- Один сервер NATS/JetStream не имеет резервной реплики и не даёт высокой доступности.

Первоисточники: https://docs.nats.io/learn/security/authentication-basics ; https://docs.nats.io/learn/security/authorization ; https://docs.nats.io/learn/security/encryption .

## 2. Проект NATS server.conf

Настройка **не применялась**. Сертификаты/приватные файлы и несуществующие реальные секреты требуется создать отдельно.

    server_name: "cosmoNATS"
    listen: "192.168.111.44:4222"
    http: "127.0.0.1:8222"

    tls {
        cert_file: "D:/cosmosimulator/nats/certs/server-cert.pem"
        key_file:  "D:/cosmosimulator/nats/certs/server-key.pem"
        ca_file:   "D:/cosmosimulator/nats/certs/ca.pem"
        timeout: 2
    }

    jetstream {
        store_dir: "D:/cosmosimulator/nats/data"
        max_memory_store: 2GB
        max_file_store: 10GB
    }

    include ./auth.conf

Директива NATS include используется **без** равенства или двоеточия; относительный файл определяется относительно основного config. Для Windows-путей предпочтительны прямые слеши. Прежний мониторинг 192.168.111.44:8222 предложено переместить на loopback; он не изменён. При необходимости смотреть мониторинг с другого ПК использовать защищённый туннель или authenticated proxy.

Пока нельзя менять запущенный сервер без резервной копии действующей конфигурации и контрольного TLS/authorization smoke test.

## 3. Только шаблон приватного auth.conf

Ни один placeholder не должен использоваться как настоящий пароль или хеш. Реальный auth.conf не публиковать в Git.

    authorization {
      users: [
        {
          user: "cosmic"
          password: "<BCRYPT_HASH_COSMIC>"
          permissions: {
            publish: { allow: ["cosmos.v1.signal.cosmic.>"] }
            subscribe: { allow: ["cosmos.v1.signal.chat.company.cleanup.completed"] }
          }
        }
        {
          user: "planet"
          password: "<BCRYPT_HASH_PLANET>"
          permissions: {
            publish: { allow: ["cosmos.v1.signal.planet.>"] }
            subscribe: { deny: [">"] }
          }
        }
        {
          user: "chat"
          password: "<BCRYPT_HASH_CHAT>"
          permissions: {
            publish: { allow: [
              "cosmos.v1.signal.chat.outbox.changed",
              "cosmos.v1.signal.chat.company.cleanup.completed"
            ] }
            subscribe: { allow: [
              "cosmos.v1.signal.chat.outbox.changed",
              "cosmos.v1.signal.cosmic.company.>",
              "cosmos.v1.signal.cosmic.moderator.changed",
              "cosmos.v1.signal.planet.visit.>",
              "cosmos.v1.signal.planet.sector.changed"
            ] }
          }
        }
      ]
    }

Блок publish/subscribe для Planet специально ограничен только отправкой. Для NATS deny [">"] означает не позволять подписываться. Не указывать wildcard > на все subjects. Пользователям не давать права JetStream API ($JS.API.>) на пилотах.

Сильные пароли генерировать безопасно интерактивной утилитой NATS; bcrypt server hashes хранить в ограниченном auth.conf, у процессов plaintext в отдельных ACL-защищённых файлах. Нельзя передавать пароль через аргумент командной строки: он может попасть в history/process inspection. При необходимости вместо статических логинов позднее можно перейти на JWT/NKey, но это отдельное проектное решение, не смешивать форматы.

**Тестирование авторизации перед внедрением**: неавторизованный клиент отвергнут; Cosmic не может публиковать от Planet, Chat не может публиковать от Cosmic, Planet не может читать Private/chats, любой пользователь не может обращаться к $JS.API.> и чужим subjects; разрешённые тестовые publishes/subscribes проходят.

## 4. Предложенная новая секция [nats] в каждом TOML

**Сейчас этих полей нет в Cosmosimulator v0.24.4.** Строгий TOML decoder отвергнет unknown fields до того, как разработчик добавит поддержку. Примеры — только для архитектуры.

### cosmos.toml (добавить к существующему файлу)

    [nats]
    enabled = true
    service_identity = 'cosmos'
    servers = ['tls://192.168.111.44:4222']
    username = 'cosmic'
    password_file = 'D:/cosmosimulator/nats/secrets/cosmic.password'
    ca_file = 'D:/cosmosimulator/nats/certs/ca.pem'
    connect_timeout = '2s'
    reconnect_wait = '1s'

### planet.toml

    [nats]
    enabled = true
    service_identity = 'planet'
    servers = ['tls://192.168.111.44:4222']
    username = 'planet'
    password_file = 'D:/cosmosimulator/nats/secrets/planet.password'
    ca_file = 'D:/cosmosimulator/nats/certs/ca.pem'
    connect_timeout = '2s'
    reconnect_wait = '1s'

### chat.toml

    # Добавить поля в СУЩЕСТВУЮЩИЙ раздел [chat], не дублировать блок
    [chat]
    dispatcher_mode = 'b1'
    fallback_poll_interval = '10s'
    degraded_poll_interval = '2s'
    outbox_overlap = '60s'

    [nats]
    enabled = true
    service_identity = 'chat'
    servers = ['tls://192.168.111.44:4222']
    username = 'chat'
    password_file = 'D:/cosmosimulator/nats/secrets/chat.password'
    ca_file = 'D:/cosmosimulator/nats/certs/ca.pem'
    connect_timeout = '2s'
    reconnect_wait = '1s'

Этот chat TOML — **фрагмент для слияния с текущим файлом**, а не полный рабочий пример: не удалять database.url, chat.erasure_journal, origins, HTTP URLs.

## 5. Формальные свойства будущего parser

| Ключ | Default | Валидация |
|---|---|---|
| nats.enabled | false | Bool; при false не подключаться и не читать secrets |
| nats.service_identity | пусто | При enabled cosmos/planet/chat; обязательно сравнивать с реальным процессом, чтобы общий файл не заставил все службы использовать одну identity |
| nats.servers | [] | При enabled 1–3 tls://host:port, без username/password/path/query, TLS hostname/IP SAN |
| nats.username | пусто | При enabled непустой |
| nats.password_file | пусто | При enabled существующий файл, проверить ACL насколько возможно, не раскрывать содержимое в ошибках |
| nats.ca_file | пусто | При enabled валидный CA PEM |
| nats.connect_timeout | 2s | Например 100ms–10s |
| nats.reconnect_wait | 1s | Например 250ms–30s, jitter, bounded logs |
| chat.dispatcher_mode | legacy | legacy/a1/a2/b1; b1 требует nats.enabled=true |
| chat.fallback_poll_interval | 10s | Не выбирать 30s без доказанного срока; 1–30s |
| chat.degraded_poll_interval | 2s | <= normal poll, например 500ms–10s |
| chat.outbox_overlap | 60s | Достаточный срок относительно max tx duration + polling interval + jitter |

Обязательные NATS параметры проверять на этапе конфигурации. Временная недоступность корректно сконфигурированного NATS в B1 вызывает degraded polling, **не** ошибку запуска и не 503 игрового чата. Недоступный парольный файл, неверные параметры TLS, неожиданный пустой username — ошибка конфигурации с безопасным сообщением.

Относительные password_file/ca_file в CONFIG_FILE-режиме предлагается разрешать относительно директории TOML; в ENV-режиме лучше требовать абсолютные пути, особенно для служб Windows SCM.

## 6. CONFIG_FILE и переменные окружения

Существующее правило сохраняется **без изменений**:

- CONFIG_FILE не задан → только переменные окружения.
- CONFIG_FILE задан → только TOML; значения из ENV не подмешиваются. Новое поле, которого нет в старом TOML, получает default.
- Старые TOML v0.24.4 без секции [nats] → enabled=false, dispatcher_mode=legacy, работоспособность прежних служб.
- Для нового ENV-режима предложены: NATS_ENABLED, NATS_SERVICE_IDENTITY, NATS_SERVERS (список через запятую), NATS_USERNAME, NATS_PASSWORD_FILE, NATS_CA_FILE, NATS_CONNECT_TIMEOUT, NATS_RECONNECT_WAIT; для Chat — CHAT_DISPATCHER_MODE, CHAT_FALLBACK_POLL_INTERVAL, CHAT_DEGRADED_POLL_INTERVAL, CHAT_OUTBOX_OVERLAP.
- Изменение CONFIG_FILE требует рестарта конкретного процесса, без горячей подмены security settings на первом этапе.
- **Один общий TOML с общей [nats] для трёх процессов непригоден**, поскольку identity и credential разные. Для NATS-пилотов — отдельные cosmos.toml, planet.toml, chat.toml. Общий файл со disabled NATS по-прежнему работает; поддержка дополнительных [nats.cosmos]/[nats.planet]/[nats.chat] — отдельное будущее улучшение.
- Не удалять HTTP origin, proxy, chat Cosmic/Planet readiness URL, PostgreSQL URL, erasure journal.

Новые поля нужно добавить в internal/platform/configuration/document.go и values map, проверки — в конфигурацию соответствующей службы, включая strict TOML unknown-field test. На этапе III код не менять.

## 7. Windows Service, отказ и наблюдаемость

- NATS Server — отдельный Windows Service с явной командой запуска и путём -c к основному conf через выбранную ОС-совместимую обёртку (например WinSW/NSSM) или официально поддерживаемую установку проверенной версии; права к TLS keys/auth/JetStream только учётной записи службы.
- Запуск Cosmic/Planet/Chat **не должен иметь обязательную SCM dependency на NATS**. Иначе отключённый NATS заблокирует старт игры вопреки принятому решению.
- При startup/reconnect логировать только код ошибки, состояние транспорта, hostname без userinfo, попытки reconnect и дату истечения сертификата; не пароль, не cookie/DSN.
- Проверять локально HTTP monitoring /varz, /connz, /jsz на 127.0.0.1:8222. Доступ из LAN к 8222 — отдельный protected endpoint, не считать его безопасным из-за read-only.
- Метрики: connected/disconnected, reconnect_count, send_error, subscription_error, slow_consumers, queue overflow, fallback poll interval, NATS conn count, JetStream storage/use и uptime. Broker health = отдельный статус, **не** подмена Cosmic/Planet readiness.

Официально: https://docs.nats.io/reference/config/ ; https://docs.nats.io/learn/monitoring/monitoring-endpoints .