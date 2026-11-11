# Единый командный терминал — техническая архитектура изолированного Frontend-прототипа

**Статус:** рекомендации фронтенд-разработчику, **не разрешение на production**. Дата аудита: 2026-11-11. Проверенный main: `94ed9da4b204776c02fb84e347d7be27adbbcfb5`.

**Основа:** [README.ru.md](./README.ru.md) — исходная концепция Astra Journey: 1–10 пространств на аккаунт, имя 1–32 видимых символа, независимые экземпляры модулей и корабельные контексты. Данный документ не меняет правила концепции и описывает только **изолированную демонстрацию в scripts/frontend-generation/**.

## 1. Итоговое решение

**Не изменять original Modules и не внедрять workspace-aware состояние в production.** В демо создать слой `PilotWorkspaceController`, который оперирует устойчивыми `workspaceId` и `moduleInstanceId`, **вызывает существующий `Modules.create(uniqueWindowId,...)`** и подставляет отдельное mock-представление. Существующие production-модули `createFleet()`/`createMining()` **нельзя монтировать несколько раз без рефакторинга**, потому что они используют `Session.data.expedition`, фиксированные корни #window-ship/#window-cargo/#window-research, singleton `Game.performExpedition` и общие переменные фильтров.

Минимальная демосцена: две вкладки «Добыча — 1» / «Добыча — 2», два разных mock-корабля **с одинаковым названием «Пилигрим»**, идентификаторами `41` / `82`, по одному окну «Корабль», «Трюм», «Добыча», плюс независимая «Справка». Это **тестовые ID**, не подтверждённые реальные корабли аккаунта и не текущие fixture IDs (в существующем mock/data.py используются 13 и 14).

**Инвариант:** открытие/выбор/переключение пространства меняет только локальный UI, **никогда** не изменяет `Session.data.expedition.ship`, `Session.data.fleet` или серверное `ships.active`, не вызывает `select_ship` и не отправляет реальные игровые команды.

## 2. Точная модель ID и изоляции

В `internal/server/web/ui/windows.js` есть один `windows = new Map()` с ключом `id`. `Modules.create(id,...)` при повторном id **переиспользует существующее окно**, а `Modules.open/close/destroy/title` и `moduleopen/moduleclose/moduleresize` также используют этот id. Положение и размер сохраняются через `Preferences.write("module:"+id,...)` с полями x,y,w,h,pinned. Функция `Modules.link("ship","cargo")` относится только к обычным глобальным ID и не является связыванием корабельного контекста.

Поэтому демонстрационный адаптер создаёт уникальный ключ **окна-экземпляра**, например:

```js
const windowId = `ptdemo-u${accountId}-w${workspaceId}-i${instanceId}`;
const root = Modules.create(windowId, title, 440, 320, { private: true });
Modules.open(windowId);
```

- `moduleType` = `ship | cargo | mining | guide` — тип и источник представления, **не** ключ окна.
- `instanceId` — устойчивый UUID; `workspaceId` — UUID; `accountId` — ID игрока из mock-сессии. Для DOM/селекторов только безопасные numeric/UUID части, никаких пользовательских названий в ID.
- Одновременно существуют произвольные типы окон в разных рабочих пространствах, но не имеют общего module ID. В первой пробе можно ограничиться одним экземпляром каждого типа **в пределах одной вкладки**, не ограничивая будущую модель.
- Чтобы не держать десятки тяжёлых окон, демо при переключении **сохраняет состояние и закрывает/уничтожает DOM-окна текущего пространства** (`Modules.close(id)`, затем `Modules.destroy(id)`), а выбранное пространство создаёт заново. `Modules.destroy` удаляет DOM и registry entry, но **не удаляет** `Preferences["module:"+id]`; при повторном `create` старые x/y/w/h восстанавливаются.
- При удалении экземпляра или пространства удалить соответствующие `module:<uniqueWindowId>` ключи через `Preferences.remove`, иначе остаются сиротские локальные раскладки.
- Контроллер прототипа подписывается на `moduleclose` и `moduleresize` **только для своих pt-demo IDs**; события обычных окон не обрабатывать и не изменять их поведение. Поскольку в windows.js нет отдельного события «drag finished», надёжную геометрию при переключении восстанавливать из сохранённого `module:<id>`, а перед переключением фиксировать конечный rect при необходимости.
- Демо должно уметь закрывать и повторно добавлять экземпляр через каталог. «Закрыть окно» удаляет его из сцены только по осознанному выбору UX (предложено: крестик закрывает/убирает экземпляр, само игровое действие не отменяет). Меню «Убрать из пространства» — отдельная операция удаления дескриптора.
- Existing `pinned` — это запрет drag/resize; **это не fixed ship binding и не workspace pin**. Связывание с кораблём живёт только в дескрипторе модуля; существующую кнопку ⌖ не переименовывать глобально.

## 3. Состояние и разрешение контекста корабля

Рекомендуемый сериализуемый формат `version:1` (пример без runtime данных):

```json
{
  "version": 1,
  "activeWorkspaceId": "ws-uuid-1",
  "defaultWorkspaceId": "ws-uuid-1",
  "workspaces": [
    {
      "id": "ws-uuid-1",
      "name": "Добыча — 1",
      "shipId": 41,
      "instances": [
        {"id": "i-ship-1", "type": "ship", "binding": {"mode": "fixed", "shipId": 41}, "uiState": {"tab": "overview"}},
        {"id": "i-cargo-1", "type": "cargo", "binding": {"mode": "workspace"}, "uiState": {"category": "all", "sort": "name"}},
        {"id": "i-mining-1", "type": "mining", "binding": {"mode": "workspace"}, "uiState": {"filter": "all"}},
        {"id": "i-guide-1", "type": "guide", "binding": {"mode": "none"}, "uiState": {}}
      ]
    }
  ]
}
```

В действительном JSON использовать UUID из `crypto.randomUUID()`, а не условные `ws-uuid-1`. Допустимое имя: Unicode NFC, `trim()` не пустой, 1–32 **графемных кластера** (если поддержан `Intl.Segmenter` с `granularity:'grapheme'`; fallback явно тестировать, не считать количество байтов). Имеется ровно 1–10 workspaces. `type`, `binding.mode`, `shipId`, `uiState` валидируются по allowlist, лишние ключи/недостоверные версии не выполняются как код.

Функция разрешения контекста (одна для всех mock-представлений):

```js
function resolveShip(instance, workspace, ownedShips) {
  if (instance.binding.mode === "none") return {ship: null, status: "unbound"};
  const id = instance.binding.mode === "fixed"
    ? instance.binding.shipId
    : workspace.shipId;
  if (id == null) return {ship: null, status: "choose_ship"};
  const ship = ownedShips.find(s => s.id === id);
  return ship ? {ship, status: "ready"} : {ship: null, status: "unavailable"};
}
```

Ключевое требование: **источник данных — одна mock-карта кораблей keyed by numeric ID**, название только label; один идентичный текст «Пилигрим» не делает корабли идентичными. При смене `workspace.shipId` перерисовывать **только** экземпляры с `mode='workspace'` активной вкладки. Fixed остаются на выбранном ID, none игнорируют ship. Если объект удалён или перестал быть доступным → «Корабль #82 недоступен» с предложением выбрать другой; **не переключать тихо** на #41/первый корабль. В inactive workspace дескрипторы не менять.

Видимая подпись каждого окна: `Корабль — Пилигрим (#41) · фиксирован` / `Трюм — Пилигрим (#82) · следует пространству`. Разработчик не должен подменять серверный active ship при смене вкладок и вызывать `POST /api/workbench action=select_ship` даже в демонстрации как реальный запрос.

## 4. Сохранение: 1 аккаунт = 1 пространство ключей

В `core/preferences.js` уже есть `Preferences.key(feature,{account,instance,version})`, `read/write/remove`; она использует JSON localStorage и volatile fallback при запрете хранилища. Это готовая основа.

- Один основной ключ: `Preferences.key("pilot-terminal", {account: accountId, version: 1})`. Аккаунт брать из **текущей** mock session `Session.data.state.user.id` после mock login; не из текста имени игрока/URL. Demo logout/account switch → немедленно убрать DOM окон прежнего аккаунта, очистить in-memory data и загрузить только новый ключ. Демо может предоставлять переключатель mock-аккаунтов, но он не должен переопределять настоящую сессию.
- Основной JSON хранит: список до 10 пространств, порядок, имена, active/default, `workspace.shipId`, дескрипторы модулей, `binding`, необязательный `uiState` (filter/tab/search; **не** secrets). `uiState` ограничивать безопасными типами/длинами; избегать хранения текста Private/Chat и целых ответов API.
- Геометрию x/y/w/h/pinned для каждого экземпляра хранит **существующий** `Modules` в `Preferences["module:"+syntheticWindowId]`, а не в дублирующемся втором объекте; syntheticWindowId уже включает accountId, wsId, instanceId. При смене/закрытии Modules сохраняет rect автоматически.
- Сохранять основной JSON после изменения/создания/удаления/переименования/перестановки вкладок, выбора корабля, фиксации/разфиксации ship binding, изменения фильтра/вкладки; debounce порядка 150–300 мс для ввода и немедленно перед switch/pagehide. Для старого module geometry использовать существующий `save`.
- На восстановлении валидировать `version`, account, предел 10, имена, уникальность workspace/instance IDs, типы, binding; повреждённое/неизвестное состояние → безопасное **новое пустое пространство** с предупреждением, не затрагивать старые production настройки.
- На `localStorage` quota / disabled browser storage: UI работает в памяти, но показывает «Сохранение только до закрытия вкладки». `Preferences.write` сам скрывает ошибку и использует volatile fallback, поэтому **доказать физическое сохранение** без дополнительной проверки браузерного storage нельзя. Не заявлять persisted, если localStorage запрещён.
- Никакой серверной синхронизации, миграций, cookie, токенов, игровых ресурсов или финансового состояния в storage.
- Не использовать существующий `Preferences["module:ship"]` или `module:cargo` для демо: это пользовательская production раскладка; все новые ID имеют префикс `ptdemo-`.

## 5. Подтверждённые ограничения существующих модулей и API

| Тип | Что можно безопасно повторно использовать | Что нельзя просто копировать | Решение для прототипа |
|---|---|---|---|
| «Корабль» `features/fleet/index.js:renderShip` | `GET /api/fleet` возвращает массив owned ships с ID; визуальные UI.bar, UI.row, терминальные стили, информацию о fuel/cargo/position из ship | `renderShip()` читает `d().ship`, меняет `Modules.title("ship")`, вызывает `select_ship`, `Game.performExpedition`, `Modules.link("ship","cargo")`, рендерит в фиксированном root | Лёгкий `ShipPreview({ship})`; только mock read, индикатор ID и binding |
| «Трюм» `features/fleet/index.js:renderCargo` | Поле `ship.cargo`/spec и существующие `UI.bar`, tiles/стили `.cargo-grid` | `d().ship`, `cargoCategory`/`cargoSort` находятся в singleton closure `createFleet`, DnD `Logistics` и действия продажи/заправки адресуют глобальный active | `CargoPreview({ship, uiState})` с local sort/category, drag/drop отключён или имитирован без игровых последствий |
| «Добыча» `features/mining/index.js:renderResearch` | `workbench.mining` содержит записи с ship_id; для mock можно использовать list of site/quotas и ship ID | `d().system`/`d().ship`/`d().job`, кнопки `performExpedition(move/mine/cancel_mine)`, глобальные `FormState` и research root | `MiningPreview({ship, siteData, uiState})` фильтрует mock records по shipId, с fake action log, **не** запускает Game |
| Независимая «Справка» | Терминальные стили, `UI.row`, статический текст, семантика `guide` | Производственный `features/guide/index.js:renderGuide` также читает `d().resources` и общее workbench; нельзя переиспользовать его root напрямую | Static `GuidePreview()` с `binding.mode='none'` |
| Chat/Notifications/Dialogs | Existing `mock/chat-native.js`, existing global `Modules.open("notifications")`, `UI.confirm` | Создание по одному новому Chat WS / уведомлениям в каждой вкладке и сохранение диалогов как user workspace window | Один общий слой на весь терминал, не входит в `instances` и не уничтожается при switch |

### API: просмотр ≠ действие

Точные server findings по актуальному коду:

- `GET /api/fleet` (`internal/terminal/fleet.go`) возвращает `ships[]` данного игрока; для выбранного ID можно **локально выбрать запись** и отобразить её. Серверная команда выбора активного ship не нужна.
- `GET /api/expedition` (`internal/terminal/expedition.go`) отвечает **только по активному ship** через `fleet.EnsureShip(ctx,tx,user)`. Не использовать результат как данные другого ship. Отдельного `GET /api/expedition?ship=82` из прочитанного кода не следует.
- `POST /api/expedition`: поле `ship` действительно есть, **но** `if in.Ship>0 && in.Ship!=ship.ID` → HTTP 409 «Активный корабль изменился. Повторите действие». Значит, передача ship=82 не превращает expedition в endpoint произвольного корабля. `move/mine/refuel/sell/cancel_mine` из оригинальных экранов остаются active-expedition действиями.
- `POST /api/workbench` (`internal/terminal/workbench.go`): `commands.Command.Ship` поддерживается; при положительном ID сервер проверяет `ships.user_id == actor`, затем `fleet.ReadShip`. Это **частичная поддержка явного ship**. Действия отличаются по семантике: `rename_ship` адресуется выбранному `s`, `select_ship` **изменяет глобальный active корабль**, другие workflows могут использовать user/global context. Для production сначала составить action-by-action allowlist и проверить каждый backend handler; не автоматически разрешать все команды workbench из workspace.
- Текущий frontend `features/workspace/index.js`: `Game.performExpedition` строит `{ship:d().ship.id,...body}`; `createFleet` и `createMining` вызывают эту функцию. `select_ship` из UI — серверная команда, а не локальная смена просмотра.
- `GET /api/workbench` содержит research/mining данные, в SQL-источнике есть `ship`/`ship_id`; прототип может строить **mock filtered view**, не предполагая, что весь реальный workbench пригоден для каждого выбранного корабля.
- Никаких настоящих POST из демонстрационных окон. Даже если backend однажды даст безопасную explicit-ship команду, в этом эксперименте mock-кнопки показывают «Демо: операция для корабля #41; реальные команды отключены», не вызывая fetch/api/act/Game.performExpedition.

**Особый риск:** команда `select_ship` в `internal/navigation/commands.go` выполняет UPDATE ships.active для игрока. Её нельзя вызывать при переключении вкладки «ради отображения», иначе второй пользовательский workspace реально сменит корабль всей игровой сессии.

## 6. Где собирать пробу: только scripts/frontend-generation/

Использовать существующую изолированную среду из `scripts/frontend-generation/README.md`:
- Python mock HTTP server `server.py`, fictional API `mock/data.py`, `mock/ui-overlay.js` как безопасное место интеграции с original-web.
- Запуск `python launch.py --offline` (при наличии подготовленного mirror) или `python launch.py`, локальный `http://127.0.0.1:8765/`.
- Предложенные **новые** файлы внутри демосреды:
  - `mock/pilot-terminal/terminal.js`: WorkspaceController, window ID registry, state transitions и запись Preferences.
  - `mock/pilot-terminal/modules.js`: фабрика `ShipPreview/CargoPreview/MiningPreview/GuidePreview` на явно переданных `ship`/instance, с использованием существующих UI styles/helpers без импортов originals createFleet/createMining.
  - `mock/pilot-terminal/fixtures.js`: локальные mock корабли #41/#82 с одинаковым названием «Пилигрим», sites, cargos, mining jobs. **Не переписывать** общие fixture `mock/data.py`, чтобы не испортить другие демонстрации.
  - `mock/pilot-terminal/terminal.css`: только стили вкладок/панели демо, переиспользование `tokens.css` и `terminal.css`.
  - `tests/test_pilot_terminal.py` и/или доступный Playwright/browser smoke test для переключения/перезагрузки.
- Добавить **только в mock оболочку** (например, маленькая точка подключения в `mock/ui-overlay.js`) кнопку «Командный терминал — проба» либо запуск по `?pilot-terminal=1`; не менять `internal/server/web/`, `original-web/`, `ui/windows.js` или game routes. При обычном открытии demo без флага прежние studies/chat/news продолжают работать.
- `Modules` из `/ui/runtime.js` уже инициализирован стандартным bootstrap, к нему обращаться только после его setup. При возвращении в обычную демоверсию уничтожать только pt-demo окна, не трогать прочие `Modules`.
- Не вводить React/Vue, второй оконный менеджер, новый API/DB или систему авторизации.
- `scripts/frontend-generation/README.md` указывает на исходную копию `v0.23.0`; актуальный source audit выполнен по main. При переносе стилей/функций учитывать возможную разницу между snapshot `original-web` и текущим `internal/server/web`; **не** запускать опасное обновление production.
- Существующие Chat/Notifications/Modal dialogs — **общий слой** терминала. Chat уже существует в mock как единственная плавающая/home-docked панель; не создавать новые подключения или копии Chat DOM. Notifications — одна shared account panel, не per-workspace; `UI.confirm`/toast — временный UI и не сохраняется в layout. Если демо workspace показано поверх главного экрана, общие окна либо остаются доступны, либо открываются поверх, но не клонируются.
- При необходимости сделать визуальную оболочку с вкладками и слотом catalog, **использовать** UI/menu/tab patterns/стили игры, но не писать отдельный framework. Окна остаются обычными floating Modules windows с scoped ID; позже можно оценить dock/grid отдельно.

## 7. Поведение контроллера: алгоритмы

1. **init(account):** прочитать `Preferences.key(...)`, валидировать v1; если нет valid состояния — создать 1 пустое пространство. Установить общий каталог типов; загрузить mock fleet map один раз; никакого изменения `Session.data.expedition`.
2. **openWorkspace(wsId):** сохранить `uiState` активных экземпляров, вызвать `Modules.close`→`Modules.destroy` для окон прежней вкладки, сохранить master; установить activeWorkspaceId; восстановить только экземпляры новой вкладки уникальными window ID и uiState, вычислить resolved ship. Недоступный ship — placeholder.
3. **setWorkspaceShip(shipId):** проверить ID по owned mock fixtures; записать только workspace.shipId; обновить только workspace-bound modules в **текущей** вкладке. Fixed windows и иные вкладки остаются прежними.
4. **setInstanceBinding(instanceId, mode, fixedShipId?):** validate `mode`; для fixed выбрать owned ship ID; для none разрешать только независимый тип; сохранить, перерисовать один экземпляр и обновить badge.
5. **add/remove/duplicate/rename/reorder:** валидировать 10-space/32-grapheme ограничения. Duplicate workspace создаёт **новые instance IDs** и новые window IDs, копирует binding/filter state, но не копирует серверное состояние; duplicate geometry при необходимости скопировать из existing `module:<oldId>` в new namespaced key после проверки. Удаление последней вкладки запрещено.
6. **captureState/restoreState:** каждый mock module сохраняет только своё UI состояние (cargo filter/category, mining active filter, ship view tab); события при переключении не вызывают `Game.performExpedition`, fetch, session mutation. Dirty input и scroll в первой пробе опциональны, но не должны превращаться в игровые данные.
7. **restore/focus:** окно создаётся только при входе в пространство; если пользователь выбрал из catalog уже существующий тип, сфокусировать это окно вместо второго (для первого прототипа). Ограничение одного instance per type in same workspace — UX режим v1, не ограничение идентификационной схемы.
8. **logout/account switch:** уничтожить pt-demo окна и отпустить listeners, затем загрузить новый user-scoped storage; не переносить состояние другого пользователя в текущий UI. При физически отключённом localStorage демонстрация остаётся рабочей в RAM.

## 8. Риски и условия приёмки

| Риск | Проверка / решение |
|---|---|
| Два одноимённых корабля смешиваются | Везде отображать id #41/#82, фильтровать по id, не по name |
| Повторное Modules.create() перезаписывает другое окно | Unique `ptdemo-u<account>-w<uuid>-i<uuid>`; две вкладки открывают одинаковый type с разными IDs |
| Неправильная команда с корабля #82 | В демо no real POST/fetch; mock action ledger помечает resolved shipId, active ship не меняется |
| Глобальный `Session.data.expedition` перетирает карточки | Ни один mock renderer не импортирует originals `createFleet/createMining`; вся data через явный ship param |
| Меню «Закрепить» ошибочно трактуется как fixed binding | Отдельные UI controls и названия для window pin и ship binding |
| Потеря расположения при F5 | Synthetic IDs неизменны, `Modules` сохраняет module keys по account/ws/instance; master сохраняет descriptors |
| Неподходящий/удалённый корабль | «Недоступен» и ручной выбор нового; no silent fallback |
| Старый account виден новому | Account-scoped key + lifetime cleanup on mock logout |
| 10 вкладок перегружают DOM | Монтировать только активное пространство; bounded UI updates |
| Обычный demo/chat/news сломан | Точка подключения только под флагом или явной кнопкой; smoke tests существующей оболочки |
| Локальное хранилище не работает | Понятное предупреждение, RAM fallback |
| Грубый Unicode-счётчик режет эмодзи | Intl.Segmenter для 32 видимых графем; протестировать emoji и RU/EN |
| Разные позиции после resize viewport | Existing Modules clamp, проверить small screen; при выходе из narrow viewport не разрушать layout |

**Минимальные acceptance tests для фронтендера:**

- PT01: Две вкладки и два объекта с одним названием «Пилигрим»: #41 и #82.
- PT02: В каждой по «Кораблю», «Трюму», «Добыче», «Справке»; нигде чужого id/ cargo/job.
- PT03: В первой вкладке поставить context #82 — только её `workspace`-модули перестроились; фиксированный #41 прежний; вторая вкладка неизменна.
- PT04: Двигать/изменять размеры двух окон, переключить вкладки, F5 → восстановить для каждой свою геометрию и UI фильтры.
- PT05: Создать 10 пространств; 11-е запрещено с сообщением; имя из 33 графем запрещено, имя из пробелов запрещено, последнее пространство не удаляется.
- PT06: Дублировать/переименовать/переставить пространство — distinct instanceIds, без скопированных shared references.
- PT07: Mock ship #82 исчез → только его fixed/workspace карточки показывают «Объект недоступен», никакой автовыбор.
- PT08: Mock action в «Добыче» показывает цель #41 или #82, но **не** вызывает реальные POST/api/workbench/expedition и не меняет server active ship.
- PT09: Переключить mock account — полный уход чужих окон и настроек из UI; возврат возвращает только свои preference keys.
- PT10: Chat, notifications, confirmations существуют **по одному** на весь терминал; обычная demo оболочка работает без флага.
- PT11: Reload и localStorage disabled, browser zoom/mobile, клавиатура/focus, два одинаковых окна разных типов/вкладок.
- PT12: Проверить origin production: `internal/server/web` не изменён ни одной строкой, Go/SQL/игровые API также не менялись.

## 9. Что не входит в прототип и остаётся для будущей production-версии

**Только демо сейчас:**
- WS-scoped instance IDs, 2–10 вкладок, localStorage, визуальная раскладка и mock read-only Ship/Cargo/Mining + Guide, один global Chat/notifications/dialogs.
- Mock buttons, no real game mutation; нет серверной аутентификации рабочего пространства, новых endpoint, миграций, синхронизации между устройствами, второго WS чата.
- Упрощённый catalog из четырёх модулей, не переносить целиком большое sidebar меню.

**Будущий production-дизайн (не выполнять):**
- Выделение экземпляра/контекста в factory constructors `createFleet/createMining`, явные `shipId` в безопасных read API и whitelist per-action в command handlers; аудит `GET /api/expedition` и `POST /api/expedition`.
- Расширение `Modules` либо адаптер с контрактом events/linking без конфликтов между account/workspaces, владелец identity для всех окон; возможность нескольких одинаковых типов в одном workspace.
- Политика shared окна/слоя, миграция старых `Preferences["module:ship"]`, реальное сохранение вкладок/фильтров после обновления игры, optional cross-device sync **только после решения владельца**.
- Реальное auth/permissions, session lifecycle, orphan object, stale data, API concurrency и server-side validation; transactional semantics игровых команд остаются на Go.
- Оптимизация тяжёлых карт/рендереров и жизненного цикла module instances (pause/resume/dispose).

## 10. Итог для исполнителя

**Сделать:** независимый frontend-only интерактивный POC в `scripts/frontend-generation/`, с повторным использованием неизменённого `Modules` и общего UI, с двумя разными одноимёнными кораблями #41/#82; записать короткий отчёт со скриншотами/тестами и перечнем проблем.

**Не делать:** переносить production screens буквально с global Session, вызывать `select_ship`, выполнять реальные игровые POST, менять в GitHub `internal/server/web`, Go/SQL, объявлять полноценную production готовность.

После принятия прототипа владельцем — отдельно планировать production миграцию. На нынешнем этапе frontend-разработчику достаточно этой спецификации и исходного README для реализации безопасной пробы.
