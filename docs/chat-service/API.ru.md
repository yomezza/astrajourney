# Контракт Chat v1

Внешний origin совпадает с терминалом. Cosmic проксирует запросы в независимую службу. Общая HttpOnly-cookie `session`; токен не передаётся в URL/JSON. WS принимает только явно настроенный Origin. Изменения доступны только через действующее Chat WS; чтения и выяснение результата операции — HTTP GET.

## WebSocket

`GET /chat/ws/v1`. Первая действующая вкладка сохраняет соединение; вторая получает HTTP 409 до upgrade. Lease с номером поколения проверяется внутри каждой изменяющей транзакции. Переподключение не даёт старому соединению право писать.

```json
{"type":"message.send","request_id":"UUID-v4","payload":{"kind":"global","body":"Привет!","entities":[]}}
```

После COMMIT:

```json
{"type":"ack","request_id":"тот же UUID","result":{"message_id":"UUID","channel_id":"UUID","seq":"1","sent_at":"server UTC","expires_at":"server UTC"}}
```

Ошибки передаются полем `error`: `auth_required`, `access_denied`, `rate_limited`, `muted`, `scope_changed`, `presence_unverifiable`, `conversation_pending`, `invitation_cooldown`, `idempotency_conflict` и другие коды проверок. Неизвестные ошибки БД превращаются в `request_failed`; SQL/содержимое запроса клиенту не возвращаются. ACK не содержит текста сообщения. Повтор UUID с тем же действием и payload возвращает исходные метаданные, другой payload — конфликт. При `operation_unknown` не создавать новую отправку: проверить `/operation?id=UUID`.

Команды:

| `type` | Существенные поля `payload` |
|---|---|
| `message.send` | `kind`, `target`, `body`, необязательно `reply_to`, `entities` |
| `message.delete` | `id`; модератору обязательна `reason` без полного текста оригинала |
| `favorite.copy` | `id` живого доступного оригинала |
| `favorite.note`, `favorite.edit` | `body`, для изменения `id`; редактирование только личных заметок |
| `favorite.move`, `favorite.delete` | `id`, для переноса `folder` либо пустая строка |
| `folder.create`, `folder.rename`, `folder.delete` | `name`, для существующей папки `id` |
| `private.create` | `kind:dm` + `user` либо `kind:group` + `title` |
| `private.invite`, `private.rename` | `target` беседы, `user` либо `title` |
| `private.decide` | `id` приглашения, `decision:accept/reject` |
| `private.owner` | `target`, `user` нового владельца, `revision` |
| `private.leave`, `private.kick` | `target`; для исключения/передачи при выходе `user` |
| `private.delete` | `target`, `confirm:true`; результат `deleting`, физическое завершение выполняет работник |
| `read` | `kind`, `target`, для Private до 100 видимых `ids`, для общих каналов `seen_seq` |
| `mute`, `unmute` | `target` корпорации, `user`, `reason`; mute дополнительно `duration` |
| `report.create`, `report.resolve` | `id` сообщения + `reason` либо `id` жалобы + `decision:resolved/rejected` |
| `settings` | объект `settings`: `opacity` 40–100 с шагом 5, `docked`, `sounds`, `tab` |
| `notification.read` | массив `ids` собственных уведомлений |

`kind`: `global`, `planet`, `sector`, `corporation`, `private`. `target`: пусто для Global, surface ID для Planet, `surface/sector` для Sector, company ID для Corporation, UUID беседы для Private. Даже при переданном target сервер заново проверяет текущие права/присутствие; он не принимает произвольную подписку на чужой сектор.

Прочие server frames: `hello` с собственной идентичностью, `health` с доступностью служб, `sync` без содержимого переписки и чужих UUID. Получатель синхронизации проверяется по текущему доступу. Доставка использует перекрытие ограниченного времени транзакций, чтобы не потерять событие с меньшим sequence ID, закоммиченное позже другого.

## Чтение

`GET /chat/api/v1/{route}`:

| Route | Назначение |
|---|---|
| `health` | Проверка процесса, БД и зависимостей, без токенов/строк подключения |
| `bootstrap` | Свои беседы/приглашения, папки, настройки, собственные ограничения и доступные области |
| `history?kind=&target=&before=&search=` | До 50 сообщений, keyset по seq, только живая доступная история |
| `favorites?id=&before=&search=` | Свои заметки/копии; id папки, пустой id — виртуальная «Все» |
| `private.search?search=` | Поиск по доступным активным личным беседам, минимум три символа |
| `roster?kind=&target=` | Доступный список участников/пилотов |
| `notifications` | Собственные уведомления после повторной проверки области |
| `operation?id=` | `completed` с неизменными метаданными, `not_found` либо `in_progress` |
| `link.options?search=`, `link?id=` | Выбор проверяемых игровых целей и повторная проверка JSON entity при открытии |
| `my.moderation` | Причины наказаний самого игрока |
| `corporate.moderation` | Текущие корпоративные ограничения и история действий; владелец/модератор |
| `reports`, `report?id=`, `audit` | Служебные жалобы, один расследуемый оригинал и журнал; игровой администратор/модератор |
| `service.channels`, `service.history?id=` | Аудируемый служебный просмотр публичных/корпоративных областей, без Private/Favorites |
| `metrics` | Сводные количества по видам сообщений, только игровой администратор |

Назначение игровых модераторов принадлежит Cosmic: `POST /api/admin/chat/moderator` с `{user:"ID",granted:true/false}`, только администратор. Корпоративное полномочие `chat.moderate` настраивается существующим редактором ролей.

Длина считается Unicode codepoints: заметки 2500, Private 500, прочее 256. Частота на аккаунт, а не на беседу: Global 1/3с, Planet 1/2с, Sector/Corporation 1/с, Private 10/скользящие 10с. Приглашения — 20 созданных одному адресату за 24ч, отказ — 24ч для той же беседы/пары; действуют 7 суток, занятость группы не более 50 с ожидающими приглашениями. Жалобы — 5 за 10 минут. Звук массового упоминания ограничен одним на канал в 60 секунд.

Сетевые ограничения отделены от игровых: 24 КиБ на WS frame, очередь 16, burst 20 и 10 frames/с на соединение; до 64 одновременно выполняемых HTTP-чтений и до 256 открытых WS. Медленный клиент отключается при истечении срока записи, восстанавливает данные через bootstrap. Они не вводят скрытого дневного лимита групп/заметок.
