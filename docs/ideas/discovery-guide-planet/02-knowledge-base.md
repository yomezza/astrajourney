# 02. База знаний пилота в PostgreSQL

**Статус:** проект. **Визуальный эталон — только Original C** на ветке `frontend/pilot-guide-prototype`, каталог `docs/ideas/pilot-guide-prototype/original-c/`. Изучены README, FUNCTIONALITY, HANDOFF, STYLE_GUIDE и исходники index.html/app.js/styles.css/fixtures.js/hologram.js.

## 1. Что реально есть и что нужно добавить

В `internal/server/web/features/guide/index.js` существующий `createGuide()` формирует категории «Ресурсы / Космические объекты / Корабли / Техническая информация» из `d().resources`, `w().ship_classes`, `objectArticles`, таблицы стажа и текущих игровых API. Он использует `objectHologram`, `shipStats`, `filterShipyards` и существующие `Modules`.

Original C — **локальный mock**, не CMS: две панели, крупный поиск, вкладки «Все темы / Избранное / Недавние», категории-chip, выбор статьи, связанные материалы, предыдущая/следующая, восстановление прокрутки, SVG 360°. `fixtures.js` содержит демоматериалы, а избранное/история — RAM вкладки. `hologram.js` — процедурный эталон, а не реальные SVG игры. Не импортировать fixtures как реальный официальный контент.

## 2. Модель контента

Рекомендуется **Markdown без raw HTML** как формат редактора: безопасный серверный Markdown parser с allowlisted AST (paragraph, heading, lists, links, emphasis, code, изображения по asset-ID), без пользовательских iframe/script/style, без inline JS/HTML/URL schemes. Не вставлять markdown через `innerHTML` напрямую: renderer создаёт safe DOM или использует серверный sanitizer с тестами. Админ видит превью **того же** renderer, что игрок.

Драфт [sql/002-knowledge-base.review.sql](./sql/002-knowledge-base.review.sql):

- `aj_guide_categories`: UUID, slug, parent_id, sort_order, visible/hidden/archived. `aj_guide_category_i18n`: отдельный RU/EN title/description.
- `aj_guide_articles`: UUID, slug, category_id FK RESTRICT, status, order, creator; статьи не каскадируются при удалении категории.
- `aj_guide_article_revisions`: immutable revision + locale, title, summary, markdown_source, editor, created_at. Хранить несколько ревизий, а не overwrite опубликованный текст.
- `aj_guide_publications`: active published revision **для каждой локали отдельно** с composite FK на действительную (article,revision,locale). Снятие публикации — DELETE/atomic change ссылки, не история контента.
- `aj_guide_publication_history`: append-only publish/unpublish/archive/restore audit.
- `aj_guide_related`: directed relation source→target UUID, с order; обратный переход в UI определяется через отдельный запрос при необходимости.

**Состояние содержимого:** unpublished draft → published RU/en revision independently → unpublish → archive. В каждой локали можно сохранять draft не влияя на опубликованную revision. Изменение категории/slug не ломает related links: всё связано с UUID. Отношения на архивные/непубличные target не показывать обычному игроку. `current edit revision`/ETag используется для optimistic conflict, два администратора не теряют правки: новая revision + compare-and-swap, при stale expected_revision → 409.

## 3. Категории и безопасное удаление

- До 3 уровней вложенности на v1; `parent_id` допускает root, UUID FK и циклический guard. Все структурные изменения администратора сериализуются; нельзя сделать parent собственным потомком. При нарушении — 409.
- Удалить категорию с вложенными категориями или статьями нельзя по умолчанию (FK RESTRICT). UI предлагает **сначала перенести** статьи/детей, затем архивировать или удалить пустую. Архивирование категории скрывает всех дочерних без потери записей; админ видит и может восстановить.
- Визуальный порядок хранить integer `sort_order` плюс стабильную сортировку по slug/UUID; перестановка категорий и статей требует проверки версий и audit.
- Категории переведены отдельно; при отсутствии переводов целевой локали fallback RU с пометкой. Нельзя показывать RU-заголовок и EN-тело как будто это одна переводная версия.

## 4. Публичный и динамический каталоги

**Два независимых источника:**

1. CMS: admin-authored статьи из PostgreSQL, опубликованные для выбранной локали, связанные материалы, медиа. Отдельный API и cache revision/ETag.
2. Game-generated: ресурсы, корпуса, текущее доступное описание звёзд/объектов и стаж/лимиты — из существующих game catalogs `d().resources`, `w().ship_classes`, `universe` и guide objects. Для них используются виртуальные стабильные ID (`game:resource:iron`, `game:ship:agni`, `game:object:star`), без копирования характеристик в CMS.

Публичный `GET /api/guide` выдаёт объединённый catalog (CMS + game-defined) с `source='article'|'game'` и type/safe ref. UI Original C рендерит общую таблицу/индекс, но dynamic game cards продолжают вызывать работающие `objectHologram`, `shipStats`, `filterShipyards`, `Game.openShipyards()`. Сервис не отдаёт неподтверждённые сведения о скрытых системах и не расширяет ACL исследований: игровые материалы о конкретной географии показывать по существующим знаниям игрока.

Не хранить в редактируемых статьях копии экономических параметров как authoritative. При необходимости использовать вставку «игровая характеристика» с typed safe source key, который вычисляет backend. При отсутствии source в новом релизе показывать unavailable, не старую фиктивную цифру.

## 5. Локализация RU/EN

- Для каждого article+locale отдельный published revision; запрос требует явный `lang=ru|en` из настроек игрока, не из чужой сессии.
- Если EN нет, возвращать всю RU-статью с `requested_locale=en,actual_locale=ru,fallback=true` и честной пометкой. Не комбинировать EN title с RU body и не выдавать неизвестную черновую статью.
- Категории используют тот же fallback на title/description; при связывании статей запрашивать перевод связанной статьи как единое целое.
- Поиск применяется только к опубликованным вариантам и разрешённым динамическим данным, case-insens RU/EN; Unicode и экранирование пользовательского шаблона, лимит page sizes.
- Приватная персональная история чтения/избранное не является публичной CMS записью: для v1 либо RAM, либо `Preferences.key("guide", {account: id, version: 1})` с учётом privacy, cleanup при logout. **Не** размещать в общем globalStorage без account-scoping. Синхронизация между устройствами — будущая задача.

## 6. Original C — интеграция без копирования второй системы окон

- Сохранить `Modules.open("guide")` и `#window-guide .module-content`, existing titlebar, move/resize/pin, shortcut/switcher. Не переносить demo window control, body, stage, global CSS, fake `GUIDE_FIXTURES`/mock API.
- Скопировать **композицию и ограниченные CSS** `.mode-c`, `.c-hero`, `.c-tabs`, `.c-main`, `.c-index`, `.c-reading`, `.result-row`, `.article-pane`, `.holo-frame` под `#window-guide`; использовать token variables `--panel`, `--border`, `--text`, `--accent`.
- Список статей и правый reader — отдельные scroll containers; при изменении активного материала обновлять только reader, index scroll сохранять. Переходы «Недавние/Избранное», поиск, связанные/предыдущая/следующая не сбрасывают страницу к верху. Для фокуса `focus({preventScroll:true})`, где доступно.
- На ширине 420px компактное переключение list↔article, на 810px полноценные две колонки; размеры 420×580, 810×550, 960×580, 760×340 из HANDOFF. Текст и вкладки RU/EN.
- Голограммы встроенных trusted SVG с плавным непрерывным **360°** вращением; не демонстрационный `hologram.js` как готовый production renderer. Применять `objectHologram()` и игровой SVG manifest, проверять reduced-motion.
- Старое содержимое руководства (объекты, ресурсы, корабли, лимиты) сохранить. Персональный favorites/recent — новая функция, нельзя утверждать её существование в production.

## 7. Приёмка

Публикация EN и RU независимо, draft не виден игроку; неверный locale fallback без утечки чужих данных; редакция/связи/архивируются без потерь; редактирование slug и названия сохраняет ссылки; пустой каталог и 100+ статей, поиск, XSS test with inline HTML/script/javascript: URL, SVG / embedded markup; быстрый выбор ряда без сброса скролла; 360° объект, 200% zoom, новая статья без Go-релиза; game values берутся из live каталога, не из fixtures; проверенный render после backend publication cache invalidation.
