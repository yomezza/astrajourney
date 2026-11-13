# 04. Planet Mode ↔ Space Mode: единая игровая оболочка

**Проект, не реализован.** Отправная точка: `internal/server/web/features/planet/visit.js` открывает `Modules.create('planet-visit',...,1000,760)`, поддерживает WS hello/resume/scene/transition и кнопку «Вернуться в космос». `scene-actors.js` уже отображает `scene.landing` как объект с `id='landing'`, hover и hit-test; возвращение сейчас не проверяет близость. `shell/app.js` управляет существующим `#startup-screen`.

## 1. Цель: не новый frontend, а режим представления

Один браузер/одна cookie сессия, **одна** Planet WS control lease на пользователя. Космический интерфейс с окнами и их геометрией **не уничтожается**, а временно скрывается/замораживается. Planet renderer, Canvas и WS-контракт переиспользуются в fullscreen-capable `PlanetHost` без второй копии `#window-planet-visit`.

Рекомендуемый `ModeCoordinator` (новая малая frontend-модель):
```
space
  -- click landing --> transitioning_to_planet
  -- valid welcome + scene + assets ready --> planet
planet
  -- return.prepare accepted --> transitioning_to_space
  -- Cosmic return finalized --> space
transitioning_* -- retryable error --> error_recoverable
error_recoverable -- resume --> latest server-authoritative mode
```

Единый `transitionToken`/AbortController для каждой операции: новое нажатие/смена session epoch отменяет устаревшую UI задачу, **не** отменяет уже COMMIT на сервере. При гонке ответов применять только последний токен и подтверждённый `visit_id,connection_epoch,runtime_fence`; F5 всегда читает `GET /api/planet/visit`, не локальную guess-сохранённую фазу.

**Отображение:** `space` — текущий `#game`, sidebar/окна; `planet` — полноэкранная **по основному контейнеру**, возможно настоящая браузерная Fullscreen API по клику пользователя, не обязательная. Скрытие Space через class/hidden (без удаления DOM), отключить его ввод/keyboard shortcuts, suspend тяжёлые UI-обновления и космические модальные команды, но оставить серверный snapshot/cache/lifecycle по прежнему протоколу. Вернуть размеры/позиции окон из Modules без переписывания geometry. Planet HUD компактный; общий Chat при наличии показывать в разрешённом представлении, не поднимать вторую WS чата.

При ошибке Fullscreen API отказ/ESC → продолжать Planet в обычном fullscreen-like root. Выход из browser fullscreen **не** означает возвращения в космос. При logout: shutdown Planet render/WS, clear private UI, показать login; не оставлять два активных control WS.

## 2. Единый `#startup-screen`

`index.html` уже содержит `#startup-screen`, `#startup-title`, `#startup-message`, `#startup-retry` и CSS. В `shell/app.js` startupStatus/finishStartup сейчас привязаны к первому входу на сайт; после первого логина экран скрыт.

Новый `StartupScreenController` — **адаптер над этим DOM**, а не второй overlay: `acquire(owner,phase)` / `update(owner,message)` / `release(owner)` / `fail(owner,reason,retry)`. Владение экрана scoped transition token с priority: startup/auth > world switch; старое `release` не закрывает новую загрузку. Подписки, клавиатурный фокус, ошибки и кнопка retry централизованы. Локализация RU/EN через existing I18n.

Фазы:
- `initializing`: «Подключение к серверу» (прежний сценарий).
- `landing`: «Подготовка планетарной высадки», затем ticket → WS hello/welcome → scene → art/Canvas ready.
- `sector`: переходы между секторами; использовать существующий offer/confirm/welcome без сброса epoch.
- `returning`: «Возвращение на корабль», оставить экран до Cosmic `state='none'` или подтверждённого server status.
- `failed`: показать конкретную retry/return-to-safe состояния, не бесконечную заставку.

Для progress использовать **реальное количество завершённых стадий**; произвольные проценты на основе таймера не выдавать за прогресс. Timeout на каждый network stage, retry/backoff, отмена устаревшего attempt без двойного handoff.

## 3. Штатное возвращение только через посадочный модуль

Сегодня `scene-actors.js:createSceneSprites` строит объект landing на реальной позиции `scene.landing`; это семантический кандидат для взаимодействия. На Canvas click/hit-test по `id='landing'` открыть `UI.menu` с командой «Вернуться на корабль» **только при доступном действии**; если далеко — можно предложить «Подойти к модулю». Интерактивный объект не создаётся из декора: его ID/координаты должны соответствовать серверной scene/landing-site и существующей активной высадке.

**Ключевое требование: проверка на сервере**. Нельзя спрятать кнопку и оставить `POST /api/planet/return` без ограничения: клиент может вызвать его напрямую. Предложенный протокол:

1. Planet WS client `return.prepare` с UUID operation; сервер проверяет actual actor/visit, текущий sector=landing sector, active visit, runtime fence, epoch, DB session, неактивный переход, при необходимости живой ship reservation.
2. Planet проверяет `server-owned` последние позиции персонажа и фактическое место посадки из pinned scene/landing object; дистанцию считать на сервере (предлагаемая граница 3–5 игровых units, окончательная проверка с текущими физическими размерами объекта), достижимость посадочного объекта и отсутствие движения. Нельзя принимать координаты от UI в качестве доказательства. Положение записать через действующий checkpoint с epoch.
3. В короткой Planet SQL tx фиксируется `aj_planet_return_authorizations` (см. [sql/004](./sql/004-planet-return-authorizations.review.sql)): user, visit, operation, generation, connection_epoch, runtime_fence, landing_site, sector, server position, expiry (проектно 30s). Ответ `return.authorized` содержит operation UUID без claim прав.
4. Браузер вызывает **существующий** Cosmic `POST /api/planet/return` с тем же operation UUID. Cosmic под своим мировым/пользовательским lock проверяет одноразовую авторизацию в общей PostgreSQL (`FOR UPDATE`), срок, поколение, соответствие reserving ship; помечает consumed и инициирует прежнее revoke/return-pending. Повтор UUID возвращает сохранённый статус, но не делает второй refund/release.
5. Planet прекращает действующую сессию по флагу отзыва и сохраняет позицию. Cosmic завершает reconciliation, безопасно освобождает корабль. UI меняет режим только после подтверждённого `state='none'`. Авторизация не должна допустить обход через `leave`, старые `planet/return`, HTTP и восстановление.
6. Если COMMIT авторизации есть, но ответ WS потерян, повтор с тем же UUID проверяет запись. Если Cosmic получил авторизацию, но backend завис, recovery читает persisted state; не вызывать второй normal return с другим ID автоматически.

**Совместимый rollout:** SQL 004 + изменение Planet `leave`/return.prepare + Cosmic `return` + UI **одним совместным релизом** или под feature flag со строгим переходным режимом, без периода, когда UI считает return защищённым, а сервер принимает обход. Нельзя ослаблять существующие Planet visit checks и generation/revoke fences.

## 4. Невозможность застрять на планете

Обычный пользователь **не** должен получать свободную кнопку мгновенного возврата «на всякий случай». Вместо неё:

- UI при потере Planet WS предлагает `resume_visit` / retry, показывает статус от сервера; F5 восстанавливает режим, если визит active/disconnected/pending. Служба и DB не пропадают из-за смены оболочки.
- Если посадочный объект/сцена отсутствуют, geometry/sector несовместимы, подтверждённый path не достижим, Planet runtime lease истёк или визит повреждён — отдельный `recovery request`/админ интерфейс **с серверной диагностикой**. Автоматическое восстановление допустимо только при строго подтверждённом unrecoverable состоянии и existing Cosmic reconciliation/fencing; не принимать признак «застрял» от клиента как достаточное доказательство.
- Админ/система могут применять отдельное audited emergency termination по проверенной политике: actor, reason code, visit_id, generation, evidence, exactly-once transfer release. Команда не публикуется как обычный player return, не вызывает произвольное teleport или возврат ресурсов без сервера.
- Если есть активное движение/переход или старый WS epoch, `return.prepare` отклоняется с конкретной причиной и возможностью retry/resume, не очищает визит.
- При Chat/Cosmic down сохранять существующие правила недоступности и восстановления. Core NATS — не обязательный путь, все разрешения по PostgreSQL.

## 5. Что скрывать и что сохранять

| Компонент | Space → Planet | Planet → Space |
|---|---|---|
| Главная `#game`, sidebar | hidden/inert, DOM и layout сохр. | вернуть видимость и фокус |
| Cosmic Modules окна | скрыть без `destroy`, обновления тяжёлых модулей suspend | сохранить geometry/pinned и открытые окна |
| Planet renderer и Canvas | mount в `PlanetHost` один раз, asset cache переиспользовать | stop+dispose, release image references |
| Planet WS | один control connection, takeover только через existing fencing | корректно close после confirmed end |
| Chat | один общий controller/WS, разрешённые пространственные каналы обновляются по DB presence | пересчитать Planet/Sector доступ; не дублировать |
| Startup overlay | acquire phase owner | release after authoritative confirmation |
| UI preferences | остаются account-scoped | resume и no loss |
| URL/browser refresh | не является source of truth | `/api/planet/visit` определяет активный режим |

## 6. Приёмка

- Высадка открывает настоящий Planet Mode без Cosmic поверх, без второго window titlebar; движение/A*/переход секторов прежние.
- Landing sprite соответствует server landing location, click открывает `UI.menu`, далеко — only approach, server `return.prepare` rejects.
- Прямой HTTP `planet/return` без authorization **403/409**, прямой старый `leave` не обходит proof.
- Двойной UUID/cross-visit/stale epoch/expired proof → never double release; entity on unavailable scene does not trap player permanently.
- Ctrl+R/F5, исчезновение WS, отложенные network replies, нажать дважды, browser fullscreen ESC, logout во время loading и reload после return pending — без потери корабля.
- Один `#startup-screen`, действующий login startup не сломан; RU/EN и error retry, таймаут не бесконечен.
- Сохранены размеры окон, единственный Planet WS, отдельный Chat WS; нет mass handler leaks, сохранено настоящее управление через Planet.
