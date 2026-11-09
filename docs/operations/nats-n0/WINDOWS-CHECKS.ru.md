# Проверка уже установленного NATS под Windows

Все команды этого раздела read-only. Нужен PowerShell **7.4+** (`pwsh`),
для firewall/process inventory — консоль администратора. Windows PowerShell 5.1
не подходит для проверочного TLS-клиента с CustomRootTrust. Скрипты не
запускают и не перезапускают службы, не публикуют игровые сигналы.

## 1. Версия, основной файл и auth.config

Уточните имя настоящего основного файла; `server.config` ниже — пример имени,
а не утверждение о вашей установке. Выполнять на машине `192.168.111.44`:

```powershell
$natsBinary = 'D:\cosmosimulator\nats\nats-server.exe'
$natsConfig = 'D:\cosmosimulator\nats\server.config'
& $natsBinary --version
& $natsBinary -t -c $natsConfig
```

`-t` только разбирает конфигурацию и include, не запускает новый сервер. Выход
0 означает корректность синтаксиса **для этой установленной версии**, но не
доступность сертификата из-под служебного аккаунта и не применение файла
работающим процессом. Проверить include `./auth.config` локально; не присылать
его содержимое/хеши/пароли в отчёт. Рабочие секреты не нужны разработчику.

Сводный безопасный сбор:

```powershell
pwsh -NoProfile -File .\docs\operations\nats-n0\scripts\Get-NatsN0Readiness.ps1 `
  -NatsServerPath $natsBinary -ConfigPath $natsConfig `
  -ServerAddress '192.168.111.44' -MonitorUri 'http://192.168.111.44:8222'
```

До переноса monitoring используйте фактический LAN URI; после переноса —
`http://127.0.0.1:8222`. Включённый auth в INFO и успешный `-t` ещё не доказывают
правильность пароля каждого пользователя. Для этого после согласованного TLS
применения предназначена отдельная проверка в разделе 4.

## 2. Listening ports и реальные ограничения firewall

На сервере:

```powershell
Get-NetTCPConnection -State Listen | Where-Object LocalPort -In 4222,8222 |
  Select-Object LocalAddress,LocalPort,OwningProcess
Get-NetFirewallProfile | Select-Object Name,Enabled,DefaultInboundAction
```

Inventory-скрипт дополнительно выводит применимые inbound TCP/Any правила,
включая диапазоны портов и `Any`, Action, Profile, RemoteAddress и Program из
ActiveStore. Нельзя считать один найденный Allow доказательством эффективного
доступа: возможны широкое правило приложения, Block, GPO, иной профиль и
внешний firewall. Не отключать firewall целиком для проверки.

На **другом компьютере LAN** до/после изменения, отдельно для доверенного и
постороннего адреса:

```powershell
Test-NetConnection '192.168.111.44' -Port 4222
Test-NetConnection '192.168.111.44' -Port 8222
```

Ожидается: 4222 доступен только разрешённым клиентским машинам; 8222 после
переноса на loopback недоступен из LAN, но работает локально. Проверка с самого
сервера не заменяет попытку с посторонней машины. Правила IPv6 и дополнительные
слушатели также проверить, если IPv6 включён. Эти пробы из облака не выполнены.

## 3. Как запущен NATS и где backup

Inventory связывает PID `nats-server.exe` и родителя с Windows Service, выводит
Name/State/StartMode/StartName и ищет Scheduled Tasks. Это помогает увидеть
WinSW/NSSM, встроенную службу или ручной запуск. Имя `cosmoNATS` внутри NATS
**не обязательно** является именем SCM-службы. Не создавайте вторую службу.

```powershell
Get-CimInstance Win32_Service | Where-Object Name -Match 'nats' |
  Select-Object Name,State,StartMode,StartName,ProcessId
Get-ScheduledTask | Where-Object TaskName -Match 'nats|backup' |
  Select-Object TaskPath,TaskName,State
Get-Acl 'D:\cosmosimulator\nats\data' | Select-Object Owner,AccessToString
Get-Item 'D:\cosmosimulator\nats\data' | Select-Object FullName,LastWriteTime
```

Если служба-обёртка называется иначе, сопоставьте её PID/parent PID с inventory.
Уточните путь `-c` в фактической команде запуска локально; не публикуйте
командную строку с секретами. Наличие задачи backup и LastWriteTime папки не
доказывает годность копии: нужны успешный результат задачи, полный manifest
копии, защищённое хранилище и восстановление на отдельном экземпляре.

## 4. JetStream и read-only login после TLS

Локально после переноса monitoring:

```powershell
Invoke-RestMethod 'http://127.0.0.1:8222/varz' |
  Select-Object server_name,version,auth_required,tls_required,connections
Invoke-RestMethod 'http://127.0.0.1:8222/jsz' |
  Select-Object disabled,config,memory,storage,accounts,streams,consumers,messages
```

Ожидаются включённый JS и лимиты 512MB/10GB, прежний store в основном файле.
Эти GET не создают stream и не доказывают сохранность сообщений после restart.
До изменения используйте фактический monitoring URI из раздела 1.

После **разрешённого и выполненного оператором** применения TLS/новых секретов:

```powershell
pwsh -NoProfile -File .\docs\operations\nats-n0\scripts\Test-NatsN0Login.ps1 `
  -Username cosmos -PasswordFile 'D:\cosmosimulator\nats\secrets\cosmos.password' `
  -CAFile 'D:\cosmosimulator\nats\certs\ca.pem'
```

Повторите с `planet` и `chat` и их файлами. Это только CONNECT/PING через
проверенный TLS, без publish/subscribe. Пароль не передаётся в аргументах,
URL или выводе. До TLS этот скрипт **откажется** передавать пароль по plaintext.
TLS-проверка не устанавливает CA в системное хранилище и не отключает SAN/chain
validation; для частной CA без CRL проверка отзыва в пробнике отключена,
проверки цепочки, срока и имени сохраняются.

ACL-publish, запрещённые действия, restart и создание пробного stream выполняйте
только на изолированном брокере: [матрица и команды](TEST-MATRIX.ru.md).
