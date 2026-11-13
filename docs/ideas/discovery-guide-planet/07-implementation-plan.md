# 07. Поэтапный план для будущей реализации

**Статус: только проект, backend ещё не разрешён.** Не коммитить сразу в main, не менять реальную БД, не выпускать релиз без решения владельца. Этапы разделены на транзакционную основу, API, frontend. Все производственные работы в отдельной feature-ветке.

## A. Первые открытия и названия

**A0. DBA + тестовая схема.** Проверить schema240 и номер следующей миграции, 5 SQL review файлов из sql/, data preflight, legacy mapping, Unicode name_key, founder mapping, конкуренцию, механизм rollback. Писать только через Cosmic migration runner. Никаких схем в Planet/Chat runtime.

**A1. События открытия.** Точечная доработка navigation.AdvancePlayer(): в tx исследования фиксировать первый system claim при личном INSERT surveys(source=survey), planet claims по newly revealed SystemSites с учётом шага полного завершения. Случаи received intelligence не учитывать. Тесты: одновременно два игрока, rollback победителя, разные discoverer для star и planet.

**A2. Переименование.** SQL registry/history, self-contained validator и request UUID, атомарный World.Commit, admin correction. Ключевые тесты: one-time used, replay, плохое имя не тратит право, founder protected, old data unaffected. Не вводить UI кнопку до серверных гарантий.

**A3. Колонии.** Отдельное имя готовой planet_colonies, current ownership ACL и corp permission, revision, cooldown, immutable history. Доработать SQL уникальности поверхности до кодинга. Не изменять существующую ownership-модель.

**A4. Интерфейс и кэш.** Встроить управление названием в существующее «Исследования», актуальные надписи в galaxy/system/Planet/colony API, ревизии snapshot, без изменения ID маршрутов, откат через feature flag. При NATS down изменения видимы при очередном read/ETag refresh.

**Gate A:** доказательство first-claim under concurrency; one rename; admin correction без ресета; не было разрыва старых маршрутов/сектора/географии.

## B. База знаний и Original C

**B0. SQL и права.** Категории, locale, статьи, ревизии, published pointer, related links, image metadata, админ роль, privacy/audit. На тестовой схеме.

**B1. Public API.** Published-only catalog/article, game-derived resource/ship/object cards, RU/EN fallback, sanitized Markdown renderer, bounded search, ETag; current dynamic values не копировать в CMS.

**B2. Admin API и редактор.** Categories tree, draft/revision, preview, publish/unpublish, link order, access rights, optimistic conflict, history restore as new revision. Сохранить общий admin menu.

**B3. Изображения.** Safe MIME/limits, реальное decoding и SVG rasterization в offline sandbox (или временный отказ для SVG, если sandbox не готов), staging/private blob + DB metadata + GC + backup, встроенный manifest со сверкой безопасных keys.

**B4. Original C UI.** Существующий Modules.open('guide') и guide-root, заменить только внутреннюю композицию на выбранный C, перенести scoped CSS, использовать реальные SVG/hologram 360° и source data, сохранить legacy guide functions; избранное/недавние scoped по аккаунту, список не прыгает вверх.

**Gate B:** публичный игрок не получает draft; неадмин не редактирует; SVG/XSS безопасны; RU/EN локаль чистая; original UI целостен; старый guide каталог не пропал; статьи публикуются без пересборки Go.

## C. Планетарный режим

**C0. Серверный возврат и SQL proof.** В текущих Planet/transfer handlers нет proximity validation. Сначала обеспечить один безопасный normal-return workflow: серверная landing позиция, proof UUID с epoch+fence, Cosmic consumption, idempotency, сохранение existing handoff/reconcile. Закрыть bypass и старым WS leave, и Cosmic POST /api/planet/return. Прописать recovery для повреждённой сцены/expired lease.

**C1. UI ModeCoordinator.** Существующий Planet renderer и WS переносится в большой PlanetHost; космический DOM и окна не уничтожаются, только hidden/inert; Fullscreen API опционален, escape не завершает визит, один WS controller.

**C2. Startup reuse.** Общий controller #startup-screen для initial auth и Space↔Planet, owner-token, retry, errors, timeout, accepted server state before hide; никакого дублирования DOM или бесконечной загрузки.

**C3. Landing interactions.** Использовать настоящий scene.landing и UI.menu, движение к объекту/near check, «Вернуться на корабль» только после разрешения Planet. Проверить восстановление F5, disconnected state, fullscreen ESC, sector transitions, shutdown.

**Gate C:** ни UI, ни прямые HTTP/WS запросы не обходят серверную близость; игрок не застревает после аварии; старый movement/A* и sector epoch продолжают работать; Cosmic окна не теряют раскладку.

## Совместимость и порядок

A, B, C архитектурно независимы; но World name labels используются и в B game cards, и в Planet, поэтому общий worldstore projection/feature flag должен быть совместим. Этап C0 требует **совместной** серверной совместимости Cosmic+Planet и UI из C1/C3: выпускать вместе или через закрытый flag, избегая периода, когда старый endpoint остаётся открытым обходом. B UI и API можно внедрять поэтапно behind flag, не изменяя existing guide до B3/B4. Все миграции additive.

## Минимально достаточные тесты

A: PostgreSQL integration против двух конкурентных claims/renames + validator + revision/world cache; B: ACL admin/public, publication history, SVG/XSS upload, Original C browser smoke с сохранением scroll; C: near/far return, direct bypass, double request, stale epoch, lost connection/F5, #startup-screen и единственный WS. После этапа — go test/vet адресных пакетов, один короткий UI smoke, только финальный набор затронутых integration тестов. Не требовать гигантский регрессионный прогон после каждого текста.

## Порядок передачи

Будущий исполнитель изучает AGENTS.md, документы 01–08, SQL, Original C branch; создаёт feature branch, уточняет номера схемы, разрабатывает через PR, не сливает и не deploy без одобрения. Архитектурный PR текущего этапа не разрешает кодить эти стадии.
