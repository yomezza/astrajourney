# 05. API-контракты — проект, не реализовано

## Общие принципы

Все изменения требуют действительной cookie session, server-side проверки роли, UUID operation, canonical request digest, SQL-транзакции и записи результата после COMMIT. Одинаковый UUID+body возвращает сохранённый ответ; тот же UUID с иным body → 409. Actor_id берётся из сессии. NATS — только необязательный сигнал обновления, не источник полномочий.

Коды: 400 неверный ввод, 401 сессия, 403 права/защищённый объект, 404 отсутствует или скрыт, 409 гонка/уже переименовано/UUID collision, 413 превышен размер, 415 формат, 422 недопустимое имя/контент, 429 лимит, 503 сбой службы.

## Существующие GET — минимальное расширение

| Метод и путь | Изменение |
|---|---|
| GET /api/galaxy | Глобальное текущее имя системы, стабильные ID и catalog_code, world revision |
| GET /api/system?id=41 | Discoverer/status/rename_available у звезды и **только разрешённых revealed sites**; не показывать скрытые Planet детали |
| GET /api/fleet, /api/expedition, /api/workbench | Актуальные display names по ID, но без смены активного корабля |
| GET /api/planet/surface, /api/planet/state, /api/planet/projects, /api/planet/project | Новые глобальные имена планеты/колонии, стабильные surface/sector/colony ID |
| GET /api/planet/visit | Авторитетное состояние для восстановления режима после F5 |

В текущем /api/system Star.Planets удаляются из публичного ответа, а открытые планеты приходят через RevealedSites. Сохранить этот ACL, не отдавать все планеты автоматически.

## Новое POST /api/discovery/name

Вход (условный пример):
~~~json
{"operation":"UUID","object":{"kind":"system","star_id":41,"planet_id":0},"name":"Новая Заря"}
~~~

Ответ 200, в том числе при безопасном replay:
~~~json
{"operation":"UUID","object":{"kind":"system","star_id":41,"planet_id":0},"name":"Новая Заря","catalog_code":"edem:star-41","name_revision":1,"world_revision":12345,"rename_available":false}
~~~

Для планеты kind=planet, planet_id>0. Действия: проверить личное первооткрытие, неиспользованное однократное право, founder protection, нормализацию Unicode, уникальность имени и revision; затем одна SQL tx обновляет registry + galaxies.data + history и World.Commit. Имя не становится идентификатором объекта.

Новое POST /api/admin/discovery/name — отдельная коррекция администратора (reason, operation), с admin_log и history, не восстанавливать право игрока и не менять первооткрывателя.

## Колонии

Расширить существующий POST /api/planet/action новым action=rename_colony, id=colony ID, request UUID, name, expected_revision. В той же транзакции проверяются актуальные planet_colonies.owner_user / owner_company, CompanyAccess с разрешением colonize, cooldown (предложено 24 часа), сохранение истории; смена владельца немедленно меняет доступ. Для просмотра использовать уже существующие Planet API.

## База знаний — публичные

| HTTP | Проектный URL | Ответ |
|---|---|---|
| GET | /api/guide/catalog?lang=ru&q=&category=&cursor= | Пагинация до 40, published + visible, game-derived карточки и CMS, ETag, locale/fallback |
| GET | /api/guide/articles/{uuid}?lang=ru | Единственная published revision, safe content blocks, media IDs, links; draft/archived → 404 |
| GET | /api/guide/assets | Публичный manifest разрешённых встроенных ассетов |
| GET | /api/guide/media/{uuid} | Только готовый безопасный PNG/JPEG/WebP с nosniff, никогда пользовательский raw SVG |

Если Go ServeMux требует иной синтаксис, выбрать единый query-id route вместо path variable и зафиксировать в тесте, не дублировать API. RU/EN выбирается по активной сессии пользователя и указанному lang, при отсутствии EN возвращать **всю** RU статью с actual_locale=ru и fallback=true.

## База знаний — административные API

| HTTP | Проектный URL | Операции |
|---|---|---|
| GET/POST | /api/admin/guide/categories | Список и create/update/reparent/reorder/archive/delete; ссылки на дочерние и статьи RESTRICT |
| GET/POST | /api/admin/guide/articles | Черновики, save_revision, move, archive, restore; optimistic expected_revision |
| POST | /api/admin/guide/publish | publish/unpublish article_id+locale+revision, audit |
| GET | /api/admin/guide/revisions?article_id=UUID | История и preview, только admin |
| POST | /api/admin/guide/related | Упорядоченные UUID ссылки add/remove |
| POST | /api/admin/guide/media | Multipart media, проверка сигнатуры/размера/пикселей, безопасная конверсия SVG в растр |
| POST | /api/admin/guide/media/{uuid}/delete | Soft-delete, RESTRICT при ссылках |
| GET | /api/admin/guide/assets | Manifest с доступными встроенными SVG и ключами |

Каждый административный маршрут проверяет users.role=admin **на backend**, а не по видимости кнопки, и пишет только метаданные в admin_log. Markdown source никогда не вставлять как raw HTML; публичный reader и preview используют одинаковый renderer/sanitizer.

## Planet return и клиентские режимы

Оставить POST /api/planet/handoff, GET /api/planet/visit, GET /planet/api/v1/scene, WS /planet/ws/v1. Добавить к Planet WS команду return.prepare с UUID; после server-owned proximity check и COMMIT подтверждения — return.authorized {operation,expires_at}. Existing Cosmic POST /api/planet/return проверяет этот proof под FOR UPDATE и только потом использует старый revoke/reconciliation. Старый WS leave и прямой HTTP return без proof **не должны** позволять штатный обход.

Отдельно — аудированная серверная процедура recovery, если посадочный объект недостижим/сцена повреждена/lease устарел; не обычная кнопка телепорта.

## Обновления кэша

Все смены имени обновляют world revision, новые публикации — content revision. UI сравнивает ETag на открытии/refresh/reconnect; при отсутствии NATS правдивые данные всё равно приходят из PostgreSQL. Core NATS signals только ускоряют обновление. Names не меняют stable ID гиперкоридоров, кораблей или исследовательских данных.
