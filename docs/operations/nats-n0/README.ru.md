# NATS N0 — подготовка существующего cosmoNATS

Дата: 2026-11-09. Исследованный main: `13cae438028886d4fc3ee3c3dfb239876496cbb6`.
Cosmic 0.24.6, Planet/Chat 0.24.4. Это эксплуатационная подготовка N0,
**не внедрение NATS в игровые службы**. Архитектурная база:
[01–08 и индекс](../../ideas/nats-event-bus/README.md).

## Результат и границы проверки

Из этой облачной среды нет настроенного VPN/доступа к Windows-машине владельца.
Подключение к `192.168.111.44:4222/8222` **не выполнялось**. Текущая конфигурация
ниже известна со слов владельца, а не прочитана с его диска. Пароли не получены.
Версия рабочего брокера, Windows Firewall, фактическая служба, сертификаты,
сохранность рабочего store и резервные копии пока не подтверждены.

Для локальных результатов см. [TEST-RESULTS.ru.md](TEST-RESULTS.ru.md).
Тестовый процесс создаётся на loopback, с временными портами, ключами, паролями
и JetStream store; он не использует рабочие 4222/8222 или диск `D:` владельца.

Игровые Go-файлы, go.mod/go.sum, SQL, активные TOML, HTTP/WS, службы Windows,
рабочий NATS, его данные и настройки **не изменены**. Новых игровых EXE и
релиза игры для N0 нет. N1/N2/N3 и пилоты не запускались.

## Что уже есть и что требуется

| Предмет | Известно сейчас | Следующее действие |
| --- | --- | --- |
| NATS | cosmoNATS уже установлен | Узнать реальную версию и способ запуска |
| Клиентский адрес | `192.168.111.44:4222`, без TLS | Проверить доступность; подготовлен TLS-проект |
| Auth | пользователи `cosmos`, `planet`, `chat`, plaintext auth transport | Сменить секреты, хранить bcrypt на сервере, password files у клиентов |
| ACL | сейчас отсутствуют | Подготовлены точные отдельные списки publish/subscribe |
| JetStream | включён, `D:/cosmosimulator/nats/data` | Подтвердить `/jsz`, лимиты, права папки, backup/restore |
| Лимиты | 512 MB RAM / 10 GB file | Сохранены без увеличения |
| Monitoring | `192.168.111.44:8222` | Проект переноса на `127.0.0.1:8222`, без отключения |
| Игровые службы | NATS-клиента в коде нет | На N0 ничего к брокеру не подключать |

## Поправки к проектным примерам архитектора

1. NATS username Cosmic — **`cosmos`**, не `cosmic`. При этом имя домена в
   subjects **остаётся** `cosmos.v1.signal.cosmic.*`, а envelope source —
   `cosmic` по документу 03. Имя login не переименовывает контракт событий.
2. `max_memory_store: 512MB`, не проектные 2GB; `max_file_store: 10GB` сохранён.
3. Файл подключения — **`include ./auth.config`**, не `auth.conf`.
4. Сохранены server_name, путь существующего JetStream и логирование
   `server.log`, ротация 50MB/5 файлов. Store не переносится и не пересоздаётся.
5. Списки ACL сужены до полных subjects из каталога 03, без разрешений на
   произвольные новые subjects домена. Chat также явно читает предложенные
   `planet.presence.stale` и `cosmic.transfer.return.completed`; это права
   транспорта, а не разрешение реализовывать эти необязательные события на N0.
6. Архитектурное `cosmos.v1.signal.*` — обозначение семейства. В синтаксисе
   NATS `*` покрывает ровно один токен: такой literal wildcard не покрывает
   `cosmos.v1.signal.planet.sector.changed`. В ACL используются полные имена.
7. Обычные static users/password, **без `.creds`**, JWT/NKey и нового аккаунта JS.

## Подготовленные материалы

- [SECURITY-ROLLOUT.ru.md](SECURITY-ROLLOUT.ru.md) — TLS, bcrypt/password files,
  monitoring, безопасная смена конфигурации и backup/rollback.
- [WINDOWS-CHECKS.ru.md](WINDOWS-CHECKS.ru.md) — точные команды read-only проверки
  рабочего сервера, сети, Windows-службы и firewall.
- [TEST-MATRIX.ru.md](TEST-MATRIX.ru.md) — разрешённые и запрещённые действия,
  воспроизводимый изолированный тест.
- [auth.config.example](templates/auth.config.example) — три существующих login
  с bcrypt placeholders и точными ACL.
- [server-tls.config.example](templates/server-tls.config.example) — проект
  поправки существующего server config, не новая основная установка.
- [server-cert.cnf.example](templates/server-cert.cnf.example) — SAN для IP.
- [cosmos](templates/cosmos.future.toml), [planet](templates/planet.future.toml),
  [chat](templates/chat.future.toml) — будущие `[nats]`, пока disabled.
- [scripts](scripts) — read-only Windows inventory/login, защищённая подготовка
  password file и изолированная проверка ACL/TLS/JetStream/restart.

**Будущие TOML нельзя сейчас использовать как CONFIG_FILE или сливать с
активными конфигами.** Текущий strict decoder отвергает `[nats]` даже при
`enabled=false`. В N0 он намеренно не изменён. Адреса HTTP/WS, PostgreSQL,
Origin и `CHAT_ERASURE_JOURNAL` остаются прежними. Данные о доступе к игре
не передаются через брокер, PostgreSQL остаётся источником истины.

## Допуск к N1

Локальная проверка проекта не является приёмкой рабочего сервера. Для закрытия
эксплуатационного N0 нужны результаты Windows inventory, подтверждённые TLS/SAN,
права и ротация секретов, ограничения monitoring/firewall, реальный порядок
backup/restore и версия установленного NATS. Применение подготовленных изменений
к рабочему серверу требует **отдельного разрешения владельца**.

N1 (общий Chat Dispatcher, A1 без брокера) можно обсуждать отдельно после
рассмотрения этого отчёта; разрешение на его разработку из N0 не следует.
Клиент NATS и включение `[nats]` в игровые службы относятся к последующим
согласованным этапам. Эффект на производительность и игровой fallback на N0
не измерялись и не заявляются.
