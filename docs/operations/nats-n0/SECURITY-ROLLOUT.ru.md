# TLS, пароли, ACL и применение к существующему cosmoNATS

Это **проект изменений**, не автоматически применяемая конфигурация.
Рабочий сервер не изменялся. Остановка/reload, замена auth, включение TLS и
изменения firewall требуют отдельного разрешения владельца и окна обслуживания.

## 1. Что менять в имеющемся основном файле

[server-tls.config.example](templates/server-tls.config.example) сохраняет:
`server_name=cosmoNATS`, `192.168.111.44:4222`, существующий `store_dir`,
512MB/10GB и ротацию лога 50MB/5. Правки — TLS и monitoring на loopback.
`include ./auth.config` сохраняет настоящее имя файла. Если в реальном файле
есть дополнительные настройки, их нельзя затереть копированием шаблона целиком.
Не устанавливать ещё один основной NATS, не создавать новый production store,
streams, accounts или Windows Service.

## 2. Точные ACL

[auth.config.example](templates/auth.config.example) и
[acl-matrix.json](acl-matrix.json) перечисляют полные subjects, без универсального
`>` и без автоматического разрешения будущих событий.

| Login | Publish | Subscribe |
| --- | --- | --- |
| `cosmos` | 6 `cosmos.v1.signal.cosmic.*` из каталога 03 | только `cosmos.v1.signal.chat.company.cleanup.completed` |
| `planet` | 6 `cosmos.v1.signal.planet.*` из каталога 03 | запрещены все подписки (`deny: [">"]`) |
| `chat` | `chat.outbox.changed`, `chat.company.cleanup.completed` под общим prefix | 6 перечисленных Cosmic signals, 6 Planet signals и собственный outbox signal |

`*` в этой таблице — краткое обозначение перечня; реальные ACL содержат полные
имена. Все прочие публикации/подписки запрещены списками allow. Никто из трёх
пользователей не имеет `$JS.API.>`, `$SYS.>`, `_INBOX.>` или
`cosmos.v1.event.>`; Request/Reply и долговечные события в пилоты не входят.
Это не меняет пользовательские права игры. Транспортный login `cosmos`
публикует домен `cosmic`, как требует контракт 03.

## 3. Новые пароли и bcrypt

Используйте три разных случайных пароля из менеджера секретов, например
48–64 ASCII-символа; **не более 72 UTF-8 байтов** из-за ограничения bcrypt.
Не передавайте пароль в URL, CLI аргументах, Git, переписке, скриншотах или
PowerShell transcript. Клиенту нужен исходный пароль, серверу — его bcrypt hash.
Хеш не является клиентским паролем. `.creds` не используется.

С установленной официальной NATS CLI, в интерактивной консоли без записи ввода:

```powershell
nats server passwd
```

Введите новый пароль в защищённом интерактивном запросе. Полученный bcrypt
локально вставьте вместо `<BCRYPT_HASH_COSMOS>` в копию auth.config, аналогично
для Planet/Chat. Предпочтительна стоимость не ниже 11; проверяйте параметр
своей версии CLI через `nats server passwd --help`. Не используйте `-p`/`--password`
или аргумент со значением пароля. Placeholders из шаблона не пригодны для работы.
Эта команда не выполнялась с рабочими секретами владельца.

Сначала создайте **приватную** папку для клиентских password files. Пример
команд для оператора после выбора реальных service accounts:

```powershell
$secretDirectory = 'D:\cosmosimulator\nats\secrets'
if (Test-Path -LiteralPath $secretDirectory) { throw 'Directory already exists; review its ACL instead of overwriting it' }
New-Item -ItemType Directory -Path $secretDirectory
icacls.exe $secretDirectory /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)'
if ($LASTEXITCODE -ne 0) { throw 'Secret directory ACL failed' }
```

Не запускайте эти команды на существующей папке с неизвестными ACL без
отдельной проверки. Группы указаны SID, независимо от языка Windows. Для
каждой реальной учётной записи службы выдайте **только traverse/read папки**,
не наследуемое чтение всех файлов. На конкретный файл — Read его службы:

```powershell
$cosmicAccount = 'YOUR_MACHINE\YourCosmicServiceAccount'
icacls.exe $secretDirectory /grant "${cosmicAccount}:(RX)"
pwsh -NoProfile -File .\docs\operations\nats-n0\scripts\New-NatsPasswordFile.ps1 `
  -Path "$secretDirectory\cosmos.password" -ServiceAccount $cosmicAccount
icacls.exe "$secretDirectory\cosmos.password"
```

Скрипт запрашивает тот же пароль через Read-Host -AsSecureString, пишет UTF-8
без перевода строки, запрещает перезапись и не выводит пароль. Повторите для
Planet и Chat с их аккаунтами и разными значениями. Проверяйте членство групп:
общая учётная запись Windows для всех служб не обеспечивает файловую изоляцию.
Скрипт использует управляемую строку на короткое время для записи; это защита
доступа к файлу и выводу, не обещание исключить секрет из дампа памяти процесса.

NATS account получает Read к своему `auth.config` и TLS private key; Modify —
только к существующим data/logs. Клиентским аккаунтам закрыть auth.config,
private key и чужие password files; разрешить Read публичного CA. Проверить
`icacls` от имени реальных аккаунтов. Действующие значения в Git не сохранять.

## 4. Сертификаты и проверка TLS

Используйте корпоративную CA либо отдельную частную CA. Сертификат сервера
должен иметь `IP:192.168.111.44` в SAN для подключения по этому IP. CN недостаточно.
Для подключения по DNS добавьте **реальное** DNS-имя в SAN; не отключайте
проверку имени. Частный ключ CA хранится отдельно/offline и не нужен брокеру.

Пример на машине выпуска с установленным OpenSSL, в защищённой папке:

```powershell
# CA key зашифрован, OpenSSL запросит пароль интерактивно.
openssl req -x509 -newkey rsa:4096 -keyout ca-key.pem -out ca.pem -days 3650 -sha256 `
  -subj '/CN=Cosmosimulator Private CA' -addext 'basicConstraints=critical,CA:TRUE'
# Серверный ключ для unattended NATS; защищается NTFS ACL.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out server-key.pem
openssl req -new -key server-key.pem -out server.csr -config server-cert.cnf
openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial `
  -out server-cert.pem -days 365 -sha256 -extfile server-cert.cnf -extensions server_ext
openssl verify -CAfile ca.pem -verify_ip 192.168.111.44 server-cert.pem
openssl x509 -in server-cert.pem -noout -dates -ext subjectAltName
```

`server-cert.cnf` — локальная копия [шаблона](templates/server-cert.cnf.example).
Копировать на брокер только server-cert.pem, server-key.pem; клиентам — ca.pem.
Если CA использует intermediate, сервер отдаёт leaf+intermediate chain.
Зафиксировать владельца сертификата, срок, процедуру продления заранее.

В существующий основной config добавить TLS блок из шаблона. Это server TLS
с обычным user/password, не mTLS. Клиенты проверяют CA, срок и SAN; не добавлять
`InsecureSkipVerify`, permissive callback или `--insecure`. После реального
включения TLS будущий адрес — `tls://192.168.111.44:4222`; текущий plaintext
брокер не становится TLS-сервером от изменения URL в TOML.

## 5. Monitoring и Windows Firewall

Предложение: `http: "127.0.0.1:8222"`, monitoring остаётся доступен оператору
локально. Он не получает авторизацию из users NATS и не защищается автоматически
TLS блока клиентского listener. Не публиковать `/connz`, `/varz`, `/jsz` в LAN.

Для удалённого администратора — существующий защищённый SSH tunnel, например:

```powershell
ssh -N -L 127.0.0.1:18222:127.0.0.1:8222 admin@192.168.111.44
```

Затем `http://127.0.0.1:18222/varz` на машине администратора. SSH должен уже
быть настроен и проверять host key; новая SSH/WinRM служба автоматически не
устанавливается. Альтернатива — действующий Kerberos/HTTPS PowerShell Remoting
или отдельный HTTPS reverse proxy с обязательной авторизацией и IP ACL.

После инвентаризации согласуйте точные RemoteAddress/Program/Profile правила
4222. Не открывать порт всему LAN по умолчанию. Добавление узкого Allow **не
закрывает прежний широкий Allow**; старые правила пересматривать по точному
ID и с возможностью отката. Не отключать GPO/Firewall целиком. Недоступность
8222 посторонним подтвердить с другого ПК, не только с сервера.

## 6. Безопасный порядок применения — только после отдельного разрешения

1. Снять read-only inventory, версию/команду запуска/SCM account, список
   существующих подключённых клиентов. Изменение auth/TLS затронет и клиентов,
   не относящихся к игре; согласовать их переподключение.
2. Сохранить текущие основной config и auth.config с исходными ACL в
   защищённой резервной папке. Зафиксировать store_dir, лимиты, версию бинарника.
3. На отдельном тестовом процессе проверить именно установленную версию
   `nats-server` с предлагаемыми TLS/ACL, а не полагаться только на локальный
   результат с 2.15.2. У брокера и теста разные порты и хранилища.
4. Выпустить/проверить сертификат, подготовить новые секреты и хеши, проверить
   доступ служебных аккаунтов. Не переписывать файлы у работающего процесса
   промежуточными/пустыми значениями. Новые файлы сначала подготовить отдельно.
5. `nats-server -t -c <подготовленная-копия>` с include на подготовленный
   auth.config. Если меняется расположение копии, include должен разрешаться
   к нужному файлу, а не к старому auth. Содержимое и права проверить локально.
6. В согласованное окно оператор останавливает **выявленную существующую**
   службу/обёртку, дожидается завершения процесса. Не угадывать имя по server_name.
7. При выбранном cold backup сохранить весь существующий store в остановленном
   состоянии, затем заменить только утверждённые config/auth/cert файлы.
   `D:/cosmosimulator/nats/data` не очищать, не перемещать и не пересоздавать.
8. Запустить тот же бинарник и ту же службу с прежним store_dir. Проверить
   лог запуска, listening, локальные `/varz`/`/jsz`, три TLS CONNECT/PING,
   remote firewall/monitoring. Убедиться, что прежние streams/consumers на месте.
9. При неудаче остановить только эту службу и вернуть сохранённые файлы/ACL.
   Не откатывать или удалять JetStream store ради ошибки TLS. Временный возврат
   к plaintext допускается только по отдельному решению в закрытой сети;
   при компрометации старого пароля его не восстанавливать.
10. Удалить старые credentials после подтверждения перехода всех клиентов,
    защитить backup. Игровые службы на N0 всё ещё **не подключать**.

План не обещает бесшовный reload: изменение credentials/ACL/TLS может разорвать
или потребовать пересоздать существующие подключения. Приёмка проводится на
новых соединениях, а не по сохранившемуся старому socket.

## 7. Сохранность и backup

Факт: рабочая backup policy, расписание, RPO/RTO и restore не подтверждены.
По `/jsz` нельзя объявлять backup исправным. JS replicas=1 — не HA и не backup.

Для N0 предлагается: защищённая копия config/auth/cert metadata перед каждым
изменением; регулярные snapshots существующих streams средствами NATS с
отдельной операторской identity, **либо** полный cold backup store при
остановленном брокере. Игровым `cosmos/planet/chat` не давать JS API ради backup.
Точные operator ACL для snapshot/restore определяются по реальному перечню
streams; универсальный `$JS.API.>` в эти три login не добавляется.

Не копировать активный JetStream store обычным robocopy как «согласованный backup».
Пример только после разрешённой штатной остановки и подтверждения отсутствия
процесса, с новым каталогом назначения на защищённом backup-томе:

```powershell
robocopy 'D:\cosmosimulator\nats\data' 'E:\protected-backup\cosmoNATS\APPROVED_DATE\data' /E /COPY:DAT /DCOPY:DAT /R:1 /W:1
if ($LASTEXITCODE -ge 8) { throw 'JetStream backup copy failed' }
```

Путь/дату выбирает оператор. Это не команда остановки и не разрешение её
выполнять сейчас. Проверять manifest/хеши, права и восстановление копии на
**отдельном тестовом broker/store**. Не использовать рабочий store одновременно
в двух процессах. Закрытые ключи/пароли резервировать только в защищённом
зашифрованном хранилище; CA private key хранится отдельно. Retention, частоту
и приемлемые RPO/RTO согласовать; до этого гарантий восстановления нет.

PostgreSQL backup и внешний `CHAT_ERASURE_JOURNAL` остаются независимыми;
NATS/JetStream их не заменяет. Будущие Economic events, outbox/inbox и сверка
разновременных backup относятся к следующему отдельному проекту.
