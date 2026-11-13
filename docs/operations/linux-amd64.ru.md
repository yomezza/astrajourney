# Astra Journey · Linux amd64 · Debian 13

Выпуск 0.24.13 содержит три статических ELF x86-64 для Debian 13:

- `astra-journey-v0.24.13-linux-amd64` — космический сервер;
- `astra-journey-planet-v0.24.13-linux-amd64` — планетарная служба;
- `astra-journey-chat-v0.24.13-linux-amd64` — чат.

[Релиз](https://github.com/yomezza/cosmosimulator/releases/tag/v0.24.13),
[общий конфиг](../../configs/shared.example.toml). Параметры сборки:
GOOS=linux, GOARCH=amd64, GOAMD64=v1, CGO_ENABLED=0. Нет зависимости от glibc
или внешнего динамического загрузчика. Контрольные суммы — downloads/SHA256SUMS,
исходный commit/параметры — astra-journey-v0.24.13-build.json.

## Запуск

После скачивания выдать право исполнения:

```sh
chmod +x astra-journey-v0.24.13-linux-amd64
chmod +x astra-journey-planet-v0.24.13-linux-amd64
chmod +x astra-journey-chat-v0.24.13-linux-amd64
```

Каждую службу запускать отдельным процессом с тем же общим CONFIG_FILE. Например:

```sh
CONFIG_FILE=/etc/astra-journey/config.toml ./astra-journey-v0.24.13-linux-amd64
```

Для фонового запуска можно использовать systemd, остановка — SIGTERM.
Планетарную службу и Chat запустить после готовности Cosmic/его миграций.
Конфигурационные секции `[cosmos]`, `[planet]`, `[chat]` и переменные окружения
сохранены. Старый путь к CONFIG_FILE тоже допустим: каталог не определяет имя игры.

Для новой установки данные можно хранить в `/var/lib/astra-journey`, настройки —
в `/etc/astra-journey`. Каталоги должны быть доступны соответствующим процессам.
При обновлении сохранить старые database.schema и erasure_journal; не выбирать
другую схему ради переименования. При переносе журнала скопировать существующий
файл модераторских удалений, а не создавать пустую замену.

## Сборка и публикация

```sh
python3 scripts/build-release.py v0.24.13
```

Скрипт по умолчанию собирает все три службы только для Linux amd64, сохраняет их
в downloads и обновляет SHA256SUMS/build.json. Для обновления только Chat:
`python3 scripts/build-release.py v0.24.14 --service chat` (после изменения Version).
По решению владельца новые Windows-сборки больше не выпускаются. Исторические файлы сохранены; готовые файлы скрипт не перезаписывает.
Каталоги команд: cmd/astra-journey, cmd/astra-journey-planet, cmd/astra-journey-chat.
Go module и адрес GitHub остаются github.com/yomezza/cosmosimulator.

Прежние выпуски до 0.24.13 сохраняют оригинальные имена бинарников в downloads
и на страницах релизов. Скрипт backfill для первого Linux выпуска 0.24.11
обслуживает именно тот старый тег и не переименовывает исторические файлы.
