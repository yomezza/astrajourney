# MATRIX × STUDIO 2.0 — серверный контракт

Версия API — `/api/studio/v2`. Базовый main — `60f2fcd4846595210425b9a2d646e59bbeaab26c`. Требования сверены с концепцией `48ef07cea51b3ecbf3ab8238f9568bfe9a3e5b83`. Первая редакция контракта опубликована коммитом `cdcf9dc` для подготовки Frontend. Эта редакция описывает ответы реализации.

## Реестр

`GET /api/studio/v2/catalog` возвращает `{"families":[{"type":"ship","id":"M001","variants":[{"id":"M001.01","variant":"ship.owned","purpose":"owned","bindings":["workspace","fixed"],"placements":["tile","window"]}]}]}` с тремя семействами. Подвид вычисляется по паре `type` и `purpose`.

| type | purpose | variant | binding |
| --- | --- | --- | --- |
| ship | owned | ship.owned | workspace, fixed |
| inventory | cargo | inventory.cargo | workspace, fixed |
| inventory | personal | inventory.personal | derived, fixed |
| inventory | corporate | inventory.corporate | derived, fixed |
| navigation | interstellar | navigation.interstellar | workspace, fixed |
| navigation | local | navigation.local | workspace, fixed |

Все шесть подвидов поддерживают плитку и временное окно. Для окон Frontend фиксирует разрешённый якорь при открытии. Для derived это исходный корабль. Сохранение окна в раскладке запрещено.

## Раскладка

`GET /api/studio/v2/layout` и `PUT /api/studio/v2/layout` используют один DTO. При записи передавайте последнюю полученную ревизию. Успешная запись возвращает новый документ с ревизией. При 409 перечитайте раскладку и согласуйте локальные изменения. Порядок определяется массивами. UUID v4 принадлежит аккаунту, уникален среди пространств и экземпляров. При дублировании создайте новые UUID.

```json
{"revision":7,"layout":{"version":2,"active":"00000000-0000-4000-8000-000000000001","default":"00000000-0000-4000-8000-000000000001","workspaces":[{"id":"00000000-0000-4000-8000-000000000001","name":"Мой флот","ship_id":41,"modules":[{"id":"00000000-0000-4000-8000-000000000002","type":"inventory","purpose":"personal","binding":{"mode":"derived","relation":"docked_station"},"settings":{"display":"table","sort":"quantity"}}]}]}}
```

- `ship_id:0` означает пространство без корабля. Отсутствующий контекст отображайте явно.
- `binding` — `{"mode":"workspace"}`, `{"mode":"fixed","ship_id":42}`, `{"mode":"derived","relation":"docked_station"}`, `{"mode":"fixed","star":17,"station":"station-1"}` (+ `company_id` для корпоративного склада), либо `{"mode":"global"}` для совместимой сводки. Производный корпоративный склад может закрепить `company_id`.
- `settings` допускает `geometry:{x,y,w,h,pinned,minimized}`, `wide:boolean`, `section:"overview"|"specs"|"equipment"` для корабля, `display:"table"|"grid"`, `sort:"name"|"quantity"` и `category:"resources"|"equipment"` для инвентаря. `legacy_view:"card"|"window"` сохраняет режим исходной карточки при переносе. Для новых плиток передавайте `settings:{}` либо настройки семейства.
- Лимиты — 1–10 пространств, до 16 экземпляров на пространство, 1–32 графемы в имени, 64 КиБ JSON на запись. Все поля, кроме обозначенных необязательных настроек и ID в binding, обязательны. `customized:boolean` необязателен у пространства.

### Миграция и откат

V2 хранится рядом с V1 в той же строке. До первой записи V2 сервер детерминированно преобразует текущую V1 при чтении. После записи обе версии живут независимо с общей ревизией и общей проверкой UUID. Старый Web продолжает читать и сохранять свой документ. Новые настройки остаются в V2 при откате Web на V1. Обе колонки входят в резервную копию, удалять V2 при откате приложения не требуется. При повторном включении V2 пользователь продолжит работу со своей сохранённой V2. Триггер удерживает UUID из V2 при очистке V1 старым бинарником. Для нового аккаунта с единственным кораблём стартовое пространство содержит корабль, межзвёздную навигацию, трюм и прежние операции. При нескольких кораблях или пустом флоте стартовый контекст отсутствует. Уже существующие раскладки сохраняются.

Маппинг — `fleet/overview → ship.owned`, `cargo → inventory.cargo`, `map → navigation.interstellar`, `operations/market/mining → legacy`, где purpose повторяет старый type. Старая плитка `map` показывает состояние маршрута и открывает карту отдельной кнопкой. Плитка переходит в навигацию, отдельная карта остаётся доступна в прежнем инструменте. `fixed` сохраняет ID, `workspace` сохраняет ссылку на пространство, старый ship-scoped `none` становится `workspace`; при ship_id=0 цель отсутствует. `overview`, `map`, `operations` и `market` при переносе следуют кораблю пространства. Исходный `view:window` становится настройкой плитки `legacy_view`, а временное окно не восстанавливается. UUID, порядок, названия, фильтры, геометрия и закрепление сохраняются. Устаревшие ссылки допускаются для существующих экземпляров, новые ссылки проверяются на доступ.

## Адресные операции

Запросы требуют обычную игровую сессию. Чтения принимают явные идентификаторы. Резолвинг `workspace` выполняет клиент. Для `derived` передавайте явный корабль-якорь серверу, который проверит фактическую стыковку. При отсутствии контекста показывайте состояние ошибки и не переключайте глобальный активный корабль.

| Метод | Путь | Назначение |
| --- | --- | --- |
| GET | /api/studio/v2/ships/{ship_id} | Обзор, характеристики и оснащение |
| GET | /api/studio/v2/inventory?kind=ship&ship=42 | Трюм |
| GET | /api/studio/v2/inventory?kind=personal&star=17&station=station-1 | Персональный склад |
| GET | /api/studio/v2/inventory?kind=company&star=17&station=station-1&company=3 | Корпоративный склад |
| GET | /api/studio/v2/inventory?kind=personal&ship=42 | Склад станции стыковки 42 |
| GET | /api/studio/v2/inventory?kind=company&ship=42&company=3 | Корпоративный склад станции стыковки 42 |
| POST | /api/studio/v2/inventory/transfer | Атомарный прямой перенос |
| GET | /api/studio/v2/ships/{ship_id}/navigation | Позиция, маршрут, системы, цели и закладки |
| POST | /api/studio/v2/ships/{ship_id}/preview | Предпросмотр маршрута, манёвра, заправки или спасения |
| POST | /api/studio/v2/ships/{ship_id}/commands | Команда адресу корабля |
| POST | /api/planet/handoff | Существующий протокол высадки |

Параметры склада `star/station` и `ship` взаимоисключающие. `company` обязателен для фиксированного корпоративного склада и опционален для производного. Ответ инвентаря содержит `location` (формат ниже), `items:[{"resource":"agni","quantity":5,"category":"resources"}]`, `equipment` (неустановленные экземпляры оснащения), `capacity`, `used`, `reserved`, `free` (числа у трюма, `null` у склада), `can_deposit` и `can_withdraw` (права роли, стыковка и остатки проверяются при переносе). Пустые списки — `[]`. Для ресурсов единица количества — игровая единица груза. `free` учитывает резерв в трюме. Для складов отсутствует общий лимит вместимости, при этом действует прежний технический предел на количество ресурса.

```json
{"request":"00000000-0000-4000-8000-000000000003","mode":"direct","from":{"kind":"ship","ship":42},"to":{"kind":"personal","star":17,"station":"station-1"},"resource":"agni","quantity":2}
```

Формат места хранения — `{kind:"ship",ship}`, `{kind:"personal",star,station}` или `{kind:"company",star,station,company}`. Для переноса между складами укажите стыкованный корабль отдельным верхнеуровневым полем `ship`. Перенос принимает также `items:[{resource,quantity}]` вместо пары `resource/quantity`. Запрос использует действующие правила логистики. Повторите идентификатор (16–64 символа) с неизменным телом при сетевом сбое. Сервер повторно проверяет доступ к источнику и назначению. Успешный ответ для одной позиции — `{from,to,mode,resource,quantity,task:0,time}`. Для нескольких позиций вместо `resource,quantity,task` приходят `items,tasks:[]`. В ответе `from/to` может дополнительно содержать фактические `star,station` корабля. Оборудование со своими экземплярами перемещается по контракту оснащения, поскольку логистика переносит только ресурсы.

Чтение корабля возвращает `ship` (текущий DTO флота), `base` и `effective` (структура характеристик корпуса), `docked`, `cargo_used`, `module_used`, `module_limit`, а также `activity` и `fitting`. Команды принимают `request` (строка 16–64 символа) и `action`. Повтор с тем же request и неизменным телом возвращает сохранённый ответ. При новом намерении создавайте новый request. Действия доступны с учётом реальных правил конкретной команды. Состояние либо предпросмотр сами по себе не гарантируют успех.

| action | Поля команды |
| --- | --- |
| rename_ship | name |
| refuel | quantity: 1, одна единица водорода |
| module_install, module_remove, module_sell | id экземпляра оснащения |
| module_buy | target: catalog_id, star, station, price: подтверждённая цена из offers |
| module_scoop | без параметров |
| route | waypoints: массив 1–20 систем |
| cancel_route | без параметров |
| move | star, target, mode: approach, orbit, docked |
| portal_travel | target: строковый ID открытой галактики из portal_destinations |
| rescue | confirm: true, expected_cost: цена из preview |
| note | star, target (пустая строка для системы), note, bookmark |

Команда возвращает `{"ok":true}`. Для заправки сервер использует действующую механику: одна единица водорода из трюма добавляет до 5 единиц топлива с ограничением по ёмкости. Произвольное количество за один запрос и покупка топлива за кредиты потребуют отдельного продуктового решения. Выход из стыковки работает при локальном перемещении в другой режим, отдельного манёвра без назначения в действующей механике нет.

Предпросмотр принимает `action` и те же параметры действия, включая `waypoints` для `route` и `star/target/mode` для `move`. Для `rescue` на предпросмотре передавайте только `action`. Ответ `{action,path,target,cost,seconds,minimum_fuel,can_afford}`. Пример маршрута:

```json
{"action":"route","waypoints":[17,21]}
```

```json
{"action":"route","path":[15,17,19,21],"waypoints":[17,21],"fuel":3,"seconds":45,"available_fuel":20,"reserved_fuel":0}
```

Маршрут вычисляется по игровым гиперкоридорам. Соседние одинаковые точки, включая исходную, пропускаются. Успешная команда `route` оставляет маршрут для обработки игровым тиком. `seconds` — сумма длительностей переходов без ожидания серверного тика. Отмена убирает оставшиеся точки и резерв, не прерывая начатый переход. Перед заменой уже поставленного маршрута отправьте `cancel_route`. Локальный предпросмотр возвращает `{action,target,mode,seconds,fuel}`, заправка — `{action,resource:"agni",quantity:1,fuel,cost:0}`. `fuel` в ответе заправки означает фактическую прибавку. Для высадки используйте действующий формат `{"operation":"UUID v4","landing_site":"17","ship":"42"}`, где оба игровых ID — десятичные строки. Свежий ответ выдаёт `{state:"pending",operation,ticket,expires_at,endpoint:"/planet/ws/v1",protocol:1}`. Повтор той же операции после активации возвращает состояние визита. Передайте билет действующему загрузчику Planet Mode. Существующие Planet-коды ошибок включают `invalid_handoff`, `access_denied`, `ship_busy`, `operation_conflict`, `visit_already_exists`, `runtime_unavailable` и `planetary_disabled`.

Ошибки возвращают `{"error":"сообщение"}` в формате Cosmic. Обрабатывайте HTTP-статус, текст предназначен для показа. 400 — неверный JSON, входные поля или раскладка; 401 — сессия; 404 — недоступный объект; 409 — конфликт ревизии, повтор с изменённым содержимым или текущее игровое ограничение; 415 — при записи отправлен иной Content-Type; 422 — коллизия UUID раскладки; 503 — недоступен Planet Runtime. После 409 перечитайте данные или повторите предпросмотр до отправки нового действия.


### Поля ответов

`number` в ID и количествах означает целое число. Координаты и орбиты могут быть дробными. Время — строка RFC 3339, `null` при отсутствии. Все POST и PUT принимают `Content-Type: application/json`. Запросы из другого origin отклоняются с 403.

| DTO | Структура |
| --- | --- |
| Ship | `{id,name,class,spec,star,object_id,position_mode,active,fuel,reserved_fuel,cargo,destination,arrival,departure,path,waypoints,local_target,local_mode,local_started,local_arrival,extractor,mining,modules,module_limit,modules_ready}`. `cargo` — словарь `resource → quantity`, `mining` — `{min,max,current}`, `path/waypoints` — массивы ID систем, `destination` и `local_*` могут быть `null`. |
| ShipClass | `{id,module_limit,name,role,description,cargo,fuel,jump_seconds,jump_fuel,mining_bonus,mining,price,model,length,required_level}`. `model` — имя ассета корпуса. Размеры трюма и бака в единицах игры, время перехода в секундах. |
| Module | `{id,catalog_id,name,kind,rating,size,price,description,effects,installed,paid}`. У `effects` необязательные поля `extractor,cargo_bonus,yield_bonus,scan_percent,jump_percent,scoop_rate`. |
| ModuleOffer | Поля Module без `id,installed,paid`, плюс `quantity`. `price` — фактическая цена единицы. |
| Capability | `{available:boolean,reason?:string}`. Отсутствие ограничения на момент чтения означает только возможность попытки. |
| ShipView | `{ship,base,effective,docked,cargo_used,module_used,module_limit,activity,fitting,capabilities}`. `base/effective` — ShipClass. `activity` — булевы `mining,research,delivery,automation,parked`. `fitting` — `{available,reason?,free,items:[{id,install,remove,sell}],offers:[ModuleOffer]}`. `install/remove/sell` — Capability. `capabilities` содержит `rename_ship,refuel,module_scoop`. |
| NavigationView | `{ship,systems,sites,galaxies,bookmarks,landing_sites,rescue_cost,remaining_route,route_valid,eta,available_fuel,portal_destinations,capabilities}`. |
| NavigationView.systems | `[{id,name,x,y}]` — публичные системы галактики корабля. |
| NavigationView.sites | `[{id,name,kind,orbit,modes}]` — раскрытые объекты и публичные порты текущей системы. `modes` — массив режимов `approach/orbit/docked`. |
| NavigationView.galaxies | `[{id,name,shape,opened,radius,region_base,territories}]` — существующий публичный список галактик. |
| NavigationView.bookmarks | `[{star,target,note,label,bookmark}]` — заметки текущей галактики и доступные заметки об объектах текущей системы. `bookmark` отмечает закладку. |
| NavigationView.landing_sites | `[{id,sector,name}]` — доступные по правам площадки у текущего объекта. `id` — строка для handoff. |
| NavigationView.remaining_route | `{path,waypoints,fuel,seconds}`. Активный переход отображается через `ship.path,ship.departure,ship.arrival`. После него маршрут продолжается по `remaining_route`. `eta` — расчётная дата с учётом активного перехода, без ожидания следующих тиков. При `route_valid:false` остановите расчёт на стороне клиента. |
| NavigationView.portal_destinations | Словарь `galaxy_id → system_id` для доступных из текущей системы направлений. |
| NavigationView.capabilities | Capability для `route,cancel_route,move,rescue,portal_travel,landing`. Доступность отдельной цели проверяется в preview или при исполнении. |

### Данные для Frontend

Для двух плиток одного семейства сохраните два UUID. Пример настройки второй плитки корабля:

```json
{"id":"00000000-0000-4000-8000-000000000004","type":"ship","purpose":"owned","binding":{"mode":"fixed","ship_id":42},"settings":{"section":"equipment"}}
```

Ожидаемая ошибка для корабля другого игрока либо удалённого корабля — HTTP 404 с `{"error":"Объект недоступен"}`. Ошибка стыковки при производном складе — HTTP 409 с `{"error":"Корабль не пристыкован к станции"}`.

Ключ кеша чтений — путь и разрешённая цель. Черновики и подтверждения команд привязаны к UUID экземпляра и ID цели. При её смене отменяйте старый запрос или игнорируйте его поздний ответ. При подтверждении фиксируйте цель, параметры и ключ повтора. После успешной команды перечитывайте затронутые объекты. После перезагрузки восстанавливайте UI-настройки без повторной отправки команд. Навигация использует один снимок на корабль для двух своих подвидов.

### Согласование интеграции

Доступ к прежним операциям, рынку, добыче и карте сохраняется через их старый интерфейс. Адаптер `legacy` отображает перенесённые настройки, пока для этих инструментов нет собственного паспорта. Описания старых окон и конфигурация V1 остаются в БД. PR #17 меняет маршрутизацию, рабочие пространства и логистику, PR #16 затрагивает экспедицию и модель раскладки. При интеграции нужно сверить общие обработчики, ключи квитанций и семантику ревизий. [PR #17](https://github.com/yomezza/astrajourney/pull/17) · [PR #16](https://github.com/yomezza/astrajourney/pull/16)

Произвольное количество топлива за одну операцию, прямое перемещение оснащения как экземпляров, отдельная отстыковка без цели и перемещение между удалёнными складами не входят в серверный контракт. Для первых трёх сценариев потребуется согласованная игровая операция и проверка инвариантов, для последнего уже существует отдельная физическая доставка.
