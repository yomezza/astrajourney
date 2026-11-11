# Linux amd64 · Debian 13

Начиная с 0.24.11 доступны Cosmic, Planet и Chat для Linux amd64. Бинарники
собраны с `GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0`: статические ELF
x86-64, без динамического загрузчика и зависимости от версии glibc.

Для первого выпуска Linux использован точный тег **v0.24.11**, commit
`15447049599e17477a2553c7c517543f64226c77`. Windows EXE взяты из существующего
релиза без пересборки и без повторной загрузки поверх них. Последующие изменения
main не включались в Linux 0.24.11: обе платформы относятся к одному выпуску.

В [релизе 0.24.11](https://github.com/yomezza/cosmosimulator/releases/tag/v0.24.11)
доступны три файла без расширения:

- `cosmosimulator-v0.24.11-linux-amd64`
- `cosmosimulator-planet-v0.24.11-linux-amd64`
- `cosmosimulator-chat-v0.24.11-linux-amd64`

В downloads сохраняются все версии. `SHA256SUMS` включает Windows, Linux и
`cosmosimulator-v0.24.11-build.json` с исходным commit, компилятором и параметрами.
Суммы manifest записаны относительно корня репозитория (`downloads/...`).

## Запуск

После скачивания выдать право исполнения. Например, из каталога с файлами:

```sh
chmod +x cosmosimulator-v0.24.11-linux-amd64
chmod +x cosmosimulator-planet-v0.24.11-linux-amd64
chmod +x cosmosimulator-chat-v0.24.11-linux-amd64
```

Каждой службе нужен собственный TOML. Пример запуска Cosmic в текущем терминале:

```sh
CONFIG_FILE=/etc/cosmosimulator/cosmos.toml ./cosmosimulator-v0.24.11-linux-amd64
```

Для Planet использовать `planet.toml` и её бинарник, для Chat — `chat.toml` и её
бинарник, в отдельных процессах. Без CONFIG_FILE применяются только ENV, как на
Windows. Правила PostgreSQL, DB_SCHEMA, HTTP адресов, origins и NATS не менялись.
Для фона можно использовать systemd; процессы штатно обрабатывают SIGTERM.

При переносе настроек Windows заменить пути вида `D:/...` на Linux paths.
Пример Chat: `erasure_journal='/var/lib/cosmosimulator/chat-erasure.log'`;
каталог должен существовать и быть доступен на запись аккаунту Chat. Для NATS
используются отдельные password_file, читаемые только нужным аккаунтом (0400/0600),
и доверенный ca_file. Относительные пути TOML считаются от CONFIG_FILE.
NATS по умолчанию отключён; перенос на Linux не включает его автоматически.

Проверены ELF amd64, отсутствие динамического loader и выполнение всех трёх
бинарников на Debian 13.6 x86_64 до штатной проверки конфигурации. Повторный
полный игровой/БД прогон не проводился: игровые исходники релиза не менялись.
Установка и запуск на сервере владельца не выполнялись.

## Дальнейшие релизы: обе платформы

Из корня проекта с установленным Go версии go.mod или новее:

```sh
python3 scripts/build-release.py v0.24.12
```

По умолчанию собираются все три службы для **Windows amd64 и Linux amd64**,
в downloads добавляются бинарники, build.json и SHA256. Предварительно обновить
Version изменяемых служб. Если меняется одна служба, например только Chat:

```sh
python3 scripts/build-release.py v0.24.12 --service chat
```

Дополнение Linux к существующему тегу без пересборки Windows:

```sh
python3 scripts/build-release.py v0.24.11 --platform linux --source-ref v0.24.11
```

Последняя команда уже выполнена для 0.24.11; повтор откажется затирать готовые
файлы. Скрипт сохраняет прежние версии, проверяет соответствие Version исходников
номеру выпуска и записывает параметры сборки. Для двух платформ обычного нового
релиза используйте один запуск, чтобы обе собирались из одного состояния кода.

Tag workflow требует пары Windows/Linux для каждой обновляемой службы и
публикует обе. Частичный Windows-only релиз не пройдёт проверку. Backfill workflow
дополняет существующий релиз только Linux/build.json/SHA256SUMS, предварительно
проверяет исходный тег и прежние опубликованные Windows checksums. Windows assets
при этом остаются на месте; исходный tag не перемещается.
