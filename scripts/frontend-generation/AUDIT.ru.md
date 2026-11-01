# Исправление v3 — интеграция Chat Service и прогресс загрузки

Раньше пункт «Чат» загружал через iframe целую самостоятельную демонстрационную страницу (с собственной главной, sidebar и header), поэтому главная дублировалась. В новой версии убрана и лишняя оконная оболочка: `#chat-floating` монтируется прямо в документ, содержит один `#chat-root` с собственным заголовком и не создаёт `Modules.create('chat-v13-mock')`. При закреплении на главной тот же узел переносится под показатели пилота. Встроенный исходный HTML-экран, содержащий свою главную, остаётся архивной резервной копией.

Кнопка «Чат» добавляется в исходное левое меню первой в нижнем блоке. Переключение «▣ На главную» / «↗ В окно» перемещает единственный `#chat-root` между `.module-content` и `#chat-home-slot` под `.overview-stats`; режим dock занимает 50% ширины и высоту 825px. Floating управляется **оригинальным** `Modules`: кнопки pin/max/close, перемещение, размеры. Отдельный второй оконный менеджер для чата отключён. На `moduleclose` и `signedout` состояние чата синхронизируется, чтобы не показывать закрытый/устаревший экран.

`prepare.py` теперь выводит в консоли процент загрузки ZIP, МБ полученных данных и ход распаковки 152 файлов. При `--from` отображается прогресс локального копирования. Тесты: 20 unittest (включая инвариант «только чат без iframe») и сценарии Chromium 1440×900, 390×850 на DOM-оболочке, моделирующей Modules. **Полная браузерная проверка на реальных оригинальных 152 web-файлах ещё не проводилась.**

---

# Cosmosimulator UI Mirror — отчёт о переносе (01.11.2026)

## Проверенный источник

- GitHub: https://github.com/yomezza/cosmosimulator
- Последний опубликованный релиз, подтверждённый GitHub Releases: **v0.23.0**, опубликован 31.10.2026.
- Дерево исходников, использованное для проверки: коммит `31e849aa600bfd272d5b1f2f3a0adddcb00f3133` (main, без изменений исходного web относительно релиза).
- Рекурсивная инвентаризация `internal/server/web/`: **152 файла, 16 710 930 байт** — 89 JS, 33 SVG, 25 PNG, 2 CSS, 2 JSON, 1 HTML.

## Статус выполненной работы

| Компонент | Что есть в оригинале | Что есть в пакете v3 | Реальная проверка | Статус |
|---|---|---|---|---|
| SVG кораблей | 15 файлов на верхнем уровне `internal/server/web/` | **15 оригинальных SVG** в `embedded-original-assets/` | Все 15 Git blob SHA совпали | **Завершено** |
| SVG объектов | 17 файлов `object-*.svg` | **17 оригинальных SVG** в `embedded-original-assets/` | Все 17 Git blob SHA совпали | **Завершено** |
| Favicon | `favicon.svg` | Оригинальная копия | Git blob SHA совпал | **Завершено** |
| PNG планет, поверхности и посадки | 25 файлов (`planet-art/`, `surface-ground-*`, `planet-landing-core.png`) | Список и обязательная проверка в `graphics-inventory.json`, загрузка при `prepare.py`; **файлы ещё не вложены** | Отсутствие отмечается подготовкой | **Ожидает исходников** |
| Основной HTML/CSS/JS | Оригинальные `index.html`, `bootstrap.js`, `core/`, `ui/`, `features/`, `styles/`, RU/EN | Подготовка `prepare.py` переносит **все 152** исходных файлов из GitHub или локального checkout в `original-web/` без изменения | Сверка количества и размера при подготовке; здесь исходники не установлены | **Подготовлено, не проверено в браузере** |
| Оригинальные окна `Modules` | `ui/windows.js` | Исходная реализация используется после подготовки | Проверка вызовов по исходникам, не E2E | **Непроверено** |
| Компоненты меню, вкладок и picker | `ui/menu.js`, `ui/tabs.js`, `ui/combobox.js`, `ui/tooltip.js` | Исходники устанавливаются без адаптаций | Требуются браузерные регрессии после установки | **Непроверено** |
| Галактическая карта/Canvas | `features/maps/galaxy.js`, `features/objects/visuals.js` | Подлинный JS после `prepare.py`; фиктивная топология из `mock/data.py` | Не проверена полноценная Canvas-сцена | **Непроверено** |
| Планетарная сцена | `features/planet/` и оригинальные PNG | JS загружается; PNG требуются из репозитория | Не проведён E2E переходов и графики | **Непроверено** |
| Backend-адаптер | Go-сервисы и PostgreSQL в оригинале | Локальный `server.py`, `mock/data.py`; **никаких реальных запросов** | 7 тестов API, плюс независимые SVG/галерея тесты | **Частично** |
| Чат v13 | Ранее согласованный, НЕ релизный Chat Service | `prototype/chat/` (раздельный код), резерв `prototype/approved-chat-v13.html` | SHA резервной копии проверен; E2E полной интеграции ещё нет | **Сохранён** |
| Исследования v13 | Ранее согласованный редизайн | `prototype/studies/` + 7 звёздных SVG + резерв `prototype/approved-studies-v13-offline.html` | SHA резервной копии проверен; E2E с оригинальным окном ещё нет | **Сохранены** |
| Реестр программно создаваемых UI | Сложная DOM/Canvas/SVG-графика в JS | `ui-registry-scan.py` сканирует реальные исходники после подготовки и пишет `UI-REGISTRY.json` с файлами и строками | В этом окружении без web-файлов не запускался | **Инструмент готов** |

## Инвентаризация SVG (в архиве, оригинальные байты)

**Корабли, 15:** argus, ark, astra, atlas, crucible, forge, hermes, iris, leviathan, lightning, pilgrim, titan, vesper, wanderer, zephyr.

**Космические объекты, 17:** object-asteroids, object-black-hole, object-desert, object-garden, object-gas, object-ice-field, object-icy, object-lava, object-rocky, object-station; звёзды `object-star-o/b/a/f/g/k/m`.

**Проверка подлинности:** `python check_embedded_assets.py`; сравниваются SHA-1 Git blob для всех 33 файлов. Файлы сгенерированы по алгоритмам `scripts/generate_ship_holograms.py`, `scripts/generate_object_holograms.py` и `docs/design/ship-holograms/generate.py`. Точные оригинальные SVG совпадают с GitHub — это не иллюстрации и не перерисованные альтернативы. Сохранены 16-секундная анимация геометрии, свечение и эффекты тяги.

## Инвентаризация PNG (пока требуется установка)

**`planet-art/` — 18 файлов:** bush_sparse, colonist, colonist_8dir_atlas, footstep_dust, grass_dry, grass_tuft, move_blocked, move_target, rock_flat, rock_large_cracked, sector_boundary, selection_ring, shore_wet, tree_old, tree_young, water_deep, water_ripple_overlay, water_shallow.

**Корневые PNG — 7:** `planet-landing-core.png`; `surface-ground-{dry,grass,rock,sand,snow,volcanic}.png`.

Полные имена и категории — в `graphics-inventory.json`. **Ни один PNG не заменён синтетическим**, поскольку требования запрещают понижение качества или подмену оригинала. Эти файлы переносит `prepare.py` из оригинального каталога в момент установки.

## Каталог окон и источников оригинала

В `features/shell/navigation.js` зарегистрированы базовые окна: `galaxy`, `ship`, `cargo`, `research` (**Добыча**), `market`, `corporations`, `profile`, `settings`, `users`, `gameplay`, `galaxies`, `adminchanges`, `notifications`, `guide`, `tasks`, `finance`, `benchmark`, `services`, `portal`. Отдельный модуль `studies` создаётся `features/research/index.js`. Динамически создаваемые окна, плавающие меню и вспомогательные UI — проверять отдельно по реестру.

- **Навигация и оболочка:** `index.html`, `bootstrap.js`, `features/shell/`, `ui/windows.js`.
- **Корабль/трюм/верфи:** `features/fleet/`, изображения из `argus.svg`–`zephyr.svg`; в mock исправлен путь модели `ship-pilgrim.svg` → `pilgrim.svg`.
- **Добыча:** `features/mining/`, workbench/expedition DTO.
- **Рынок/перевозки/финансы:** `features/markets/`, `features/inventory/`, `features/finance/`.
- **Корпорации:** `features/corporations/`; роли/разрешения в рабочей игре серверные, здесь тестовые.
- **Автоматизация/задания:** `features/automation/`.
- **Карта/планеты:** `features/maps/`, `features/planet/`, `features/objects/visuals.js`; **Canvas нельзя заменить статической картинкой**.
- **Администрирование:** `features/administration/`; не все серверные DTO доступны в mock.
- **Чат/Исследования:** отдельно изолированы и сохранены, замена двух согласованных UX старым релизом запрещена.

## Как проверять после подготовки

1. `python prepare.py` (или `--from <путь>` / `--archive <скачанный ZIP>`).
2. `python prepare.py --check-only` — строго **152/152 файла, 16 710 930 байт и 58/58 ресурсов**, все корабельные SHA должны совпасть. При несовпадении утилита выдаёт ошибку, не заявляет готовность.
3. `python check_embedded_assets.py` — проверка вложенных SVG по SHA.
4. `python ui-registry-scan.py` — построение исходного реестра динамических UI-фабрик.
5. `python -m unittest discover -s tests -v` — локальные unit/HTTP тесты.
6. `python server.py`, открыть `http://127.0.0.1:8765/` и галерею `http://127.0.0.1:8765/demo/assets/`.
7. Проверить каждый модуль вручную и через браузерную автоматизацию: открытие, действия, отсутствие ошибок Console/Network, адаптивность, GIF/SVG-анимации, правильные PNG-ресурсы.

**Важное ограничение:** тесты HTML/CSS оригинального клиента здесь пока не выполнены, поскольку в рабочей среде недоступна загрузка архива GitHub. Это **не** полностью пройденная приёмка всего frontend. Для ряда модулей дополнительные fake DTO и моделирование транзакций ещё потребуются.

## Предложения (не применены)

Сформировать визуальный реестр UI-элементов с адресацией «исходный модуль → окно → конкретное применение компонента». Разделить в следующих версиях мок-сценарии по ролям, ошибкам, состояниям загрузки и сетевым отказам. Оптимизацию самих оригинальных CSS не выполнять до согласования владельцем.
