# 03. Администрирование базы знаний и безопасные изображения

**Проект, не реализация.** Базируемся на `users.role='admin'`, общей cookie-сессии, `admin_log`, существующей навигации admin-модулей. Пользовательский Original C — только reader, не отдельная административная авторизация.

## 1. Административная панель

Место: новый раздел внутри существующего admin-меню, реализуемый по паттернам `internal/server/web/features/administration/`, без второго «приложения» и отдельного login.

Дерево/таблица категорий с вложенностью не глубже 3, порядок, RU/EN названия; фильтр статусов и поиск. Редактор статьи — обязательные поля slug/category/locale/title/summary/Markdown, preview, связанные статьи, ordered illustrations, двухшаговая кнопка «Сохранить черновик» и отдельная «Опубликовать». История редакций с diff/restore-as-new-revision, preview независимо от публикации. Отдельные действия hide/archive/unpublish. При stale revision/cas mismatch — 409, показать актуальную версию, не перезаписывать конкурирующую правку. Смена категории не удаляет статьи. Удаление непустой категории 409, предложить перенос/архивирование.

**Безопасность:** Go HTTP backend проверяет действительную session cookie и `users.role=admin` на каждом admin read/write/media endpoint. UI hide menu не является ACL. Административная роль не наследуется от корпоративной; corp moderator не редактирует KB. Все мутации пишут `admin_log` с actor, operation, target UUID, version и кодом результата; не писать туда большие тела статей, секреты, полный SVG или оригинал загруженного файла.

## 2. Markdown и публикация

Редактор хранит Markdown, не произвольный HTML. Список допустимых конструкций: headings h2–h4, paragraphs, lists, bold, italic, code, internal article links, game links из allowlist, изображения по media UUID / builtin manifest key; никаких raw HTML, script, iframe, style, embed, event handlers, `javascript:`, `data:`, `file:`, внешнего image src, CSS injection.

Один серверный sanitizer/AST renderer для public и preview. Содержимое выдаётся как текст/структурированные безопасные блоки, UI строит DOM безопасными методами, не `innerHTML` на untrusted source. Ссылки `https://` внешнего домена — либо отклонять, либо allowlisted и `rel=noopener noreferrer`; лучше первая политика для v1. Контроль ссылок по актуальным ID и правам.

Workflow: draft revision immutable INSERT → preview → transaction publish per-locale (verify revision belongs article + locale + admin role, upsert `aj_guide_publications`, append `aj_guide_publication_history`) → COMMIT → bump public content version/cache invalidate. Unpublish удаляет только pointer, не редакцию и не файл. Архивирование скрывает публичное чтение и связи. Вернуть старую редакцию — создать новую revision из snapshot, не менять старую row. Если перевод отсутствует, whole-content RU fallback с честной меткой.

## 3. Загрузка PNG/JPEG/WebP/SVG

**Предлагаемая политика:** принимать PNG/JPEG/WebP до **8 MiB** и 4096×4096 px; SVG как upload input до **1 MiB**, но **не отдавать загруженный SVG как исполняемый/встраиваемый XML**. Приём выполняется в ограниченном worker:
1. Декодировать MIME по сигнатуре/валидным заголовкам и реальному decoder; расширение файла игнорируется как доказательство типа. Ограничения на decompression bombs, pixel count, frame count, CPU/time и recursion; запрет remote file/network access.
2. Raster input: декодировать и перекодировать в безопасный PNG/WebP, удалить EXIF, ICC с опасными chunk-расширениями, встроенные скрипты/metadata, проверить итоговый размер.
3. SVG input: валидировать ограниченный SVG subset, отказать DTD/entities/foreignObject/script/event handlers/filters с external resources, url(), CSS @import, href/xlink с remote/data/file, а затем rasterize в sandbox **без сети** и сохранить только safe растровый результат. Для v1 при отсутствии проверенного sandbox/renderer лучше **временно отвечать 415 SVG uploads unsupported**, чем небезопасно публиковать. Trusted SVG встроенной игры обрабатываются отдельно.
4. Сначала записать файл во временное private storage вне webroot (например `D:/astrajourney/guide-media/staging`) с random UUID; `fsync`/scan/size check. Затем atomically rename в постоянный private path по server generated UUID, вставить `aj_guide_media` metadata в PG, подтвердить COMMIT. При ошибке DB удалить либо GC orphan; при DB commit/file move race нужна `pending → ready` двухфазная запись и recovery scanner. Только `ready` выдавать public.
5. Отдавать медиа по ID через safe handler/имя созданного файла, с `Content-Type: image/png|image/webp|image/jpeg`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` на отдельном origin если доступен; не использовать client filename как путь. Никогда не выставлять webroot на каталог upload.
6. На удаление: ссылки редакций и опубликованных материалов блокируют hard-delete (FK RESTRICT); UI предлагает снять публикацию/разорвать связи и архивировать. Отложенный GC с audit, без удаления используемых исходников. Физические backups PostgreSQL и media должны восстанавливаться из одной согласованной контрольной точки.

Схема: [sql/003-guide-media.review.sql](./sql/003-guide-media.review.sql) — только metadata, file system не изменяется. Media `source_format=svg`, `served_format=png|webp`; v1 `role='hologram'` разрешён **только** для manifest builtin asset.

## 4. Каталог встроенных SVG

Создать build-time или release-time **версионированный manifest** из утверждённых ассетов `internal/server/web/assets/` и существующих mapper `objectHologram()`/SVG кораблей. Конкретный полный список и пути формировать автоматически после аудита реальных assets — не hardcode filesystem paths в JSON клиента.

Пример entries (не утверждённый фактический manifest):
```json
{
  "key": "object/star",
  "kind": "object",
  "path": "/assets/object-star.svg",
  "sha256": "build-generated",
  "hologram_allowed": true
}
```
Реальная запись обязательна из файла в репозитории, сверка hash при сборке, release version. Админ выбирает dropdown/media picker по `key`, статья сохраняет только manifest key в `aj_guide_revision_media`; backend проверяет membership manifest на save **и** read. Нельзя раскрыть `../`, UNC, `file://`, querystring/fragment, произвольный URL или подменить манифест из браузера.

При удалении ассета в следующей версии: не создавать произвольный fallback-path; показывать «Ресурс недоступен», migration/manifest compatibility report, редактору предложить замену. Для доверенных встроенных SVG разрешать уже действующий `objectHologram()`, 360° continuous loop (полный yaw без сброса на 180°), `prefers-reduced-motion`. **Не** использовать `hologram.js` из Original C как production asset library без отдельного графического review.

## 5. Изображения в редакциях

`aj_guide_revision_media` связывает конкретную article+revision+locale с упорядоченными uploads или builtin assets; роль `illustration|cover|hologram`. Для старой ревизии ссылки сохраняются, если опубликованный материал обновился. Markdown может ссылаться на `media UUID`; backend проверяет, что UUID указан в разрешённой media-таблице и `ready`, на save и render; неизвестный ID не рендерить как произвольную URL.

Размеры, пропорции, alt RU/EN, ограничения количества (предложение: до 12 медиа на revision), размер HTML-ответа и preview ограничивать сервером. Для тяжёлого каталога предусмотреть thumbnail и cache-control по content-hash.

## 6. Критичные проверки

Неадмин пытается POST publish/upload → 403; HTML injection `<script>`, SVG `onload`, `foreignObject`, zip bomb, `data:` in markdown, directory traversal → отклонить; raw SVG ни при каких условиях не показывать same-origin HTML; media metadata COMMIT без файла → admin repair/pending, public hide; два конкурирующих publish → 409 stale; архивирование категории сохраняет статьи; RU/EN не смешиваются; restore revision не изменяет историю; manifest asset missing даёт placeholder; CSP действующего игрового клиента не ослабляется.
