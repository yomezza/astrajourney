# P2-1 выполнен · 0.15.0

Реализована самостоятельная основа планетарной службы. Существующие колонии/география/экономика не перенесены во второй процесс, их владельцем остаётся cosmic. Источником плана был согласованный [P2-0](../P2-0/README.ru.md).

## Что работает

- `cmd/cosmosimulator-planet`: отдельные console/Windows entry, адрес, сигналы/SCM остановки; service name AstraJourneyPlanet.
- `internal/planetserver`: проверяемый config из process environment, чистое подключение, JSON logging, HTTP listener с таймаутами, liveness/readiness, Origin allowlist и shutdown. SCM Running отправляется после bind listener.
- `internal/planet/runtime`: random owner identity, DB lease/fence, heartbeat, standby второго процесса, проверка истечения по часам БД, освобождение только своей записи.
- `internal/server/schema150.sql`: marker версии 1 и singleton lease. Только cosmic initialization исполняет DDL под прежним lock; planet ничего не создаёт.
- При отсутствии marker/несовместимой версии ready=503. При недоступной БД после старта live остаётся 200, ready=503; новый владелец не получает управление до истечения/освобождения старого lease.
- Health JSON явно сообщает stage=P2-1 и gameplay_ready=false. Игровые билеты не принимаются, WS route отвечает 503 без upgrade.
- Два versioned Windows EXE и SHA256, публикация обоих assets в release workflow. Старые сборки сохранены.

## Изменённые области

| Область | Файлы |
|---|---|
| Новая команда | cmd/cosmosimulator-planet/main.go, platform_other.go, platform_windows.go |
| Новый composition root | internal/planetserver/app.go, config.go, run.go |
| Runtime coordination | internal/planet/runtime/lease.go |
| Миграция | internal/server/schema150.sql, internal/initialization/database.go |
| Адресные тесты | internal/planetserver/config_test.go, internal/server/integration/planet150_test.go |
| Документация/config | .env.example, README, docs/planetary/P2-1, ссылки service-boundaries/server map |
| Сборки/CI/version | downloads + SHA256SUMS, release workflow, race selection CI, release-notes/v0.15.0.md, web/footer и benchmark version |

UI механики не менялись: только номер версии встроенного клиента. Не создавались папки будущих пустых подсистем, вторые таблицы аккаунтов/экономики или секторные игровые данные.

## Переиспользование

`platform/postgres.Open`/quoted DB_SCHEMA, pgx pool, `platform/httpkit.Reply`, действующий cosmic migration runner и его embed SQL, существующие PostgreSQL test fixtures, стандартные Go HTTP/context/slog, уже имеющаяся Windows svc dependency. Новых внешних зависимостей нет. Planet не импортирует internal/server/initialization и не вызывает world генератор.

Платформенный SCM код остаётся в отдельной маленькой команде: не создавался общий пакет с единственным thin wrapper ради сходства двух entrypoints. В планетарном адаптере добавлена корректная граница уведомления о готовом listener; космический SCM адаптер в этом этапе не переписывался.

## Проверки

- **8 адресных Go-тестов PASS** с одноразовой PostgreSQL: config, чистое подключение/schema config, существующая изоляция схем, четыре foundation сценария. [Журнал](checks/postgres.txt).
- **4 foundation race-теста PASS**: lifecycle/standby takeover, отсутствующая/несовместимая схема, expired predecessor/fence, ограниченная DB role. Дополнительно проверена потеря runtime DB-пула: live=200 и ready=503. [Журнал](checks/race.txt).
- Package boundaries и `go vet` затронутых пакетов — PASS.
- **Два настоящих Linux процесса** на временной `_test` БД: один cosmic и planet, сравнение cosmic tick/галактик/аккаунтов, graceful SIGTERM, независимая доступность cosmic, SIGKILL planet и takeover с более высоким fence. Наследованные ADMIN_* не запустили bootstrap planet; секреты отсутствуют в process logs. [Результат](checks/process-smoke.txt).
- Windows amd64: обе команды кросс-компилируются, проверки контрольных сумм сохранённых EXE проходят.

Общий UI-набор и весь старый DB/race suite локально повторно не запускались: игровые действия/UI не менялись. CI по main выполняет существующий общий прогон и новые foundation cases; race selection CI расширен на четыре новых сценария. Нативный Windows SCM в Linux не тестировался. Отдельная проверка двух реальных процессов проводилась на Linux, не объявляется испытанием Windows service runtime.

## Границы и следующий этап

Никакой высадки персонажа, A*, сцены, корпоративного planetary.enter, ticket/visit fencing, runtime координат или нагрузки 50 игроков пока нет. Process-level fence — инфраструктура для следующих этапов, а не уже реализованный контроль записи движения. Текущие first-phase stub visits не переписаны.

Следующий этап P2-2: общая pure проходимость канонической географии, модель связанных владений, dirty reconciler и посадочные площадки. Экономические ресурсы и colony_core сохраняют прежнюю механику.

## Миграция и откат

Две новые инфраструктурные таблицы, без UPDATE/DELETE игровых рядов. Сначала резервная копия и cosmic 0.15.0, затем planet. Разные DB logins рекомендуются; минимальные GRANT для planet проверены отдельно. Если используется общий администраторский login, приложение не превращает его в ограниченную DB role автоматически.

Для отката этой foundation-версии остановить planet и cosmic перед заменой cosmic на прежний EXE. Новые таблицы можно оставить: старый код их не использует, персонажей/новых визитов этот этап не создавал. Не переносить этот простой сценарий отката на будущие этапы с реальными визитами без их migration плана. Остановка planet во время обычной работы не требует отката cosmic.
